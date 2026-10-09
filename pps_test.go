// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// pset describes the picture parameter set a test wants written.
type pset struct {
	cabac        bool
	refIdxMinus1 uint32
	// refIdxWideN writes the num_ref_idx fields as a raw exp-Golomb code with n
	// leading zeros instead of ue(refIdxMinus1). ⛔ spsWriter.ue cannot emit
	// 2^32-1, and that is the one value the +1 the syntax carries wraps on, so
	// the writer cannot reach it by itself.
	refIdxWideN  int
	sliceGroups  uint32 // num_slice_groups_minus1
	chromaOffset int32
	tail         bool // state the optional trailing part
	transform8x8 bool
	scaling      bool
	secondOffset int32
}

// build writes p as a NAL unit, ending it the way a payload must end.
func (p pset) build() Unit {
	w := &spsWriter{}
	w.ue(0) // pic_parameter_set_id
	w.ue(0) // seq_parameter_set_id
	if p.cabac {
		w.bit(1)
	} else {
		w.bit(0)
	}
	w.bit(0) // bottom_field_pic_order_in_frame_present_flag
	w.ue(p.sliceGroups)
	for i := 0; i < 2; i++ { // num_ref_idx_l0/l1_default_active_minus1
		if n := p.refIdxWideN; n > 0 {
			for j := 0; j < n; j++ {
				w.bit(0)
			}
			w.bit(1)
			for j := 0; j < n; j++ {
				w.bit(0)
			}
			continue
		}
		w.ue(p.refIdxMinus1)
	}
	w.bit(0)
	w.bits(0, 2) // weighted_bipred_idc
	w.se(-1)     // pic_init_qp_minus26
	w.se(0)      // pic_init_qs_minus26
	w.se(p.chromaOffset)
	w.bit(1) // deblocking_filter_control_present_flag
	w.bit(0)
	w.bit(0)
	if p.tail {
		if p.transform8x8 {
			w.bit(1)
		} else {
			w.bit(0)
		}
		if p.scaling {
			w.bit(1)
			for i := 0; i < 6; i++ {
				w.bit(0) // each 4x4 list absent
			}
			if p.transform8x8 {
				// The writer states two, which is what a 4:2:0 set carries; a
				// 4:4:4 test states six by asking for it below.
				for i := 0; i < 2; i++ {
					w.bit(0)
				}
			}
		} else {
			w.bit(0)
		}
		w.se(p.secondOffset)
	}
	// rbsp_trailing_bits: a one, then zeros to the byte.
	w.bit(1)
	for w.pos%8 != 0 {
		w.bit(0)
	}
	return Unit{Type: UnitPPS, Payload: w.data}
}

func TestAMinimalSetDefaultsTheSecondChromaOffset(t *testing.T) {
	// ⛔ A set that stops before the optional part leaves the second offset equal
	// to the first, not zero. A decoder told zero would shift the chroma
	// quantiser of every picture that refers to this set.
	p, err := ParsePPS(pset{cabac: true, chromaOffset: -2}.build(), SPS{ChromaFormat: 1})
	if err != nil {
		t.Fatalf("ParsePPS: %v", err)
	}
	if !p.CABAC {
		t.Error("entropy coding mode read as CAVLC")
	}
	if p.InitQP != 25 {
		t.Errorf("InitQP = %d, want 25 (26 minus one)", p.InitQP)
	}
	if p.ChromaQPIndexOffset != -2 || p.SecondChromaQPOffset != -2 {
		t.Errorf("offsets %d/%d, want -2/-2", p.ChromaQPIndexOffset, p.SecondChromaQPOffset)
	}
	if p.Transform8x8 {
		t.Error("a set with no trailing part reported 8x8 transforms")
	}
	if p.NumRefIdxL0 != 1 || p.NumRefIdxL1 != 1 {
		t.Errorf("ref idx %d/%d, want 1/1", p.NumRefIdxL0, p.NumRefIdxL1)
	}
}

