// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// spsWriter builds a sequence parameter set field by field, so a test states the
// set it means. It writes the Exp-Golomb codes the format uses, which is the only
// way to reach the fields after a variable-length one.
type spsWriter struct {
	data []byte
	pos  int
}

func (w *spsWriter) bit(v uint32) {
	if w.pos%8 == 0 {
		w.data = append(w.data, 0)
	}
	if v&1 == 1 {
		w.data[w.pos/8] |= 1 << (7 - uint(w.pos%8))
	}
	w.pos++
}

func (w *spsWriter) bits(v uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		w.bit(v >> uint(i) & 1)
	}
}

// ue writes an unsigned Exp-Golomb code: the inverse of Reader.UE.
func (w *spsWriter) ue(v uint32) {
	v++
	n := 0
	for t := v; t > 1; t >>= 1 {
		n++
	}
	for i := 0; i < n; i++ {
		w.bit(0)
	}
	for i := n; i >= 0; i-- {
		w.bit(v >> uint(i) & 1)
	}
}

// se writes a signed Exp-Golomb code.
func (w *spsWriter) se(v int32) {
	if v > 0 {
		w.ue(uint32(v)*2 - 1)
		return
	}
	w.ue(uint32(-v) * 2)
}

// set describes the sequence parameter set a test wants written.
type set struct {
	profile      uint8
	level        uint8
	chroma       uint8
	scaling      bool // state scaling matrices
	pocType      uint32
	pocOffsets   uint32 // how many, when pocType is 1
	mbWidth      uint32
	mapUnits     uint32
	frameMBSOnly bool
	crop         [4]uint32 // left, right, top, bottom
	// These four are kept in a BYTE after being read, so a test can state one
	// the format does not allow and watch the conversion narrow it onto a
	// legal value.
	frameNumMinus4 uint32
	pocLSBMinus4   uint32
	bdLuma         uint32
	bdChroma       uint32
}

// build writes s as a NAL unit, header byte and all.
func (s set) build() Unit {
	w := &spsWriter{}
	w.bits(uint32(s.profile), 8)
	w.bits(0, 8) // constraint flags and the two reserved bits
	w.bits(uint32(s.level), 8)
	w.ue(0) // seq_parameter_set_id
	if highProfiles[s.profile] {
		w.ue(uint32(s.chroma))
		if s.chroma == 3 {
			w.bit(0) // separate_colour_plane_flag
		}
		w.ue(s.bdLuma)   // bit_depth_luma_minus8
		w.ue(s.bdChroma) // bit_depth_chroma_minus8
		w.bit(0)
		if s.scaling {
			w.bit(1)
			eights := 2
			if s.chroma == 3 {
				eights = 6
			}
			// The first list is present and ends early on its first delta; the
			// rest are absent. Both shapes have to be consumed correctly or every
			// field after them is misread.
			w.bit(1)
			w.se(-8) // brings the running value to zero: the list ends here
			for i := 1; i < 6; i++ {
				w.bit(0)
			}
			for i := 0; i < eights; i++ {
				w.bit(0)
			}
		} else {
			w.bit(0)
		}
	}
	w.ue(s.frameNumMinus4) // log2_max_frame_num_minus4
	w.ue(s.pocType)
	switch s.pocType {
	case 0:
		w.ue(s.pocLSBMinus4)
	case 1:
		w.bit(0)
		w.se(0)
		w.se(0)
		w.ue(s.pocOffsets)
		for i := uint32(0); i < s.pocOffsets; i++ {
			w.se(1)
		}
	}
	w.ue(1) // max_num_ref_frames
	w.bit(0)
	w.ue(s.mbWidth - 1)
	w.ue(s.mapUnits - 1)
	if s.frameMBSOnly {
		w.bit(1)
	} else {
		w.bit(0)
		w.bit(0) // mb_adaptive_frame_field_flag
	}
	w.bit(1) // direct_8x8_inference_flag
	if s.crop != [4]uint32{} {
		w.bit(1)
		for _, v := range s.crop {
			w.ue(v)
		}
	} else {
		w.bit(0)
	}
	w.bit(0) // vui_parameters_present_flag
	return Unit{Type: UnitSPS, Payload: w.data}
}

