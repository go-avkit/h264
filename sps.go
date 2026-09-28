// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrNotSPS means the unit handed over is not a sequence parameter set.
var ErrNotSPS = errors.New("h264: unit is not a sequence parameter set")

// ErrUnsupportedSPS means the set states something this reader does not follow.
var ErrUnsupportedSPS = errors.New("h264: unsupported sequence parameter set")

// SPS is what a sequence parameter set says about the pictures that follow.
//
// Width and Height are the CROPPED size -- what a player shows and what a
// container states. The coded size is a whole number of macroblocks and is
// usually larger: 1080 lines are coded as 68 rows of 16 and cropped back by
// eight, which is why a reader that stopped before the cropping fields reports
// 1088 for most of the high-definition video there is.
type SPS struct {
	Profile         uint8
	Level           uint8 // level_idc, ten times the level: 41 is level 4.1
	ID              uint32
	ChromaFormat    uint8 // 0 monochrome, 1 4:2:0, 2 4:2:2, 3 4:4:4
	SeparatePlanes  bool
	BitDepthLuma    uint8
	BitDepthChroma  uint8
	FrameMBSOnly    bool
	MBWidth         uint32 // in macroblocks
	MBHeight        uint32 // in macroblocks, after the field doubling
	CropLeft        uint32 // in crop units, as the set states them
	CropRight       uint32
	CropTop         uint32
	CropBottom      uint32
	Width           uint32 // cropped, in luma samples
	Height          uint32
	MaxNumRefFrames uint32

	// What a slice header cannot be read without. A slice states its frame
	// number and its picture order count in widths and shapes this set decides,
	// so a reader holding only the picture size cannot walk one field of a slice.
	Log2MaxFrameNum uint8
	POCType         uint32
	Log2MaxPOCLSB   uint8
	POCAlwaysZero   bool
}

// highProfiles are the profile_idc values that carry a chroma format, bit depths
// and scaling matrices. Every other profile leaves them at their defaults, and a
// reader that took the branch anyway would be misaligned for the whole set.
var highProfiles = map[uint8]bool{
	100: true, 110: true, 122: true, 244: true, 44: true, 83: true,
	86: true, 118: true, 128: true, 138: true, 139: true, 144: true,
}

// ParseSPS reads a sequence parameter set from a NAL unit.
//
// The unit's payload is unescaped here rather than by the caller, because an SPS
// is one of the few units whose bytes are actually read: the escaping has to be
// undone before a single field can be trusted.
func ParseSPS(u Unit) (SPS, error) {
	if u.Type != UnitSPS {
		return SPS{}, fmt.Errorf("%w: type %d", ErrNotSPS, u.Type)
	}
	r := newSticky(u.Unescape())
	var s SPS

	s.Profile = uint8(r.bits(8))
	// Six constraint flags and two bits the format reserves. Nothing here reads
	// them, but they are spent: skipping the wrong number of bits is the
	// classic way to report a plausible size for a picture nobody encoded.
	r.bits(8)
	s.Level = uint8(r.bits(8))
	s.ID = r.ue()

	s.ChromaFormat = 1 // 4:2:0 unless a high profile says otherwise
	s.BitDepthLuma, s.BitDepthChroma = 8, 8
	if highProfiles[s.Profile] {
		if err := s.readHighProfile(r); err != nil {
			return s, err
		}
	}

	s.Log2MaxFrameNum = uint8(r.ue()) + 4
	if err := s.readPicOrderCnt(r); err != nil {
		return s, err
	}
	s.MaxNumRefFrames = r.ue()
	r.bit() // gaps_in_frame_num_value_allowed_flag

	s.MBWidth = r.ue() + 1
	mapUnits := r.ue() + 1
	s.FrameMBSOnly = r.flag()
	if !s.FrameMBSOnly {
		r.bit() // mb_adaptive_frame_field_flag
		// A map unit is a field pair when frames are not the only coding, so the
		// picture is twice as tall in macroblocks as the set states.
		s.MBHeight = mapUnits * 2
	} else {
		s.MBHeight = mapUnits
	}
	r.bit() // direct_8x8_inference_flag

	if r.flag() { // frame_cropping_flag
		s.CropLeft, s.CropRight = r.ue(), r.ue()
		s.CropTop, s.CropBottom = r.ue(), r.ue()
	}
	// vui_parameters_present_flag and everything after it is left unread: the
	// size is settled by here, and the VUI is a long optional structure whose
	// misreading could only harm what is already known.

	if r.err != nil {
		return s, r.err
	}
	s.size()
	return s, nil
}

