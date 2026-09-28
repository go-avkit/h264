// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrNotPPS means the unit handed over is not a picture parameter set.
var ErrNotPPS = errors.New("h264: unit is not a picture parameter set")

// ErrUnsupportedPPS means the set does not end where a set must end, so
// something in it was read wrongly.
var ErrUnsupportedPPS = errors.New("h264: picture parameter set was not consumed exactly")

// ErrSliceGroups means the set uses more than one slice group, whose map this
// reader cannot consume.
var ErrSliceGroups = errors.New("h264: slice groups are not supported")

// PPS is what a picture parameter set says about the slices that refer to it.
//
// CABAC is the field everything above this turns on: the residuals of a slice are
// coded one way or the other and nothing can read them without knowing which.
type PPS struct {
	ID                   uint32
	SPSID                uint32
	CABAC                bool // entropy_coding_mode_flag
	BottomFieldPicOrder  bool
	NumRefIdxL0          uint32
	NumRefIdxL1          uint32
	WeightedPred         bool
	WeightedBipredIDC    uint8
	InitQP               int32 // already un-biased: pic_init_qp_minus26 + 26
	InitQS               int32
	ChromaQPIndexOffset  int32
	DeblockingControl    bool
	ConstrainedIntraPred bool
	RedundantPicCnt      bool
	Transform8x8         bool
	SecondChromaQPOffset int32 // equals ChromaQPIndexOffset when not stated
}

// ParsePPS reads a picture parameter set.
//
// ⛔ It needs the sequence parameter set the PPS refers to, and not for
// convenience: the number of 8x8 scaling lists a PPS carries depends on the
// SPS's chroma format, so a reader without it cannot consume them exactly and
// everything after them is read from the wrong bits. The caller holds the SPS
// already -- a stream states it first -- so this asks for it rather than
// guessing.
func ParsePPS(u Unit, sps SPS) (PPS, error) {
	if u.Type != UnitPPS {
		return PPS{}, fmt.Errorf("%w: type %d", ErrNotPPS, u.Type)
	}
	r := newSticky(u.Unescape())
	var p PPS

	p.ID = r.ue()
	p.SPSID = r.ue()
	p.CABAC = r.flag()
	p.BottomFieldPicOrder = r.flag()

	if groups := r.ue(); groups > 0 {
		// The map that follows takes one of seven shapes, several of them a list
		// as long as the picture is in macroblocks. Refusing is the honest answer:
		// a reader that skipped a fixed amount would misread every field after
		// it and report a quantiser and a deblocking setting that were never
		// written. Slice groups are absent from every profile in ordinary use.
		return p, fmt.Errorf("%w: %d groups", ErrSliceGroups, groups+1)
	}

	p.NumRefIdxL0 = r.ue() + 1
	p.NumRefIdxL1 = r.ue() + 1
	p.WeightedPred = r.flag()
	p.WeightedBipredIDC = uint8(r.bits(2))
	p.InitQP = r.se() + 26
	p.InitQS = r.se() + 26
	p.ChromaQPIndexOffset = r.se()
	p.DeblockingControl = r.flag()
	p.ConstrainedIntraPred = r.flag()
	p.RedundantPicCnt = r.flag()

	// The second chroma offset defaults to the first when the set stops here,
	// which is what a decoder must assume rather than zero.
	p.SecondChromaQPOffset = p.ChromaQPIndexOffset

	// ⛔ Whether anything follows is decided by what is LEFT, not by the profile:
	// the trailing part is optional and a set that omits it ends here. A reader
	// that read it anyway would take the trailing bits for fields.
	if r.moreData() {
		p.Transform8x8 = r.flag()
		if r.flag() { // pic_scaling_matrix_present_flag
			// Six lists of sixteen always, and the eights only when 8x8
			// transforms are in use -- two of them, or six when the chroma
			// planes are coded at full resolution.
			for i := 0; i < 6; i++ {
				skipScalingList(r, 16)
			}
			if p.Transform8x8 {
				eights := 2
				if sps.ChromaFormat == 3 {
					eights = 6
				}
				for i := 0; i < eights; i++ {
					skipScalingList(r, 64)
				}
			}
		}
		p.SecondChromaQPOffset = r.se()
	}
	if r.err != nil {
		return p, r.err
	}
	// ⛔ A picture parameter set ends with the bits that end every raw byte
	// sequence payload -- a one, then zeros to the byte. Landing anywhere else
	// means a field was consumed wrongly, and the values above were read from
	// the wrong bits. There is nothing else in the set to check them against, so
	// this identity IS the check: it is what turns a misread variable-length part
	// from a plausible quantiser into a refusal.
	if r.moreData() {
		return p, fmt.Errorf("%w: %d bits are left over, so a field was read wrongly",
			ErrUnsupportedPPS, r.r.Left())
	}
	return p, nil
}