func TestTheTrailingPartIsReadWhenItIsThere(t *testing.T) {
	for _, tc := range []struct {
		name string
		p    pset
	}{
		{"with 8x8 and scaling lists", pset{cabac: true, chromaOffset: -2, tail: true, transform8x8: true, scaling: true, secondOffset: -3}},
		{"with 8x8 and no lists", pset{cabac: true, chromaOffset: -2, tail: true, transform8x8: true, secondOffset: -3}},
		{"without 8x8, with lists", pset{cabac: true, chromaOffset: -2, tail: true, scaling: true, secondOffset: -3}},
		{"CAVLC", pset{chromaOffset: 0, tail: true, secondOffset: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ParsePPS(tc.p.build(), SPS{ChromaFormat: 1})
			if err != nil {
				t.Fatalf("ParsePPS: %v", err)
			}
			if p.Transform8x8 != tc.p.transform8x8 {
				t.Errorf("Transform8x8 = %v, want %v", p.Transform8x8, tc.p.transform8x8)
			}
			if p.SecondChromaQPOffset != tc.p.secondOffset {
				t.Errorf("second offset = %d, want %d -- a scaling list was not consumed exactly",
					p.SecondChromaQPOffset, tc.p.secondOffset)
			}
			if p.CABAC != tc.p.cabac {
				t.Errorf("CABAC = %v, want %v", p.CABAC, tc.p.cabac)
			}
		})
	}
}

// TestFourFourFourCarriesSixEightByEightLists.
//
// ⛔ The count depends on the SPS's chroma format, which is why ParsePPS asks for
// the set. Given the wrong one, four lists' worth of present flags are left
// behind and the second chroma offset is read from them.
func TestFourFourFourCarriesSixEightByEightLists(t *testing.T) {
	w := &spsWriter{}
	w.ue(0)
	w.ue(0)
	w.bit(1) // cabac
	w.bit(0)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bits(0, 2)
	w.se(-1)
	w.se(0)
	w.se(-2)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	w.bit(1) // transform_8x8_mode_flag
	w.bit(1) // pic_scaling_matrix_present_flag
	for i := 0; i < 6; i++ {
		w.bit(0)
	}
	for i := 0; i < 6; i++ { // six, because the chroma planes are full size
		w.bit(0)
	}
	w.se(-3)
	w.bit(1)
	for w.pos%8 != 0 {
		w.bit(0)
	}
	u := Unit{Type: UnitPPS, Payload: w.data}

	p, err := ParsePPS(u, SPS{ChromaFormat: 3})
	if err != nil {
		t.Fatalf("with 4:4:4: %v", err)
	}
	if p.SecondChromaQPOffset != -3 {
		t.Errorf("second offset = %d, want -3", p.SecondChromaQPOffset)
	}
	// The control: told 4:2:0, the same set is consumed wrongly -- and the
	// identity at the end of ParsePPS is what turns that into a refusal instead
	// of a plausible offset.
	if _, err := ParsePPS(u, SPS{ChromaFormat: 1}); err == nil {
		t.Error("the wrong chroma format was accepted, so the identity check is not doing its work")
	}
}

func TestPPSRefusals(t *testing.T) {
	t.Run("not a PPS", func(t *testing.T) {
		if _, err := ParsePPS(Unit{Type: UnitSPS}, SPS{}); !errors.Is(err, ErrNotPPS) {
			t.Errorf("err = %v, want ErrNotPPS", err)
		}
	})

	t.Run("slice groups", func(t *testing.T) {
		if _, err := ParsePPS(pset{cabac: true, sliceGroups: 1}.build(), SPS{ChromaFormat: 1}); !errors.Is(err, ErrSliceGroups) {
			t.Errorf("err = %v, want ErrSliceGroups", err)
		}
	})

	// ⛔ The identity: a set that does not end on its trailing bits was read
	// wrongly, and there is nothing else in it to catch that.
	t.Run("bits left over", func(t *testing.T) {
		u := pset{cabac: true, chromaOffset: -2}.build()
		u.Payload = append(u.Payload, 0x5A)
		if _, err := ParsePPS(u, SPS{ChromaFormat: 1}); !errors.Is(err, ErrUnsupportedPPS) {
			t.Errorf("err = %v, want ErrUnsupportedPPS", err)
		}
	})

	t.Run("every truncation", func(t *testing.T) {
		whole := pset{cabac: true, chromaOffset: -2, tail: true, transform8x8: true, scaling: true, secondOffset: -3}.build()
		if _, err := ParsePPS(whole, SPS{ChromaFormat: 1}); err != nil {
			t.Fatalf("the fixture does not parse: %v", err)
		}
		for n := 0; n < len(whole.Payload); n++ {
			cut := Unit{Type: UnitPPS, Payload: whole.Payload[:n]}
			if _, err := ParsePPS(cut, SPS{ChromaFormat: 1}); err == nil {
				t.Errorf("%d of %d bytes parsed cleanly", n, len(whole.Payload))
			}
		}
	})
}