// readHighProfile reads the chroma format, the bit depths and the scaling
// matrices a high profile states.
func (s *SPS) readHighProfile(r *sticky) error {
	s.ChromaFormat = uint8(r.ue())
	if s.ChromaFormat > 3 {
		return fmt.Errorf("%w: chroma format %d is not 0 to 3", ErrUnsupportedSPS, s.ChromaFormat)
	}
	if s.ChromaFormat == 3 {
		s.SeparatePlanes = r.flag()
	}
	s.BitDepthLuma = uint8(r.ue()) + 8
	s.BitDepthChroma = uint8(r.ue()) + 8
	r.bit() // qpprime_y_zero_transform_bypass_flag
	if r.flag() {
		// ⛔ The scaling matrices are the one variable-length part of an SPS, and
		// the reason a reader that skips them by a fixed amount reports a
		// plausible and wrong picture size. Six lists of sixteen, then two of
		// sixty-four -- or six of sixty-four when the chroma planes are coded at
		// full resolution.
		eights := 2
		if s.ChromaFormat == 3 {
			eights = 6
		}
		for i := 0; i < 6; i++ {
			skipScalingList(r, 16)
		}
		for i := 0; i < eights; i++ {
			skipScalingList(r, 64)
		}
	}
	return nil
}

// skipScalingList consumes one scaling list of n coefficients.
//
// ⛔ It reads deltas only while the running value is non-zero: a delta that
// brings it to zero means "use the default list from here", and NOTHING further
// is stated for that list. Reading n deltas regardless would swallow the bits of
// whatever comes next, and reading none would leave them behind -- both leave the
// reader misaligned with no error to show for it.
func skipScalingList(r *sticky, n int) {
	if !r.flag() { // this list is not present
		return
	}
	last, next := 8, 8
	for j := 0; j < n && next != 0; j++ {
		delta := r.se()
		next = (last + int(delta) + 256) % 256
		if next != 0 {
			last = next
		}
	}
}

// readPicOrderCnt reads the picture order count fields, whose shape depends on
// the type the set states, and keeps what a slice header needs of them.
func (s *SPS) readPicOrderCnt(r *sticky) error {
	s.POCType = r.ue()
	switch t := s.POCType; t {
	case 0:
		s.Log2MaxPOCLSB = uint8(r.ue()) + 4
	case 1:
		s.POCAlwaysZero = r.flag()
		r.se() // offset_for_non_ref_pic
		r.se() // offset_for_top_to_bottom_field
		// ⛔ A count of offsets follows, each one signed. This is the second
		// variable-length part of an SPS, and it is bounded: 255 is the most the
		// format allows, and a larger count read out of a misaligned stream
		// would otherwise spin through the rest of it.
		n := r.ue()
		if n > 255 {
			return fmt.Errorf("%w: %d offsets in the picture order cycle, at most 255", ErrUnsupportedSPS, n)
		}
		for i := uint32(0); i < n; i++ {
			r.se()
		}
	case 2:
		// Nothing more is stated.
	default:
		return fmt.Errorf("%w: picture order count type %d", ErrUnsupportedSPS, t)
	}
	return nil
}

// size works out the cropped picture size.
//
// ⛔ The crop is stated in units, not samples, and the unit depends on the chroma
// format AND on whether frames are the only coding. A reader that treated the
// values as samples would take eight lines off a 4:2:0 frame where sixteen were
// meant, and report 1084 for a picture everything else calls 1080.
func (s *SPS) size() {
	s.Width = s.MBWidth * 16
	s.Height = s.MBHeight * 16

	// ⛔ A switch on the format, not a condition that only excludes monochrome.
	// The condition here before asked whether there IS chroma, which lumps 4:4:4
	// in with the subsampled formats -- and 4:4:4 has chroma at FULL size, so its
	// unit is one sample. It reported a 4:4:4 picture narrower than it is by half
	// its crop, and that looks like a plausible size.
	//
	// The test that should have caught it used separate colour planes, which take
	// the other branch, so the case was never exercised. Found by writing the same
	// arithmetic in go-avkit/h265 and testing it there.
	var cropX, cropY uint32
	switch {
	case s.ChromaFormat == 1: // 4:2:0, half in both directions
		cropX, cropY = 2, 2
	case s.ChromaFormat == 2: // 4:2:2, half width only
		cropX, cropY = 2, 1
	default: // monochrome and 4:4:4: one sample each way
		// Separate colour planes need no case of their own: the flag only exists
		// when the format is 4:4:4, which lands here already.
		cropX, cropY = 1, 1
	}
	if !s.FrameMBSOnly {
		cropY *= 2
	}
	s.Width -= cropX * (s.CropLeft + s.CropRight)
	s.Height -= cropY * (s.CropTop + s.CropBottom)
}
