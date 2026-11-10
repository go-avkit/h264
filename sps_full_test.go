// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

func baseSet() set {
	return set{profile: 100, level: 30, chroma: 1, mbWidth: 2, mapUnits: 2, frameMBSOnly: true}
}

// TestVUIIsReadWhole: every field of a full VUI, so that the bitstream
// restriction at its end is read from the right bits.
func TestVUIIsReadWhole(t *testing.T) {
	s := baseSet()
	s.vui = func(w *spsWriter) {
		w.bit(1)
		w.bits(255, 8)
		w.bits(4, 16)
		w.bits(3, 16)
		w.bit(1)
		w.bit(1)
		w.bit(1)
		w.bits(5, 3)
		w.bit(1)
		w.bit(1)
		w.bits(1, 8)
		w.bits(2, 8)
		w.bits(3, 8)
		w.bit(1)
		w.ue(1)
		w.ue(2)
		w.bit(1)
		w.bits(1001, 32)
		w.bits(60000, 32)
		w.bit(1)
		hrd := func() {
			w.ue(1) // cpb_cnt_minus1: two buffers
			w.bits(1, 4)
			w.bits(2, 4)
			for j := 0; j < 2; j++ {
				w.ue(100)
				w.ue(200)
				w.bit(uint32(j))
			}
			w.bits(23, 5)
			w.bits(23, 5)
			w.bits(23, 5)
			w.bits(24, 5)
		}
		w.bit(1) // nal_hrd_parameters_present_flag
		hrd()
		w.bit(1) // vcl_hrd_parameters_present_flag
		hrd()
		w.bit(1) // low_delay_hrd_flag
		w.bit(1) // pic_struct_present_flag
		w.bit(1) // bitstream_restriction_flag
		w.bit(1)
		w.ue(2)
		w.ue(1)
		w.ue(15)
		w.ue(15)
		w.ue(3) // max_num_reorder_frames
		w.ue(5) // max_dec_frame_buffering
	}
	got, err := ParseSPS(s.build())
	if err != nil {
		t.Fatal(err)
	}
	want := VUI{
		AspectRatio: true, AspectRatioIDC: 255, SARWidth: 4, SARHeight: 3,
		VideoSignal: true, FullRange: true, ColourPrimaries: 1, TransferCharacteristics: 2, MatrixCoefficients: 3,
		Timing: true, NumUnitsInTick: 1001, TimeScale: 60000, FixedFrameRate: true,
		BitstreamRestriction: true, MaxNumReorderFrames: 3, MaxDecFrameBuffering: 5,
	}
	if !got.VUIPresent || got.VUI != want {
		t.Fatalf("VUI %+v present %v, want %+v", got.VUI, got.VUIPresent, want)
	}
	if got.MaxNumReorder() != 3 {
		t.Errorf("MaxNumReorder %d, want 3", got.MaxNumReorder())
	}
}

func TestAnEmptyVUIStatesNothing(t *testing.T) {
	s := baseSet()
	s.vui = func(w *spsWriter) {
		for i := 0; i < 8; i++ { // aspect, overscan, signal, chroma loc, timing, nal, vcl, pic struct
			w.bit(0)
		}
		w.bit(0) // bitstream_restriction_flag
	}
	got, err := ParseSPS(s.build())
	if err != nil {
		t.Fatal(err)
	}
	if !got.VUIPresent || got.VUI.BitstreamRestriction || got.VUI.ColourPrimaries != 2 {
		t.Fatalf("%+v", got.VUI)
	}
	// Nothing states a reorder depth, so it is the level's whole buffer.
	if got.MaxNumReorder() != got.MaxDpbFrames() {
		t.Errorf("MaxNumReorder %d, MaxDpbFrames %d", got.MaxNumReorder(), got.MaxDpbFrames())
	}
}

func TestAMalformedVUIKeepsTheSizeAndSaysSo(t *testing.T) {
	for name, vui := range map[string]func(w *spsWriter){
		"cut short": func(w *spsWriter) { w.bit(1) },
		"too many buffers": func(w *spsWriter) {
			for i := 0; i < 5; i++ {
				w.bit(0)
			}
			w.bit(1) // nal_hrd_parameters_present_flag
			w.ue(32) // cpb_cnt_minus1 above 31
			w.bits(0, 16)
		},
	} {
		s := baseSet()
		s.vui = vui
		got, err := ParseSPS(s.build())
		if !errors.Is(err, ErrVUI) {
			t.Fatalf("%s: %v, want ErrVUI", name, err)
		}
		if got.Width != 32 || got.Height != 32 {
			t.Errorf("%s: size %dx%d, want 32x32 kept", name, got.Width, got.Height)
		}
	}
}

