// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"fmt"
)

// ErrScalingList means a scaling list states a value the format does not allow.
var ErrScalingList = errors.New("h264: invalid scaling list")

// ScalingLists are the scaling lists a parameter set states (7.3.2.1.1.1), in the
// order they were sent, which is zig-zag order.
//
// A list that is not Present is not stated: what stands in for it is the
// fall-back rule of Table 7-2, which depends on which parameter set it is, so it
// is the consumer's to apply, not guessed here. A list that is Present with
// UseDefault set stated "use the default list" (a first delta that brings the
// running value to zero, 7.4.2.1.1.1) and carries no coefficients.
type ScalingLists struct {
	List4x4       [6][16]uint8
	List8x8       [6][64]uint8
	Present4x4    [6]bool
	Present8x8    [6]bool
	UseDefault4x4 [6]bool
	UseDefault8x8 [6]bool
}

// readScalingLists reads count lists, the first six of sixteen coefficients and
// the rest of sixty-four. It is the one reader of the syntax, used by the sequence
// and the picture parameter set alike.
//
// ⛔ A list's deltas stop the moment the running value reaches zero: nothing more is
// stated for it, and the rest of the list repeats the last non-zero value. Reading
// all of them regardless would swallow the bits of whatever comes next, reading none
// would leave them behind, and either misaligns the rest of the unit with nothing to
// show for it.
func readScalingLists(r *sticky, count int) ScalingLists {
	var l ScalingLists
	for i := 0; i < count && r.err == nil; i++ {
		if !r.flag() {
			continue
		}
		if i < 6 {
			l.Present4x4[i] = true
			l.UseDefault4x4[i] = readScalingList(r, l.List4x4[i][:])
		} else {
			l.Present8x8[i-6] = true
			l.UseDefault8x8[i-6] = readScalingList(r, l.List8x8[i-6][:])
		}
	}
	return l
}

// readScalingList fills list and reports whether the stream asked for the default
// one instead.
func readScalingList(r *sticky, list []uint8) (useDefault bool) {
	last, next := int32(8), int32(8)
	for i := range list {
		if next != 0 {
			delta := r.se()
			// ⛔ 7.4.2.1.1.1 puts delta_scale in -128..127. A larger one is not
			// merely wrong: the remainder below is NEGATIVE for a negative sum, and
			// a negative value narrowed to a byte is a coefficient nobody stated.
			if r.err == nil && (delta < -128 || delta > 127) {
				r.err = fmt.Errorf("%w: delta_scale of %d", ErrScalingList, delta)
				return false
			}
			next = (last + delta + 256) % 256
			if i == 0 && next == 0 {
				useDefault = true
			}
		}
		if next == 0 {
			list[i] = uint8(last)
		} else {
			list[i] = uint8(next)
			last = next
		}
	}
	return useDefault
}

// zigzag4x4 and zigzag8x8 map the order scaling lists are sent in (zig-zag) to
// raster position, Table 8-12 for frame macroblocks.
var zigzag4x4 = [16]uint8{
	0, 1, 4, 8, 5, 2, 3, 6,
	9, 12, 13, 10, 7, 11, 14, 15,
}

var zigzag8x8 = [64]uint8{
	0, 1, 8, 16, 9, 2, 3, 10,
	17, 24, 32, 25, 18, 11, 4, 5,
	12, 19, 26, 33, 40, 48, 41, 34,
	27, 20, 13, 6, 7, 14, 21, 28,
	35, 42, 49, 56, 57, 50, 43, 36,
	29, 22, 15, 23, 30, 37, 44, 51,
	58, 59, 52, 45, 38, 31, 39, 46,
	53, 60, 61, 54, 47, 55, 62, 63,
}

// The default lists of Tables 7-3 and 7-4, as the tables print them (raster).
var (
	defaultRaster4x4Intra = [16]uint8{
		6, 13, 20, 28,
		13, 20, 28, 32,
		20, 28, 32, 37,
		28, 32, 37, 42,
	}
	defaultRaster4x4Inter = [16]uint8{
		10, 14, 20, 24,
		14, 20, 24, 27,
		20, 24, 27, 30,
		24, 27, 30, 34,
	}
	defaultRaster8x8Intra = [64]uint8{
		6, 10, 13, 16, 18, 23, 25, 27,
		10, 11, 16, 18, 23, 25, 27, 29,
		13, 16, 18, 23, 25, 27, 29, 31,
		16, 18, 23, 25, 27, 29, 31, 33,
		18, 23, 25, 27, 29, 31, 33, 36,
		23, 25, 27, 29, 31, 33, 36, 38,
		25, 27, 29, 31, 33, 36, 38, 40,
		27, 29, 31, 33, 36, 38, 40, 42,
	}
	defaultRaster8x8Inter = [64]uint8{
		9, 13, 15, 17, 19, 21, 22, 24,
		13, 13, 17, 19, 21, 22, 24, 25,
		15, 17, 19, 21, 22, 24, 25, 27,
		17, 19, 21, 22, 24, 25, 27, 28,
		19, 21, 22, 24, 25, 27, 28, 30,
		21, 22, 24, 25, 27, 28, 30, 32,
		22, 24, 25, 27, 28, 30, 32, 33,
		24, 25, 27, 28, 30, 32, 33, 35,
	}
)

