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
