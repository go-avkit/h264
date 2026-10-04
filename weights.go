// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

// Weighting is how a decoder combines one or two predictions into a block.
//
// A sample is (w0*p0 + w1*p1 + ((o0+o1+1) << LogDenom)) >> (LogDenom+1) for a
// bi-predicted block, and (w0*p0 + (1 << (LogDenom-1))) >> LogDenom, plus o0, for
// a single prediction. The weights are held rather than applied here: this package
// reads and derives, and the arithmetic belongs where the samples are.
type Weighting struct {
	// LogDenom is luma_log2_weight_denom, or 5 under implicit weighting.
	LogDenom int32
	// W0 and W1 are the weights of the two predictions, and O0 and O1 their
	// offsets. A single prediction uses W0 and O0 alone.
	W0, W1 int32
	O0, O1 int32
}

// DefaultWeighting is the plain average of two predictions, which is what the
// format falls back to whenever a derivation cannot be trusted.
var DefaultWeighting = Weighting{LogDenom: 5, W0: 32, W1: 32}

// clip3 is the format's own clamp, written once because it appears in every
// derivation below and a hand-inlined version of it is where an off-by-one hides.
func clip3(low, high, v int32) int32 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// DistScaleFactor is how far the current picture sits between two references,
// scaled so that 128 means "all the way to the second".
//
// It is the quantity two different parts of the format are built on -- the weights
// of implicit bi-prediction and the motion a temporal-direct block inherits -- so
// it is derived once here rather than twice at the places that need it.
//
// ⛔ Every shift here is an ARITHMETIC shift, and the distances are signed. In Go
// as in the format, -1 >> 2 is -1, while -1 / 4 is 0: the shift floors and the
// division truncates toward zero. Written with a division, 768 of the 2048
// factors this can produce come out wrong -- every negative one, which is every
// reference pair the current picture does not sit between.
//
// (The halving inside the absolute value is NOT such a trap: Abs(td/2) and
// Abs(td)/2 agree on all 256 values td can take, because truncation toward zero
// makes them the same. Checked, rather than assumed, before this was written.)
func DistScaleFactor(currPOC, poc0, poc1 int32) int32 {
	tb := clip3(-128, 127, currPOC-poc0)
	td := clip3(-128, 127, poc1-poc0)
	if td == 0 {
		// Two references at the same instant say nothing about where the current
		// picture sits between them.
		return 0
	}
	tx := (16384 + abs32(td/2)) / td
	return clip3(-1024, 1023, (tb*tx+32)>>6)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ImplicitWeighting derives the weights for a bi-predicted block when the picture
// set asks for implicit weighting -- weighted_bipred_idc of two, where no weight
// table is sent and the weights come from how far the current picture sits between
// its two references.
//
// eitherLongTerm says whether either reference is a long-term picture. The counts
// of a long-term picture do not measure a distance in time, so the format does not
// scale by them.
//
// ⛔ The fallback is not an error path. The format falls back to a plain average
// whenever the scaled distance lands outside its own window, and a decoder that
// instead used the out-of-range number would weight one reference by more than
// everything -- visible, and on exactly the pictures that are furthest from their
// references.
func ImplicitWeighting(currPOC, poc0, poc1 int32, eitherLongTerm bool) Weighting {
	if eitherLongTerm || poc1 == poc0 {
		return DefaultWeighting
	}
	w1 := DistScaleFactor(currPOC, poc0, poc1) >> 2
	// ⛔ The window is asymmetric: below -64 or above 128 is out. It is not
	// [-64, 64] and not [0, 128].
	if w1 < -64 || w1 > 128 {
		return DefaultWeighting
	}
	return Weighting{LogDenom: 5, W0: 64 - w1, W1: w1}
}

// ExplicitWeighting is the weighting a slice's own table states for one pair of
// references, or for a single one.
//
// ⛔ An entry that states nothing is the DEFAULT weight, not a zero weight. A
// reader taking the zero value would multiply the prediction by nothing and show
// a grey block.
func ExplicitWeighting(w *PredWeights, idx0, idx1 int) Weighting {
	out := Weighting{LogDenom: 5}
	if w == nil {
		return DefaultWeighting
	}
	out.LogDenom = int32(w.LumaLog2Denom)
	unit := int32(1) << w.LumaLog2Denom
	out.W0, out.W1 = unit, unit
	if idx0 >= 0 && idx0 < len(w.L0) {
		if e := w.L0[idx0]; e.LumaStated {
			out.W0, out.O0 = e.LumaWeight, e.LumaOffset
		}
	}
	if idx1 >= 0 && idx1 < len(w.L1) {
		if e := w.L1[idx1]; e.LumaStated {
			out.W1, out.O1 = e.LumaWeight, e.LumaOffset
		}
	}
	return out
}

// WeightingFor gives the weighting a bi-predicted block uses, whichever mode the
// picture set asked for.
//
// It is the question a decoder actually has, and asking it in one place keeps the
// three modes from being told apart differently in two.
func WeightingFor(pps PPS, h SliceHeader, ref SliceReferences,
	currPOC, poc0, poc1 int32, idx0, idx1 int, eitherLongTerm bool) Weighting {
	switch {
	case h.Type == SliceB && pps.WeightedBipredIDC == 2:
		return ImplicitWeighting(currPOC, poc0, poc1, eitherLongTerm)
	case h.Type == SliceB && pps.WeightedBipredIDC == 1:
		return ExplicitWeighting(ref.Weights, idx0, idx1)
	case (h.Type == SliceP || h.Type == SliceSP) && pps.WeightedPred:
		return ExplicitWeighting(ref.Weights, idx0, -1)
	default:
		return DefaultWeighting
	}
}