// scan4x4 and scan8x8 put a raster list in the order it would be sent, zig-zag.
func scan4x4(raster [16]uint8) (out [16]uint8) {
	for i, p := range zigzag4x4 {
		out[i] = raster[p]
	}
	return out
}

func scan8x8(raster [64]uint8) (out [64]uint8) {
	for i, p := range zigzag8x8 {
		out[i] = raster[p]
	}
	return out
}

var (
	defaultScan4x4Intra = scan4x4(defaultRaster4x4Intra)
	defaultScan4x4Inter = scan4x4(defaultRaster4x4Inter)
	defaultScan8x8Intra = scan8x8(defaultRaster8x8Intra)
	defaultScan8x8Inter = scan8x8(defaultRaster8x8Inter)
)

// flat is what "no matrix" means: every coefficient weighs 16 (Flat_4x4_16).
func flatLists() (l4 [6][16]uint8, l8 [6][64]uint8) {
	for i := range l4 {
		for j := range l4[i] {
			l4[i][j] = 16
		}
	}
	for i := range l8 {
		for j := range l8[i] {
			l8[i][j] = 16
		}
	}
	return l4, l8
}

// resolve applies Table 7-2: a list that is stated and says "default" is the
// default list, one that is stated is itself, and one that is not falls back to
// the list before it (to fallbackIntra / fallbackInter for the first of each
// kind, which differ between the sequence set and the picture set).
func (l ScalingLists) resolve(n8 int, fb4Intra, fb4Inter [16]uint8, fb8Intra, fb8Inter [64]uint8) (l4 [6][16]uint8, l8 [6][64]uint8) {
	for i := 0; i < 6; i++ {
		switch {
		case l.Present4x4[i] && l.UseDefault4x4[i]:
			l4[i] = defaultScan4x4Intra
			if i >= 3 {
				l4[i] = defaultScan4x4Inter
			}
		case l.Present4x4[i]:
			l4[i] = l.List4x4[i]
		case i == 0:
			l4[0] = fb4Intra
		case i == 3:
			l4[3] = fb4Inter
		default:
			l4[i] = l4[i-1]
		}
	}
	for i := 0; i < n8; i++ {
		switch {
		case l.Present8x8[i] && l.UseDefault8x8[i]:
			l8[i] = defaultScan8x8Intra
			if i%2 == 1 {
				l8[i] = defaultScan8x8Inter
			}
		case l.Present8x8[i]:
			l8[i] = l.List8x8[i]
		case i == 0:
			l8[0] = fb8Intra
		case i == 1:
			l8[1] = fb8Inter
		default:
			l8[i] = l8[i-2]
		}
	}
	_, flat8 := flatLists()
	for i := n8; i < 6; i++ {
		l8[i] = flat8[i]
	}
	return l4, l8
}

// eightByEight is how many 8x8 lists the chroma format has: two, or six at full
// chroma resolution.
func eightByEight(sps SPS) int {
	if sps.ChromaFormat == 3 {
		return 6
	}
	return 2
}

// ResolvedScaling are the six 4x4 and six 8x8 scaling lists the sequence set
// leaves in force, in the order they were sent (zig-zag), with Table 7-2's
// fall-back applied (Table 7-3 and 7-4's defaults, or the list before). A set
// that states no matrix, and a profile that cannot, gives the flat lists.
func (s SPS) ResolvedScaling() (l4 [6][16]uint8, l8 [6][64]uint8) {
	if !highProfiles[s.Profile] || !s.ScalingPresent {
		return flatLists()
	}
	return s.Scaling.resolve(eightByEight(s), defaultScan4x4Intra, defaultScan4x4Inter, defaultScan8x8Intra, defaultScan8x8Inter)
}

// ResolvedScaling is the same for a picture set, which falls back on the sequence
// set's resolved lists when it states none of its own.
//
// For a list it omits (pic_scaling_list_present_flag 0), H.264 (V16) 7.4.2.2 says
// rule set A of Table 7-2 applies when seq_scaling_matrix_present_flag is 0 and
// rule set B when it is 1. Per the table, in set A the first list of each kind
// (indices 0, 3, 6, 7) falls back to its Default_* list, and in set B to the
// "sequence-level scaling list" of the SAME index: index 6 (Sl_8x8_Intra_Y) is
// our 8x8 list 0 and index 7 (Sl_8x8_Inter_Y) our 8x8 list 1. The other lists
// fall back to the list before (set A and B alike).
func (p PPS) ResolvedScaling(sps SPS) (l4 [6][16]uint8, l8 [6][64]uint8) {
	seq := highProfiles[sps.Profile] && sps.ScalingPresent
	var s4 [6][16]uint8
	var s8 [6][64]uint8
	if seq {
		s4, s8 = sps.ResolvedScaling()
	}
	if !p.ScalingPresent {
		if seq {
			return s4, s8
		}
		return flatLists()
	}
	fb4Intra, fb4Inter, fb8Intra, fb8Inter := defaultScan4x4Intra, defaultScan4x4Inter, defaultScan8x8Intra, defaultScan8x8Inter
	if seq {
		fb4Intra, fb4Inter, fb8Intra, fb8Inter = s4[0], s4[3], s8[0], s8[1]
	}
	n8 := 0
	if p.Transform8x8 {
		n8 = eightByEight(sps)
	}
	return p.Scaling.resolve(n8, fb4Intra, fb4Inter, fb8Intra, fb8Inter)
}
