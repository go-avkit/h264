// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
	"testing"
)

// slice describes the header a test wants written.
type slice struct {
	firstMB   uint32
	typ       uint32 // as stated, so a value above four can be tested
	idr       bool
	frameNum  uint32
	fieldPic  bool
	bottom    bool
	pocLSB    uint32
	deltaBot  int32
	delta0    int32
	delta1    int32
	redundant uint32
}

// build writes the header as a NAL unit, reading the widths and conditionals out
// of the two parameter sets exactly as the parser will.
func (s slice) build(sps SPS, pps PPS) Unit {
	w := &spsWriter{}
	w.ue(s.firstMB)
	w.ue(s.typ)
	w.ue(0) // pic_parameter_set_id
	if sps.SeparatePlanes {
		w.bits(1, 2) // colour_plane_id
	}
	w.bits(s.frameNum, int(sps.Log2MaxFrameNum))
	if !sps.FrameMBSOnly {
		if s.fieldPic {
			w.bit(1)
			if s.bottom {
				w.bit(1)
			} else {
				w.bit(0)
			}
		} else {
			w.bit(0)
		}
	}
	if s.idr {
		w.ue(7) // idr_pic_id
	}
	switch sps.POCType {
	case 0:
		w.bits(s.pocLSB, int(sps.Log2MaxPOCLSB))
		if pps.BottomFieldPicOrder && !s.fieldPic {
			w.se(s.deltaBot)
		}
	case 1:
		if !sps.POCAlwaysZero {
			w.se(s.delta0)
			if pps.BottomFieldPicOrder && !s.fieldPic {
				w.se(s.delta1)
			}
		}
	}
	if pps.RedundantPicCnt {
		w.ue(s.redundant)
	}
	// Coded data would follow; a byte of it stands in, so nothing depends on the
	// header ending the payload.
	w.bits(0xAB, 8)
	typ := UnitNonIDR
	if s.idr {
		typ = UnitIDR
	}
	return Unit{Type: typ, Payload: w.data}
}

// plain is the commonest pair of parameter sets: frames only, picture order type
// zero, nothing optional.
func plain() (SPS, PPS) {
	return SPS{ChromaFormat: 1, FrameMBSOnly: true, Log2MaxFrameNum: 4,
			POCType: 0, Log2MaxPOCLSB: 4},
		PPS{}
}

func TestEachSliceTypeIsNamed(t *testing.T) {
	sps, pps := plain()
	for stated, want := range map[uint32]SliceType{
		0: SliceP, 1: SliceB, 2: SliceI, 3: SliceSP, 4: SliceSI,
	} {
		h, err := ParseSliceHeader(slice{typ: stated, frameNum: 3, pocLSB: 2}.build(sps, pps), sps, pps)
		if err != nil {
			t.Errorf("type %d: %v", stated, err)
			continue
		}
		if h.Type != want {
			t.Errorf("stated %d read as %v, want %v", stated, h.Type, want)
		}
		if h.AllOfType {
			t.Errorf("stated %d reported as fixed for the whole picture", stated)
		}
		if h.FrameNum != 3 || h.POCLSB != 2 {
			t.Errorf("stated %d: frame %d, poc %d -- a width was misread", stated, h.FrameNum, h.POCLSB)
		}
	}
}

// TestATypeAboveFourIsTheSameTypeSaidMoreStrongly.
//
// ⛔ Five to nine name the same five types and promise every slice of the picture
// is coded that way. Taking the number as it stands would name a type the format
// does not have, and a reader switching on it would fall through to nothing.
func TestATypeAboveFourIsTheSameTypeSaidMoreStrongly(t *testing.T) {
	sps, pps := plain()
	for stated, want := range map[uint32]SliceType{
		5: SliceP, 6: SliceB, 7: SliceI, 8: SliceSP, 9: SliceSI,
	} {
		h, err := ParseSliceHeader(slice{typ: stated, frameNum: 1, pocLSB: 1}.build(sps, pps), sps, pps)
		if err != nil {
			t.Errorf("type %d: %v", stated, err)
			continue
		}
		if h.Type != want {
			t.Errorf("stated %d read as %v, want %v", stated, h.Type, want)
		}
		if !h.AllOfType {
			t.Errorf("stated %d did not report the promise it makes", stated)
		}
	}
}

func TestEachPictureOrderShapeIsConsumed(t *testing.T) {
	for _, tc := range []struct {
		name string
		sps  SPS
		pps  PPS
		s    slice
		want func(SliceHeader) error
	}{
		{
			"type 0 with a bottom delta",
			SPS{FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 0, Log2MaxPOCLSB: 6},
			PPS{BottomFieldPicOrder: true},
			slice{typ: 2, pocLSB: 33, deltaBot: -3},
			func(h SliceHeader) error {
				if h.POCLSB != 33 || h.DeltaPOCBottom != -3 {
					return errWant("poc %d delta %d, want 33 and -3", h.POCLSB, h.DeltaPOCBottom)
				}
				return nil
			},
		},
		{
			"type 1 with two deltas",
			SPS{FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 1},
			PPS{BottomFieldPicOrder: true},
			slice{typ: 0, delta0: 5, delta1: -6},
			func(h SliceHeader) error {
				if h.DeltaPOC != [2]int32{5, -6} {
					return errWant("deltas %v, want [5 -6]", h.DeltaPOC)
				}
				return nil
			},
		},
		{
			"type 1 stating nothing",
			SPS{FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 1, POCAlwaysZero: true},
			PPS{},
			slice{typ: 0},
			func(h SliceHeader) error {
				if h.DeltaPOC != [2]int32{} {
					return errWant("deltas %v, want none read", h.DeltaPOC)
				}
				return nil
			},
		},
		{
			"type 2 stating nothing",
			SPS{FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 2},
			PPS{},
			slice{typ: 2},
			func(h SliceHeader) error { return nil },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, err := ParseSliceHeader(tc.s.build(tc.sps, tc.pps), tc.sps, tc.pps)
			if err != nil {
				t.Fatalf("ParseSliceHeader: %v", err)
			}
			if err := tc.want(h); err != nil {
				t.Error(err)
			}
		})
	}
}