func TestMaxNumReorderIsClamped(t *testing.T) {
	s := SPS{VUIPresent: true, VUI: VUI{BitstreamRestriction: true, MaxNumReorderFrames: 1 << 20}, MBWidth: 1, MBHeight: 1}
	if got := s.MaxNumReorder(); got != 16 {
		t.Errorf("MaxNumReorder %d, want 16", got)
	}
}

func TestLevelTables(t *testing.T) {
	for _, c := range []struct {
		level, constraint uint8
		profile           uint8
		mbs               uint32
		want              int
		is1b              bool
	}{
		{10, 0, 66, 99, 4, false},
		{11, 0, 66, 99, 9, false},
		{11, 0x10, 66, 99, 4, true},   // level 1b outside the high profiles
		{11, 0x10, 100, 99, 9, false}, // the same bits in a high profile are not 1b
		{9, 0, 100, 99, 4, true},
		{12, 0, 66, 99, 16, false},
		{13, 0, 66, 396, 6, false},
		{20, 0, 66, 396, 6, false},
		{21, 0, 66, 396, 12, false},
		{22, 0, 66, 396, 16, false},
		{30, 0, 66, 396, 16, false},
		{31, 0, 66, 3600, 5, false},
		{32, 0, 66, 3600, 5, false},
		{40, 0, 100, 8160, 4, false},
		{41, 0, 100, 8160, 4, false},
		{42, 0, 100, 8704, 4, false},
		{50, 0, 100, 8160, 13, false},
		{51, 0, 100, 36864, 5, false},
		{52, 0, 100, 36864, 5, false},
		{60, 0, 100, 139264, 5, false},
		{0, 0, 100, 139264, 5, false}, // an unknown level gets the largest buffer
		{40, 0, 100, 0, 1, false},     // no picture size: one frame
	} {
		s := SPS{Profile: c.profile, Level: c.level, ConstraintFlags: c.constraint, MBWidth: c.mbs, MBHeight: 1}
		if c.mbs == 0 {
			s.MBWidth = 0
		}
		if got := s.MaxDpbFrames(); got != c.want {
			t.Errorf("level %d at %d macroblocks: %d frames, want %d", c.level, c.mbs, got, c.want)
		}
		if got := s.IsLevel1b(); got != c.is1b {
			t.Errorf("level %d constraint %#x profile %d: 1b %v, want %v", c.level, c.constraint, c.profile, got, c.is1b)
		}
	}
	if (SPS{ChromaFormat: 3, SeparatePlanes: true}).ChromaArrayType() != 0 || (SPS{ChromaFormat: 2}).ChromaArrayType() != 2 {
		t.Error("ChromaArrayType")
	}
}

// writeList writes one scaling list that states n explicit deltas of d, the
// running value starting from 8.
func scalingSet(t *testing.T, chroma uint8, body func(w *spsWriter)) (SPS, error) {
	t.Helper()
	w := &spsWriter{}
	w.bits(100, 8)
	w.bits(0, 8)
	w.bits(30, 8)
	w.ue(0)
	w.ue(uint32(chroma))
	if chroma == 3 {
		w.bit(0)
	}
	w.ue(0)
	w.ue(0)
	w.bit(1) // qpprime_y_zero_transform_bypass_flag
	w.bit(1) // seq_scaling_matrix_present_flag
	body(w)
	w.ue(0) // log2_max_frame_num_minus4
	w.ue(2) // pic_order_cnt_type
	w.ue(1)
	w.bit(0)
	w.ue(1)
	w.ue(1)
	w.bit(1)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	return ParseSPS(Unit{Type: UnitSPS, Payload: w.data})
}

