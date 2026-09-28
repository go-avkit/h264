// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrNotSlice means the unit handed over does not carry a coded slice.
var ErrNotSlice = errors.New("h264: unit is not a coded slice")

// ErrSliceHeader means a slice states something this reader does not follow.
var ErrSliceHeader = errors.New("h264: unsupported slice header")

// SliceType is how a slice is coded.
type SliceType uint8

// The slice types, numbered as the format numbers them.
const (
	SliceP  SliceType = 0
	SliceB  SliceType = 1
	SliceI  SliceType = 2
	SliceSP SliceType = 3
	SliceSI SliceType = 4
)

// String names a slice type the way a reader of tools would expect.
func (t SliceType) String() string {
	switch t {
	case SliceP:
		return "P"
	case SliceB:
		return "B"
	case SliceI:
		return "I"
	case SliceSP:
		return "SP"
	case SliceSI:
		return "SI"
	}
	return fmt.Sprintf("slice type %d", uint8(t))
}

// SliceHeader is the beginning of a coded slice: what it belongs to and how it is
// coded, but not the coded data itself.
//
// FirstMB is what says where a picture begins. A picture may be carried by
// several slices, and only the one starting at macroblock zero begins a new
// picture -- which is how a stream's pictures are counted without decoding any.
type SliceHeader struct {
	FirstMB         uint32
	Type            SliceType
	AllOfType       bool // slice_type was stated above 4: every slice of this picture is this type
	PPSID           uint32
	ColourPlane     uint8
	FrameNum        uint32
	FieldPic        bool
	BottomField     bool
	IDR             bool
	IDRPicID        uint32
	POCLSB          uint32
	DeltaPOCBottom  int32
	DeltaPOC        [2]int32
	RedundantPicCnt uint32
}

// ParseSliceHeader reads the header at the start of a coded slice.
//
// ⛔ It needs both parameter sets, and for the same reason ParsePPS needs the SPS:
// a slice states its frame number in a width the SPS decides and its picture
// order count in a shape the SPS chooses, and whether two of its fields are there
// at all is decided by the PPS. A reader given neither cannot walk a single field.
//
// It stops after the redundant picture count. Everything past that point depends
// on the slice type in ways that branch further -- reference list changes,
// weighting tables, reference marking -- and none of it is needed to say what a
// slice is, which picture it belongs to, or where a picture begins. Unlike a
// parameter set there is no identity to check the end against: coded data follows
// the header, so stopping early cannot be detected from inside and is stated
// here instead.
func ParseSliceHeader(u Unit, sps SPS, pps PPS) (SliceHeader, error) {
	if u.Type != UnitIDR && u.Type != UnitNonIDR {
		return SliceHeader{}, fmt.Errorf("%w: type %d", ErrNotSlice, u.Type)
	}
	r := newSticky(u.Unescape())
	var h SliceHeader
	h.IDR = u.Type == UnitIDR

	h.FirstMB = r.ue()
	t := r.ue()
	// ⛔ A type above four is the same type said more strongly: it promises every
	// slice of the picture is coded this way. Taking the number as it stands would
	// name a type the format does not have.
	if t > 4 {
		t -= 5
		h.AllOfType = true
	}
	if t > 4 {
		return h, fmt.Errorf("%w: slice type %d", ErrSliceHeader, t)
	}
	h.Type = SliceType(t)
	h.PPSID = r.ue()

	if sps.SeparatePlanes {
		h.ColourPlane = uint8(r.bits(2))
	}
	h.FrameNum = r.bits(int(sps.Log2MaxFrameNum))

	if !sps.FrameMBSOnly {
		h.FieldPic = r.flag()
		if h.FieldPic {
			h.BottomField = r.flag()
		}
	}
	if h.IDR {
		h.IDRPicID = r.ue()
	}

	// The picture order count is stated in one of three shapes, and the third
	// states nothing at all.
	switch sps.POCType {
	case 0:
		h.POCLSB = r.bits(int(sps.Log2MaxPOCLSB))
		if pps.BottomFieldPicOrder && !h.FieldPic {
			h.DeltaPOCBottom = r.se()
		}
	case 1:
		if !sps.POCAlwaysZero {
			h.DeltaPOC[0] = r.se()
			if pps.BottomFieldPicOrder && !h.FieldPic {
				h.DeltaPOC[1] = r.se()
			}
		}
	case 2:
		// The order is the decoding order, and nothing is stated.
	default:
		return h, fmt.Errorf("%w: picture order count type %d", ErrSliceHeader, sps.POCType)
	}

	if pps.RedundantPicCnt {
		h.RedundantPicCnt = r.ue()
	}
	if r.err != nil {
		return h, r.err
	}
	return h, nil
}

// BeginsPicture says whether this slice starts a new coded picture.
func (h SliceHeader) BeginsPicture() bool { return h.FirstMB == 0 }
