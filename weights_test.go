// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "testing"

// TestTheScaleFactorIsDerivedOverItsWholeDomain.
//
// ⛔ The inputs are clipped to a byte each, so the whole domain is 256 x 256 =
// 65 536 pairs and can be walked. A handful of hand-picked cases would miss the
// arithmetic traps, which live at the signs and the boundaries rather than in the
// middle.
func TestTheScaleFactorIsDerivedOverItsWholeDomain(t *testing.T) {
	pairs, zero := 0, 0
	for td := int32(-128); td <= 127; td++ {
		for tb := int32(-128); tb <= 127; tb++ {
			// poc0 is fixed at zero, so tb and td ARE the distances.
			got := DistScaleFactor(tb, 0, td)
			pairs++
			if td == 0 {
				zero++
				if got != 0 {
					t.Fatalf("td=0 tb=%d gave %d, want 0", tb, got)
				}
				continue
			}
			// ⛔ The result is clipped, and nothing may leave the window.
			if got < -1024 || got > 1023 {
				t.Fatalf("tb=%d td=%d gave %d, outside [-1024, 1023]", tb, td, got)
			}
			// Against the formula written the other way round, as the spec states
			// it, so the test is not the implementation repeated.
			tx := (16384 + absRef(td/2)) / td
			want := clipRef(-1024, 1023, (tb*tx+32)>>6)
			if got != want {
				t.Fatalf("tb=%d td=%d gave %d, want %d", tb, td, got, want)
			}
		}
	}
	if pairs != 256*256 {
		t.Errorf("walked %d pairs, want %d", pairs, 256*256)
	}
	if zero != 256 {
		t.Errorf("%d pairs had equal references, want 256", zero)
	}
}