func TestScalingListsAreStoredWithTheirDefaults(t *testing.T) {
	got, err := scalingSet(t, 1, func(w *spsWriter) {
		w.bit(1) // list 0: explicit, every delta +1 then stops at zero? no: 16 deltas
		for i := 0; i < 16; i++ {
			w.se(1)
		}
		w.bit(1) // list 1: the default list
		w.se(-8)
		for i := 2; i < 6; i++ {
			w.bit(0)
		}
		w.bit(1) // list 6 (8x8): three values, then zero ends it
		w.se(2)
		w.se(2)
		w.se(-12) // 8+2+2-12 = 0: the rest repeats 12
		w.bit(0)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.TransformBypass || !got.ScalingPresent {
		t.Fatalf("flags %+v", got)
	}
	l := got.Scaling
	if !l.Present4x4[0] || l.UseDefault4x4[0] || l.List4x4[0][0] != 9 || l.List4x4[0][15] != 24 {
		t.Errorf("list 0 %v present %v default %v", l.List4x4[0], l.Present4x4[0], l.UseDefault4x4[0])
	}
	if !l.Present4x4[1] || !l.UseDefault4x4[1] {
		t.Errorf("list 1 present %v default %v", l.Present4x4[1], l.UseDefault4x4[1])
	}
	if l.Present4x4[2] || l.Present8x8[1] {
		t.Error("an absent list is marked present")
	}
	e := l.List8x8[0]
	if !l.Present8x8[0] || l.UseDefault8x8[0] || e[0] != 10 || e[1] != 12 || e[2] != 12 || e[63] != 12 {
		t.Errorf("8x8 list 0: present %v %v", l.Present8x8[0], e[:4])
	}
}

func TestFullResolutionChromaStatesTwelveLists(t *testing.T) {
	got, err := scalingSet(t, 3, func(w *spsWriter) {
		for i := 0; i < 12; i++ {
			w.bit(uint32(i % 2))
			if i%2 == 1 {
				w.se(-8)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.Scaling.Present8x8[5] || got.Scaling.Present8x8[4] || !got.Scaling.UseDefault8x8[5] {
		t.Errorf("%+v", got.Scaling.Present8x8)
	}
}

func TestADeltaOutOfRangeIsRefused(t *testing.T) {
	for _, d := range []int32{128, -129} {
		_, err := scalingSet(t, 1, func(w *spsWriter) {
			w.bit(1)
			w.se(d)
			for i := 1; i < 8; i++ {
				w.bit(0)
			}
		})
		if !errors.Is(err, ErrScalingList) {
			t.Errorf("delta %d: %v, want ErrScalingList", d, err)
		}
	}
}

func TestPictureOrderCountOffsetsAreKept(t *testing.T) {
	s := baseSet()
	s.pocType, s.pocOffsets = 1, 3
	got, err := ParseSPS(s.build())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.OffsetForRefFrame) != 3 || got.OffsetForRefFrame[2] != 1 || got.POCAlwaysZero {
		t.Errorf("%+v", got)
	}
}

func TestTheFlagsOfASetAreKept(t *testing.T) {
	s := baseSet()
	s.frameMBSOnly = false
	s.constraint = 0xE0
	got, err := ParseSPS(s.build())
	if err != nil {
		t.Fatal(err)
	}
	if got.ConstraintFlags != 0xE0 || !got.Direct8x8 || got.GapsAllowed || got.MBAdaptive {
		t.Errorf("%+v", got)
	}
}

// TestAPictureSetStatesItsScalingLists: the lists a PPS states are kept, in the
// slots they were sent for, and the ones it omits stay absent.
func TestAPictureSetStatesItsScalingLists(t *testing.T) {
	w := &spsWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(1)
	w.bit(0)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bits(0, 2)
	w.se(0)
	w.se(0)
	w.se(0)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	w.bit(1) // transform_8x8_mode_flag
	w.bit(1) // pic_scaling_matrix_present_flag
	w.bit(0) // list 0 absent
	w.bit(1) // list 1: the default
	w.se(-8)
	for i := 2; i < 6; i++ {
		w.bit(0)
	}
	w.bit(0) // 8x8 list 0 absent
	w.bit(1) // 8x8 list 1: one value, then it ends
	w.se(4)
	w.se(-12)
	w.se(0)
	w.bit(1)
	for w.pos%8 != 0 {
		w.bit(0)
	}
	// 8+4=12, then -12 brings it to zero: the rest repeats 12; the final se is the
	// second chroma offset.
	p, err := ParsePPS(Unit{Type: UnitPPS, Payload: w.data}, SPS{ChromaFormat: 1})
	if err != nil {
		t.Fatal(err)
	}
	l := p.Scaling
	if !p.ScalingPresent || l.Present4x4[0] || !l.Present4x4[1] || !l.UseDefault4x4[1] || l.Present8x8[0] || !l.Present8x8[1] {
		t.Fatalf("%+v", l.Present4x4)
	}
	if l.List8x8[1][0] != 12 || l.List8x8[1][63] != 12 || l.UseDefault8x8[1] {
		t.Errorf("8x8 list 1: %v", l.List8x8[1][:3])
	}
}
