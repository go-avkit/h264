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

	// What picture order count type 1 derives from (8.2.1.2): the offsets, and the
	// cycle of per-reference-frame offsets, at most 255 of them.
	OffsetForNonRefPic        int32
	OffsetForTopToBottomField int32
	OffsetForRefFrame         []int32

	// ConstraintFlags is the byte after profile_idc: constraint_set0..5_flag and the
	// two reserved bits, constraint_set0 being the top bit.
	ConstraintFlags uint8
	// GapsAllowed is gaps_in_frame_num_value_allowed_flag.
	GapsAllowed bool
	// MBAdaptive is mb_adaptive_frame_field_flag, stated only when frames are not
	// the only coding.
	MBAdaptive bool
	// Direct8x8 is direct_8x8_inference_flag.
	Direct8x8 bool
	// TransformBypass is qpprime_y_zero_transform_bypass_flag.
	TransformBypass bool
	// ScalingPresent is seq_scaling_matrix_present_flag; Scaling are the lists it
	// states (7.3.2.1.1.1), those it does not state left as not Present.
	ScalingPresent bool
	Scaling        ScalingLists
	// VUIPresent is vui_parameters_present_flag, and VUI what it carries.
	VUIPresent bool
	VUI        VUI
}

// ChromaArrayType is the chroma format as the decoding process sees it: 0 when the
// colour planes are coded separately, whatever the stated format.
func (s SPS) ChromaArrayType() uint8 { return chromaArrayType(s) }

// IsLevel1b reports level 1b, which is stated as level_idc 9 or, outside the high
// profiles, as level 1.1 with constraint_set3_flag.
func (s SPS) IsLevel1b() bool {
	if s.Level == 9 {
		return true
	}
	return !highProfiles[s.Profile] && s.Level == 11 && s.ConstraintFlags&0x10 != 0
}

// maxDpbMbs is MaxDpbMbs of table A-1 for the stated level; an unknown level gets
// the largest, which is the value that refuses nothing.
func (s SPS) maxDpbMbs() uint32 {
	if s.IsLevel1b() {
		return 396
	}
	switch s.Level {
	case 10:
		return 396
	case 11:
		return 900
	case 12, 13, 20:
		return 2376
	case 21:
		return 4752
	case 22, 30:
		return 8100
	case 31:
		return 18000
	case 32:
		return 20480
	case 40, 41:
		return 32768
	case 42:
		return 34816
	case 50:
		return 110400
	case 51, 52:
		return 184320
	}
	return 696320
}

// maxBufferedFrames is the most frames any level lets a decoder hold.
const maxBufferedFrames = 16

// MaxDpbFrames is how many frames the level lets the decoded picture buffer hold
// at this picture size, 1 to 16 (A.3.1, item h).
func (s SPS) MaxDpbFrames() int {
	mbs := s.MBWidth * s.MBHeight
	if mbs == 0 {
		return 1
	}
	return int(min(max(s.maxDpbMbs()/mbs, 1), maxBufferedFrames))
}