// absRef and clipRef are the format's own operators, written plainly so the test
// does not check the implementation against itself.
func absRef(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func clipRef(low, high, v int32) int32 {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}

// TestADivisionWouldGetTheNegativeFactorsWrong.
//
// ⛔ The shift is arithmetic: -1 >> 2 is -1 and -1 / 4 is 0. Of the 2048 factors
// the derivation can produce, 768 differ between the two -- every negative one,
// which is every reference pair the current picture does not sit between. This
// pins the shift so that nobody "simplifies" it into a division.
func TestADivisionWouldGetTheNegativeFactorsWrong(t *testing.T) {
	differ := 0
	for v := int32(-1024); v <= 1023; v++ {
		if v>>2 != v/4 {
			differ++
		}
	}
	if differ != 768 {
		t.Errorf("%d of 2048 factors differ between a shift and a division, want 768", differ)
	}
	// And the weights follow the shift. A reference pair 3 apart with the current
	// picture one BEFORE the first gives a negative factor.
	got := ImplicitWeighting(-1, 0, 3, false)
	want := 64 - (DistScaleFactor(-1, 0, 3) >> 2)
	if got.W0 != want {
		t.Errorf("W0 = %d, want %d: the weights do not follow the shift", got.W0, want)
	}
}

// TestTheWeightsAlwaysSumToSixtyFour.
//
// ⛔ Over the whole domain, including every fallback. The two weights share one
// denominator, so a pair that does not sum to 64 brightens or darkens the block --
// which is the kind of error that looks like a grading choice rather than a bug.
func TestTheWeightsAlwaysSumToSixtyFour(t *testing.T) {
	derived, fallback := 0, 0
	for td := int32(-128); td <= 127; td++ {
		for tb := int32(-128); tb <= 127; tb++ {
			w := ImplicitWeighting(tb, 0, td, false)
			if w.W0+w.W1 != 64 {
				t.Fatalf("tb=%d td=%d gave %d + %d", tb, td, w.W0, w.W1)
			}
			if w.LogDenom != 5 {
				t.Fatalf("tb=%d td=%d gave a denominator of %d, want 5", tb, td, w.LogDenom)
			}
			if w.O0 != 0 || w.O1 != 0 {
				t.Fatalf("tb=%d td=%d gave offsets %d and %d, want none", tb, td, w.O0, w.O1)
			}
			// ⛔ Which arm ran is NOT observable from the value: a derived
			// weighting of (32, 32) is identical to the plain average, and
			// tb=-65 td=-128 produces exactly that. The arms are counted by the
			// factor's window, which is what decides them.
			if td != 0 {
				factor := DistScaleFactor(tb, 0, td) >> 2
				if factor < -64 || factor > 128 {
					fallback++
				} else {
					derived++
				}
			} else {
				fallback++
			}
		}
	}
	// Both arms have to be reached, or the walk is proving one of them only.
	if derived == 0 || fallback == 0 {
		t.Fatalf("%d derived and %d fallbacks: one arm was never taken", derived, fallback)
	}
	t.Logf("over 65536 pairs: %d derived, %d fell back to the plain average", derived, fallback)
}

// TestTheFallbackWindowIsAsymmetric.
//
// ⛔ It is "below -64 or above 128", not a window around zero and not [0, 128].
// Guessing it symmetric sends every pair whose factor lands in (64, 128] to a plain
// average -- which is every pair where the current picture sits beyond the second
// reference, and those are the ones weighting helps most.
func TestTheFallbackWindowIsAsymmetric(t *testing.T) {
	for td := int32(-128); td <= 127; td++ {
		for tb := int32(-128); tb <= 127; tb++ {
			w := ImplicitWeighting(tb, 0, td, false)
			if td == 0 {
				continue
			}
			// ⛔ Asserting the VALUE, not which arm ran: a derived weighting of
			// (32, 32) is indistinguishable from the plain average, so inferring
			// the path from the result fails on tb=-65 td=-128 -- which it did.
			factor := DistScaleFactor(tb, 0, td) >> 2
			if want := weightsFromFactor(factor); w != want {
				t.Fatalf("tb=%d td=%d: factor %d gave %+v, want %+v", tb, td, factor, w, want)
			}
		}
	}
	// The boundaries themselves: 128 is in, 129 would be out; -64 is in, -65 out.
	// They are reached by naming the factor rather than by searching for it.
	for _, c := range []struct {
		factor int32
		inside bool
	}{{-65, false}, {-64, true}, {0, true}, {128, true}, {129, false}} {
		w := weightsFromFactor(c.factor)
		if (w != DefaultWeighting) != c.inside {
			t.Errorf("a factor of %d: weights %+v, inside=%v", c.factor, w, c.inside)
		}
	}
}

// weightsFromFactor is the second half of the derivation, applied to a factor
// stated directly. It is how the boundary cases are reached: searching the domain
// for a pair that produces exactly 129 would be a search, not a test.
func weightsFromFactor(factor int32) Weighting {
	if factor < -64 || factor > 128 {
		return DefaultWeighting
	}
	return Weighting{LogDenom: 5, W0: 64 - factor, W1: factor}
}

// TestEquidistantReferencesWeighEqually is the case a viewer would notice: a
// picture halfway between its two references takes half of each.
func TestEquidistantReferencesWeighEqually(t *testing.T) {
	for d := int32(1); d <= 60; d++ {
		w := ImplicitWeighting(d, 0, 2*d, false)
		if w.W0 != 32 || w.W1 != 32 {
			t.Errorf("halfway at distance %d: %d and %d, want 32 and 32", d, w.W0, w.W1)
		}
	}
	// And a picture nearer the first reference leans on it.
	near := ImplicitWeighting(1, 0, 8, false)
	if near.W0 <= near.W1 {
		t.Errorf("a picture next to the first reference weighs it %d against %d", near.W0, near.W1)
	}
	far := ImplicitWeighting(7, 0, 8, false)
	if far.W1 <= far.W0 {
		t.Errorf("a picture next to the second reference weighs it %d against %d", far.W1, far.W0)
	}
}

// TestALongTermReferenceIsNotScaled: the counts of a long-term picture do not
// measure a distance in time, so there is nothing to scale by.
func TestALongTermReferenceIsNotScaled(t *testing.T) {
	if got := ImplicitWeighting(1, 0, 8, true); got != DefaultWeighting {
		t.Errorf("with a long-term reference: %+v, want the plain average", got)
	}
	if got := ImplicitWeighting(1, 0, 8, false); got == DefaultWeighting {
		t.Error("without one, the weights were not derived")
	}
	// Two references at the same instant say nothing either.
	if got := ImplicitWeighting(1, 4, 4, false); got != DefaultWeighting {
		t.Errorf("two references at one instant: %+v, want the plain average", got)
	}
}

// TestAnEntryThatStatesNothingIsTheDefaultWeight.
//
// ⛔ Not a zero weight. A reader taking the zero value multiplies the prediction by
// nothing and shows a grey block -- and with weighted_pred_flag set, 8 of 18 P
// slices measured in one stream send a table whose entries state nothing at all.
func TestAnEntryThatStatesNothingIsTheDefaultWeight(t *testing.T) {
	table := &PredWeights{
		LumaLog2Denom: 6,
		L0:            []RefWeight{{}, {LumaStated: true, LumaWeight: 70, LumaOffset: -3}},
		L1:            []RefWeight{{LumaStated: true, LumaWeight: 50, LumaOffset: 2}},
	}
	// An entry that states nothing: the weight is one, in this denominator.
	silent := ExplicitWeighting(table, 0, -1)
	if silent.W0 != 64 || silent.O0 != 0 {
		t.Errorf("a silent entry gave weight %d offset %d, want 64 and 0", silent.W0, silent.O0)
	}
	if silent.LogDenom != 6 {
		t.Errorf("denominator %d, want 6", silent.LogDenom)
	}
	// One that states something.
	stated := ExplicitWeighting(table, 1, 0)
	if stated.W0 != 70 || stated.O0 != -3 || stated.W1 != 50 || stated.O1 != 2 {
		t.Errorf("a stated pair: %+v", stated)
	}
	// An index past the end leaves that side at the default rather than reading
	// out of range.
	past := ExplicitWeighting(table, 9, 9)
	if past.W0 != 64 || past.W1 != 64 {
		t.Errorf("past the end: %+v, want the default both sides", past)
	}
	// No table at all is the plain average.
	if got := ExplicitWeighting(nil, 0, 0); got != DefaultWeighting {
		t.Errorf("no table: %+v", got)
	}
}

// TestTheModeIsChosenInOnePlace: three modes, told apart once. Asking the question
// twice is how two parts of a decoder come to disagree about which one applies.
func TestTheModeIsChosenInOnePlace(t *testing.T) {
	table := &PredWeights{LumaLog2Denom: 5,
		L0: []RefWeight{{LumaStated: true, LumaWeight: 40}},
		L1: []RefWeight{{LumaStated: true, LumaWeight: 24}}}
	ref := SliceReferences{Weights: table}
	for _, c := range []struct {
		name string
		pps  PPS
		typ  SliceType
		want Weighting
	}{
		{"implicit B", PPS{WeightedBipredIDC: 2}, SliceB,
			ImplicitWeighting(1, 0, 8, false)},
		{"explicit B", PPS{WeightedBipredIDC: 1}, SliceB,
			Weighting{LogDenom: 5, W0: 40, W1: 24}},
		{"unweighted B", PPS{WeightedBipredIDC: 0}, SliceB, DefaultWeighting},
		{"weighted P", PPS{WeightedPred: true}, SliceP,
			Weighting{LogDenom: 5, W0: 40, W1: 32}},
		{"unweighted P", PPS{}, SliceP, DefaultWeighting},
		{"an I slice", PPS{WeightedPred: true}, SliceI, DefaultWeighting},
	} {
		got := WeightingFor(c.pps, SliceHeader{Type: c.typ}, ref, 1, 0, 8, 0, 0, false)
		if got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}