// fieldsOnly writes a minimal set's fields and nothing else, with the given
// pic_parameter_set_id, and says how many bits it came to.
func fieldsOnly(id uint32) ([]byte, int) {
	w := &spsWriter{}
	w.ue(id)
	w.ue(0)
	w.bit(1) // cabac
	w.bit(0)
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bit(0)
	w.bits(0, 2)
	w.se(-1)
	w.se(0)
	w.se(-2)
	w.bit(1)
	w.bit(0)
	w.bit(0)
	return w.data, w.pos
}

// TestASetThatEndsWithoutItsPaddingIsAccepted.
//
// ⛔ Every field reads and NOTHING is left -- not even the one bit and the zeros
// that should end a payload. Refusing would discard a set whose contents are
// entirely known over padding that carries nothing.
//
// Expressing that needs the fields to end exactly on a byte, so the identifier is
// chosen for the alignment it produces rather than assumed: padding the last byte
// with zeros would leave bits behind, and those read as a field that is not
// there -- which the test below pins.
func TestASetThatEndsWithoutItsPaddingIsAccepted(t *testing.T) {
	var payload []byte
	for id := uint32(0); id < 64; id++ {
		if data, bits := fieldsOnly(id); bits%8 == 0 {
			payload = data
			break
		}
	}
	if payload == nil {
		t.Skip("no identifier in range makes the fields end on a byte")
	}
	p, err := ParsePPS(Unit{Type: UnitPPS, Payload: payload}, SPS{ChromaFormat: 1})
	if err != nil {
		t.Fatalf("ParsePPS: %v", err)
	}
	if p.ChromaQPIndexOffset != -2 || p.SecondChromaQPOffset != -2 {
		t.Errorf("offsets %d/%d, want -2/-2", p.ChromaQPIndexOffset, p.SecondChromaQPOffset)
	}
}

// TestPaddingWithNoLeadingOneIsRefused is the control: zeros where a payload's
// closing one should be are not padding, and treating them as such would accept a
// set whose end nobody wrote.
func TestPaddingWithNoLeadingOneIsRefused(t *testing.T) {
	data, bits := fieldsOnly(0)
	if bits%8 == 0 {
		t.Skip("this fixture needs padding to exist for the case to arise")
	}
	if _, err := ParsePPS(Unit{Type: UnitPPS, Payload: data}, SPS{ChromaFormat: 1}); err == nil {
		t.Error("a payload closed with zeros instead of a one was accepted")
	}
}

func TestPPSRefusesAnOversizedRefIdx(t *testing.T) {
	// 7.4.2.2 puts num_ref_idx_lX_default_active_minus1 in 0..31. The boundary
	// is pinned on BOTH sides: an off-by-one either refuses conformant streams
	// or leaves the hole open.
	for _, c := range []struct {
		minus1 uint32
		wideN  int
		refuse bool
	}{
		{minus1: 0},
		{minus1: 31}, // the largest conformant value: 32 active entries
		{minus1: 32, refuse: true},
		{minus1: 1 << 20, refuse: true},
		{minus1: 1<<32 - 2, refuse: true},
		// ⛔ 2^32-1, hand-written: the +1 the syntax carries wraps it to ZERO,
		// so a bound applied after the increment lets exactly this one past --
		// and the set then claims a list of no entries at all.
		{wideN: 32, refuse: true},
	} {
		u := pset{refIdxMinus1: c.minus1, refIdxWideN: c.wideN}.build()
		pps, err := ParsePPS(u, SPS{})
		switch {
		case c.refuse && !errors.Is(err, ErrRefIdxRange):
			t.Errorf("minus1=%d wideN=%d in %d bytes: got %v (NumRefIdxL0=%d), want ErrRefIdxRange",
				c.minus1, c.wideN, len(u.Payload), err, pps.NumRefIdxL0)
		case !c.refuse && err != nil:
			t.Errorf("minus1=%d: got %v, want it read", c.minus1, err)
		case !c.refuse && pps.NumRefIdxL0 != c.minus1+1:
			t.Errorf("minus1=%d: NumRefIdxL0=%d, want %d", c.minus1, pps.NumRefIdxL0, c.minus1+1)
		}
	}
}