// TestASetStatesTheCroppedSize.
//
// ⛔ 1080 lines are coded as 68 rows of sixteen -- 1088 -- and cropped back by
// four units of two. A reader that stopped before the cropping fields reports
// 1088 for most high-definition video there is, and one that treated the units as
// samples reports 1084. Both are plausible and wrong.
func TestASetStatesTheCroppedSize(t *testing.T) {
	for _, tc := range []struct {
		name          string
		s             set
		width, height uint32
	}{
		{
			"1080p, cropped from 1088",
			set{profile: 100, level: 40, chroma: 1, mbWidth: 120, mapUnits: 68,
				frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 4}},
			1920, 1080,
		},
		{
			"854 wide, cropped from 864",
			set{profile: 100, level: 31, chroma: 1, mbWidth: 54, mapUnits: 30,
				frameMBSOnly: true, crop: [4]uint32{0, 5, 0, 0}},
			854, 480,
		},
		{
			"cropped on both axes",
			set{profile: 100, level: 41, chroma: 1, mbWidth: 68, mapUnits: 120,
				frameMBSOnly: true, crop: [4]uint32{0, 4, 0, 1}},
			1080, 1918,
		},
		{
			"4K, no crop at all",
			set{profile: 77, level: 51, chroma: 1, mbWidth: 240, mapUnits: 135,
				frameMBSOnly: true},
			3840, 2160,
		},
		{
			// 4:2:2 is half width and full height, so a vertical crop unit is one
			// sample where 4:2:0 makes it two.
			"4:2:2 crops vertically by one",
			set{profile: 122, level: 40, chroma: 2, mbWidth: 120, mapUnits: 68,
				frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 4}},
			1920, 1084,
		},
		{
			// Monochrome has no chroma planes, so both units are one sample.
			"monochrome crops by one each way",
			set{profile: 100, level: 40, chroma: 0, mbWidth: 120, mapUnits: 68,
				frameMBSOnly: true, crop: [4]uint32{0, 2, 0, 4}},
			1918, 1084,
		},
		{
			// Fields double the height in macroblocks AND the vertical crop unit.
			"fields double the height and the crop unit",
			set{profile: 100, level: 40, chroma: 1, mbWidth: 120, mapUnits: 34,
				frameMBSOnly: false, crop: [4]uint32{0, 0, 0, 2}},
			1920, 1080,
		},
		{
			// ⛔ The case the suite was missing. With the planes coded together
			// the format is still 4:4:4, so the unit is still one sample -- and
			// the condition this replaced took two, reporting 1912x1072 for a
			// picture that is 1916x1080.
			"4:4:4 with the planes together crops by one",
			set{profile: 244, level: 40, chroma: 3, mbWidth: 120, mapUnits: 68,
				frameMBSOnly: true, crop: [4]uint32{0, 4, 0, 8}},
			1916, 1080,
		},
		{
			"4:4:4 with separate planes crops by one",
			set{profile: 244, level: 40, chroma: 3, mbWidth: 120, mapUnits: 68,
				frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 8}},
			1920, 1080,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSPS(tc.s.build())
			if err != nil {
				t.Fatalf("ParseSPS: %v", err)
			}
			if s.Width != tc.width || s.Height != tc.height {
				t.Errorf("%dx%d, want %dx%d", s.Width, s.Height, tc.width, tc.height)
			}
		})
	}
}

// TestScalingMatricesAreConsumedWhole.
//
// ⛔ The matrices are the one variable-length part of a high-profile set. If they
// are not consumed exactly, every field after them is read from the wrong bits --
// and the first of those fields is the picture size, so the failure shows up as a
// plausible number rather than an error. The size asserted here is the witness.
func TestScalingMatricesAreConsumedWhole(t *testing.T) {
	for _, chroma := range []uint8{1, 3} {
		base := set{profile: 100, level: 40, chroma: chroma, scaling: true,
			mbWidth: 120, mapUnits: 68, frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 4}}
		s, err := ParseSPS(base.build())
		if err != nil {
			t.Fatalf("chroma %d: %v", chroma, err)
		}
		want := uint32(1080)
		if chroma == 3 {
			want = 1084 // a 4:4:4 vertical crop unit is one sample
		}
		if s.Width != 1920 || s.Height != want {
			t.Errorf("chroma %d: %dx%d, want 1920x%d -- the matrices were not consumed exactly",
				chroma, s.Width, s.Height, want)
		}
	}
}