func errWant(format string, a ...any) error {
	return fmt.Errorf(format, a...)
}

func TestFieldsAndPlanesAreReadWhenTheSetsSaySo(t *testing.T) {
	// A picture coded as fields states which field, and only then.
	sps := SPS{FrameMBSOnly: false, Log2MaxFrameNum: 4, POCType: 0, Log2MaxPOCLSB: 4}
	pps := PPS{}
	h, err := ParseSliceHeader(slice{typ: 2, fieldPic: true, bottom: true, pocLSB: 3}.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatalf("ParseSliceHeader: %v", err)
	}
	if !h.FieldPic || !h.BottomField || h.POCLSB != 3 {
		t.Errorf("%+v", h)
	}

	// Separate colour planes put a plane number before the frame number.
	sps2 := SPS{SeparatePlanes: true, FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 0, Log2MaxPOCLSB: 4}
	h2, err := ParseSliceHeader(slice{typ: 2, frameNum: 9, pocLSB: 5}.build(sps2, pps), sps2, pps)
	if err != nil {
		t.Fatalf("separate planes: %v", err)
	}
	if h2.ColourPlane != 1 || h2.FrameNum != 9 || h2.POCLSB != 5 {
		t.Errorf("%+v -- the plane number was not consumed", h2)
	}
}

func TestAnIDRStatesItsIdentifierAndARedundantCountIsRead(t *testing.T) {
	sps, pps := plain()
	pps.RedundantPicCnt = true
	h, err := ParseSliceHeader(slice{typ: 2, idr: true, pocLSB: 1, redundant: 2}.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatalf("ParseSliceHeader: %v", err)
	}
	if !h.IDR || h.IDRPicID != 7 || h.RedundantPicCnt != 2 {
		t.Errorf("%+v", h)
	}
}

func TestOnlyTheSliceAtMacroblockZeroBeginsAPicture(t *testing.T) {
	sps, pps := plain()
	first, err := ParseSliceHeader(slice{typ: 2, pocLSB: 1}.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	later, err := ParseSliceHeader(slice{firstMB: 120, typ: 2, pocLSB: 1}.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if !first.BeginsPicture() {
		t.Error("the slice at macroblock zero does not begin a picture")
	}
	if later.BeginsPicture() {
		t.Error("a slice part way into a picture was said to begin one")
	}
	if later.FirstMB != 120 {
		t.Errorf("FirstMB = %d, want 120", later.FirstMB)
	}
}

func TestSliceHeaderRefusals(t *testing.T) {
	sps, pps := plain()

	t.Run("not a slice", func(t *testing.T) {
		if _, err := ParseSliceHeader(Unit{Type: UnitSEI}, sps, pps); !errors.Is(err, ErrNotSlice) {
			t.Errorf("err = %v, want ErrNotSlice", err)
		}
	})

	t.Run("a type the format does not have", func(t *testing.T) {
		if _, err := ParseSliceHeader(slice{typ: 12}.build(sps, pps), sps, pps); !errors.Is(err, ErrSliceHeader) {
			t.Errorf("err = %v, want ErrSliceHeader", err)
		}
	})

	t.Run("a picture order count type the format does not have", func(t *testing.T) {
		bad := SPS{FrameMBSOnly: true, Log2MaxFrameNum: 4, POCType: 5}
		if _, err := ParseSliceHeader(slice{typ: 2}.build(bad, pps), bad, pps); !errors.Is(err, ErrSliceHeader) {
			t.Errorf("err = %v, want ErrSliceHeader", err)
		}
	})

	t.Run("every truncation", func(t *testing.T) {
		whole := slice{typ: 2, idr: true, frameNum: 5, pocLSB: 9}.build(sps, pps)
		if _, err := ParseSliceHeader(whole, sps, pps); err != nil {
			t.Fatalf("the fixture does not parse: %v", err)
		}
		for n := 0; n < len(whole.Payload)-1; n++ {
			cut := Unit{Type: whole.Type, Payload: whole.Payload[:n]}
			if _, err := ParseSliceHeader(cut, sps, pps); err == nil {
				t.Errorf("%d of %d bytes parsed cleanly", n, len(whole.Payload))
			}
		}
	})
}

func TestSliceTypeNames(t *testing.T) {
	for typ, want := range map[SliceType]string{
		SliceP: "P", SliceB: "B", SliceI: "I", SliceSP: "SP", SliceSI: "SI",
		SliceType(42): "slice type 42",
	} {
		if got := typ.String(); got != want {
			t.Errorf("%d = %q, want %q", uint8(typ), got, want)
		}
	}
}