// MaxNumReorder is how many pictures can precede another in decoding order and
// follow it in output order: max_num_reorder_frames when the VUI states it, the
// whole buffer when it does not.
func (s SPS) MaxNumReorder() int {
	if s.VUIPresent && s.VUI.BitstreamRestriction {
		return int(min(s.VUI.MaxNumReorderFrames, maxBufferedFrames))
	}
	return s.MaxDpbFrames()
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
	// Six constraint flags and two bits the format reserves. They are kept
	// (level 1b is told by one of them) and, before that, spent: skipping the
	// wrong number of bits is the classic way to report a plausible size for a
	// picture nobody encoded.
	s.ConstraintFlags = uint8(r.bits(8))
	s.Level = uint8(r.bits(8))
	s.ID = r.ue()

	s.ChromaFormat = 1 // 4:2:0 unless a high profile says otherwise
	s.BitDepthLuma, s.BitDepthChroma = 8, 8
	if highProfiles[s.Profile] {
		if err := s.readHighProfile(r); err != nil {
			return s, err
		}
	}

	// ⛔ A BIT COUNT: the slice header reads frame_num with it, so a reader
	// given a wrong one is misaligned from that field on.
	n, err := narrowed(r, "log2_max_frame_num_minus4", 12)
	if err != nil {
		return s, err
	}
	s.Log2MaxFrameNum = n + 4
	if err := s.readPicOrderCnt(r); err != nil {
		return s, err
	}
	s.MaxNumRefFrames = r.ue()
	s.GapsAllowed = r.flag()

	s.MBWidth = r.ue() + 1
	mapUnits := r.ue() + 1
	s.FrameMBSOnly = r.flag()
	if !s.FrameMBSOnly {
		s.MBAdaptive = r.flag()
		// A map unit is a field pair when frames are not the only coding, so the
		// picture is twice as tall in macroblocks as the set states.
		s.MBHeight = mapUnits * 2
	} else {
		s.MBHeight = mapUnits
	}
	s.Direct8x8 = r.flag()

	if r.flag() { // frame_cropping_flag
		s.CropLeft, s.CropRight = r.ue(), r.ue()
		s.CropTop, s.CropBottom = r.ue(), r.ue()
	}
	if r.err != nil {
		return s, r.err
	}
	s.size()

	// ⛔ The size is settled by here, so a VUI that is malformed does not take it
	// away: the set is returned with the error, as a high profile's is, and a caller
	// that wants the size alone may use it. The VUI is last, so nothing read after it
	// can be misaligned by it.
	if s.VUIPresent = r.flag(); s.VUIPresent {
		s.VUI = readVUI(r)
	}
	return s, r.err
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
	bdY, err := narrowed(r, "bit_depth_luma_minus8", 6)
	if err != nil {
		return err
	}
	bdC, err := narrowed(r, "bit_depth_chroma_minus8", 6)
	if err != nil {
		return err
	}
	s.BitDepthLuma, s.BitDepthChroma = bdY+8, bdC+8
	s.TransformBypass = r.flag()
	if s.ScalingPresent = r.flag(); s.ScalingPresent {
		// ⛔ The scaling matrices are the one variable-length part of an SPS, and
		// the reason a reader that skips them by a fixed amount reports a
		// plausible and wrong picture size. Six lists of sixteen, then two of
		// sixty-four -- or six of sixty-four when the chroma planes are coded at
		// full resolution.
		count := 8
		if s.ChromaFormat == 3 {
			count = 12
		}
		s.Scaling = readScalingLists(r, count)
	}
	return nil
}

// readPicOrderCnt reads the picture order count fields, whose shape depends on
// the type the set states, and keeps what a slice header needs of them.
func (s *SPS) readPicOrderCnt(r *sticky) error {
	s.POCType = r.ue()
	switch t := s.POCType; t {
	case 0:
		n, err := narrowed(r, "log2_max_pic_order_cnt_lsb_minus4", 12)
		if err != nil {
			return err
		}
		s.Log2MaxPOCLSB = n + 4
	case 1:
		s.POCAlwaysZero = r.flag()
		s.OffsetForNonRefPic = r.se()
		s.OffsetForTopToBottomField = r.se()
		// ⛔ A count of offsets follows, each one signed. This is the second
		// variable-length part of an SPS, and it is bounded: 255 is the most the
		// format allows, and a larger count read out of a misaligned stream
		// would otherwise spin through the rest of it.
		n := r.ue()
		if n > 255 {
			return fmt.Errorf("%w: %d offsets in the picture order cycle, at most 255", ErrUnsupportedSPS, n)
		}
		s.OffsetForRefFrame = make([]int32, n)
		for i := range s.OffsetForRefFrame {
			s.OffsetForRefFrame[i] = r.se()
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

// narrowed reads a field that is kept in a byte, and refuses it before the
// conversion that would narrow it.
//
// ⛔ A narrowing conversion does not merely lose a value, it maps out-of-range
// values ONTO LEGAL ONES: uint8(1048576) is 0, so a check made afterwards does
// not fail loudly, it PASSES. Every one of these is a width or a depth that
// something later reads a field with.
func narrowed(r *sticky, name string, hi uint32) (uint8, error) {
	v := r.ue()
	if r.err != nil {
		return 0, r.err
	}
	if v > hi {
		return 0, fmt.Errorf("%w: %s of %d, at most %d", ErrUnsupportedSPS, name, v, hi)
	}
	return uint8(v), nil
}