func TestPictureOrderCountShapesAreConsumed(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    set
	}{
		{"type 0", set{profile: 66, level: 30, mbWidth: 80, mapUnits: 45, frameMBSOnly: true, pocType: 0}},
		{"type 1 with no offsets", set{profile: 66, level: 30, mbWidth: 80, mapUnits: 45, frameMBSOnly: true, pocType: 1}},
		{"type 1 with offsets", set{profile: 66, level: 30, mbWidth: 80, mapUnits: 45, frameMBSOnly: true, pocType: 1, pocOffsets: 3}},
		{"type 2", set{profile: 66, level: 30, mbWidth: 80, mapUnits: 45, frameMBSOnly: true, pocType: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := ParseSPS(tc.s.build())
			if err != nil {
				t.Fatalf("ParseSPS: %v", err)
			}
			if s.Width != 1280 || s.Height != 720 {
				t.Errorf("%dx%d, want 1280x720 -- a field of this shape was left unread",
					s.Width, s.Height)
			}
		})
	}
}

func TestSPSRefusals(t *testing.T) {
	t.Run("not an SPS", func(t *testing.T) {
		if _, err := ParseSPS(Unit{Type: UnitPPS}); !errors.Is(err, ErrNotSPS) {
			t.Errorf("err = %v, want ErrNotSPS", err)
		}
	})

	t.Run("a chroma format the format has no value for", func(t *testing.T) {
		w := &spsWriter{}
		w.bits(100, 8)
		w.bits(0, 8)
		w.bits(40, 8)
		w.ue(0)
		w.ue(4) // chroma_format_idc
		if _, err := ParseSPS(Unit{Type: UnitSPS, Payload: w.data}); !errors.Is(err, ErrUnsupportedSPS) {
			t.Errorf("err = %v, want ErrUnsupportedSPS", err)
		}
	})

	t.Run("an unknown picture order count type", func(t *testing.T) {
		w := &spsWriter{}
		w.bits(66, 8)
		w.bits(0, 8)
		w.bits(30, 8)
		w.ue(0)
		w.ue(0)
		w.ue(9) // pic_order_cnt_type
		if _, err := ParseSPS(Unit{Type: UnitSPS, Payload: w.data}); !errors.Is(err, ErrUnsupportedSPS) {
			t.Errorf("err = %v, want ErrUnsupportedSPS", err)
		}
	})

	// ⛔ The offset count is bounded because a count read out of a misaligned
	// stream is enormous, and the loop it drives would walk the rest of the
	// bitstream before failing.
	t.Run("more offsets than the format allows", func(t *testing.T) {
		w := &spsWriter{}
		w.bits(66, 8)
		w.bits(0, 8)
		w.bits(30, 8)
		w.ue(0)
		w.ue(0)
		w.ue(1) // pic_order_cnt_type 1
		w.bit(0)
		w.se(0)
		w.se(0)
		w.ue(300)
		if _, err := ParseSPS(Unit{Type: UnitSPS, Payload: w.data}); !errors.Is(err, ErrUnsupportedSPS) {
			t.Errorf("err = %v, want ErrUnsupportedSPS", err)
		}
	})

	// Every prefix must be refused: a set completed from nothing describes a
	// picture nobody encoded.
	t.Run("every truncation", func(t *testing.T) {
		whole := set{profile: 100, level: 40, chroma: 1, scaling: true, mbWidth: 120,
			mapUnits: 68, frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 4}}.build()
		if _, err := ParseSPS(whole); err != nil {
			t.Fatalf("the fixture does not parse: %v", err)
		}
		for n := 0; n < len(whole.Payload); n++ {
			cut := Unit{Type: UnitSPS, Payload: whole.Payload[:n]}
			if _, err := ParseSPS(cut); err == nil {
				t.Errorf("%d of %d bytes parsed cleanly", n, len(whole.Payload))
			}
		}
	})
}

