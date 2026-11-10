// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrVUI means the video usability information states something the format does
// not allow, or ends before it should.
var ErrVUI = errors.New("h264: invalid VUI")

// extendedSAR is aspect_ratio_idc's escape: the aspect ratio follows in 32 bits.
const extendedSAR = 255

// VUI is the part of the video usability information (Annex E) a decoder or a
// player acts on. What else the structure carries is read past, field by field,
// because the bitstream restriction at its end is what a decoder needs.
type VUI struct {
	// AspectRatioIDC is aspect_ratio_idc, and SARWidth and SARHeight the ratio when
	// it is 255 (Extended_SAR). Zero IDC with AspectRatio false means not stated.
	AspectRatio             bool
	AspectRatioIDC          uint8
	SARWidth, SARHeight     uint16
	VideoSignal             bool
	FullRange               bool
	ColourPrimaries         uint8 // 2 (unspecified) when not stated
	TransferCharacteristics uint8
	MatrixCoefficients      uint8
	Timing                  bool
	NumUnitsInTick          uint32
	TimeScale               uint32
	FixedFrameRate          bool

	// BitstreamRestriction is bitstream_restriction_flag: the sequence states how
	// many pictures it reorders and how many frames it needs buffered. A decoder
	// that ignores it must assume the level's worst case, which is the larger delay.
	BitstreamRestriction bool
	MaxNumReorderFrames  uint32
	MaxDecFrameBuffering uint32
}

// maxHRDCount bounds cpb_cnt_minus1, E.2.2: 0..31.
const maxHRDCount = 31

// readVUI reads vui_parameters (E.1.1). A structure that is cut short, or whose
// hypothetical reference decoder states more than 32 buffers, sets the reader's
// error and is refused.
func readVUI(r *sticky) VUI {
	var v VUI
	if v.AspectRatio = r.flag(); v.AspectRatio {
		v.AspectRatioIDC = uint8(r.bits(8))
		if v.AspectRatioIDC == extendedSAR {
			v.SARWidth, v.SARHeight = uint16(r.bits(16)), uint16(r.bits(16))
		}
	}
	if r.flag() { // overscan_info_present_flag
		r.bit() // overscan_appropriate_flag
	}
	v.ColourPrimaries, v.TransferCharacteristics, v.MatrixCoefficients = 2, 2, 2
	if v.VideoSignal = r.flag(); v.VideoSignal {
		r.bits(3) // video_format
		v.FullRange = r.flag()
		if r.flag() { // colour_description_present_flag
			v.ColourPrimaries = uint8(r.bits(8))
			v.TransferCharacteristics = uint8(r.bits(8))
			v.MatrixCoefficients = uint8(r.bits(8))
		}
	}
	if r.flag() { // chroma_loc_info_present_flag
		r.ue() // chroma_sample_loc_type_top_field
		r.ue() // chroma_sample_loc_type_bottom_field
	}
	if v.Timing = r.flag(); v.Timing {
		v.NumUnitsInTick = r.bits(32)
		v.TimeScale = r.bits(32)
		v.FixedFrameRate = r.flag()
	}
	nal := r.flag() // nal_hrd_parameters_present_flag
	if nal {
		readHRD(r)
	}
	vcl := r.flag() // vcl_hrd_parameters_present_flag
	if vcl {
		readHRD(r)
	}
	if nal || vcl {
		r.bit() // low_delay_hrd_flag
	}
	r.bit() // pic_struct_present_flag
	if v.BitstreamRestriction = r.flag(); v.BitstreamRestriction {
		r.bit() // motion_vectors_over_pic_boundaries_flag
		r.ue()  // max_bytes_per_pic_denom
		r.ue()  // max_bits_per_mb_denom
		r.ue()  // log2_max_mv_length_horizontal
		r.ue()  // log2_max_mv_length_vertical
		v.MaxNumReorderFrames = r.ue()
		v.MaxDecFrameBuffering = r.ue()
	}
	if r.err != nil {
		r.err = fmt.Errorf("%w: %w", ErrVUI, r.err)
	}
	return v
}

// readHRD reads hrd_parameters (E.1.2).
func readHRD(r *sticky) {
	n := r.ue() // cpb_cnt_minus1
	if r.err == nil && n > maxHRDCount {
		r.err = fmt.Errorf("%w: cpb_cnt_minus1 of %d, at most %d", ErrVUI, n, maxHRDCount)
		return
	}
	r.bits(4) // bit_rate_scale
	r.bits(4) // cpb_size_scale
	for i := uint32(0); i <= n && r.err == nil; i++ {
		r.ue()  // bit_rate_value_minus1
		r.ue()  // cpb_size_value_minus1
		r.bit() // cbr_flag
	}
	r.bits(5) // initial_cpb_removal_delay_length_minus1
	r.bits(5) // cpb_removal_delay_length_minus1
	r.bits(5) // dpb_output_delay_length_minus1
	r.bits(5) // time_offset_length
}