// TestTheEscapingIsUndoneBeforeAnyFieldIsRead: an SPS whose bytes need unescaping
// must read the same as one that does not, or the fields after the escape are
// read from bits nobody wrote.
func TestTheEscapingIsUndoneBeforeAnyFieldIsRead(t *testing.T) {
	plain := set{profile: 100, level: 40, chroma: 1, mbWidth: 120, mapUnits: 68,
		frameMBSOnly: true, crop: [4]uint32{0, 0, 0, 4}}.build()
	want, err := ParseSPS(plain)
	if err != nil {
		t.Fatalf("ParseSPS: %v", err)
	}
	// Escape the payload the way an encoder would: every 00 00 becomes 00 00 03.
	var escaped []byte
	zeros := 0
	for _, b := range plain.Payload {
		if zeros == 2 && b <= 3 {
			escaped = append(escaped, 3)
			zeros = 0
		}
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
		escaped = append(escaped, b)
	}
	if len(escaped) == len(plain.Payload) {
		t.Skip("this fixture holds no pair of zeros to escape")
	}
	got, err := ParseSPS(Unit{Type: UnitSPS, Payload: escaped})
	if err != nil {
		t.Fatalf("escaped: %v", err)
	}
	if got != want {
		t.Errorf("escaped set read as %+v, want %+v", got, want)
	}
}

// TestAWidthCheckedAfterTheCastThatWraps.
//
// ⛔ log2_max_frame_num_minus4 and log2_max_pic_order_cnt_lsb_minus4 are BIT
// COUNTS: the slice header reads frame_num and the order count with them, so a
// reader given a wrong one is misaligned from that field on. Each was narrowed
// to a byte with NO bound at all.
//
// A narrowing conversion does not merely lose the value, it maps out-of-range
// values ONTO LEGAL ONES -- uint8(1048576) is 0, and uint8(256)+4 is 4, the
// smallest width the format allows. A check made after it would pass them.
func TestAWidthCheckedAfterTheCastThatWraps(t *testing.T) {
	base := func() set {
		return set{profile: 100, level: 30, chroma: 1, mbWidth: 2, mapUnits: 2, frameMBSOnly: true}
	}
	for _, c := range []struct {
		name  string
		apply func(*set)
	}{
		{"a frame number width one past the range", func(s *set) { s.frameNumMinus4 = 13 }},
		{"a frame number width wrapping to the smallest", func(s *set) { s.frameNumMinus4 = 256 }},
		{"a frame number width a million past", func(s *set) { s.frameNumMinus4 = 1 << 20 }},
		{"an order count width one past the range", func(s *set) { s.pocLSBMinus4 = 13 }},
		{"an order count width wrapping to zero", func(s *set) { s.pocLSBMinus4 = 252 }},
		{"a luma depth one past the range", func(s *set) { s.bdLuma = 7 }},
		{"a luma depth wrapping to zero", func(s *set) { s.bdLuma = 256 }},
		{"a chroma depth a million past", func(s *set) { s.bdChroma = 1 << 20 }},
	} {
		s := base()
		c.apply(&s)
		if _, err := ParseSPS(s.build()); !errors.Is(err, ErrUnsupportedSPS) {
			t.Errorf("%s: err = %v, want ErrUnsupportedSPS", c.name, err)
		}
	}

	// The largest each one allows must be READ: a bound one too tight refuses
	// conformant sequences.
	s := base()
	s.frameNumMinus4, s.pocLSBMinus4, s.bdLuma, s.bdChroma = 12, 12, 6, 6
	sps, err := ParseSPS(s.build())
	if err != nil {
		t.Fatalf("the largest conformant widths were refused: %v", err)
	}
	if sps.Log2MaxFrameNum != 16 || sps.Log2MaxPOCLSB != 16 || sps.BitDepthLuma != 14 {
		t.Errorf("widths %d/%d, depth %d; want 16/16 and 14",
			sps.Log2MaxFrameNum, sps.Log2MaxPOCLSB, sps.BitDepthLuma)
	}
}
