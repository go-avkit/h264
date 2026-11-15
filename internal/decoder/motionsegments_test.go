package decoder

import (
	"math/rand"
	"reflect"
	"testing"
)

// The uniform-macroblock shortcut of motionSegments must give exactly what the
// merging of motionSegmentsMerged gives: on macroblocks whose sixteen blocks
// share one motion (the shortcut) and on every other pattern (the fallback).
func TestMotionSegmentsShortcutEqualsMerging(t *testing.T) {
	rng := rand.New(rand.NewSource(61))
	pick := func(r *rand.Rand) (mv0, mv1 [2]int16, r0, r1 int8) {
		mv0 = [2]int16{int16(r.Intn(5) - 2), int16(r.Intn(5) - 2)}
		mv1 = [2]int16{int16(r.Intn(5) - 2), int16(r.Intn(5) - 2)}
		return mv0, mv1, int8(r.Intn(3) - 1), int8(r.Intn(3) - 1)
	}
	for i := 0; i < 20000; i++ {
		var m mbState
		mv0, mv1, r0, r1 := pick(rng)
		for b := 0; b < 16; b++ {
			m.MvL0[b], m.MvL1[b], m.refIdxL0[b], m.refIdxL1[b] = mv0, mv1, r0, r1
		}
		switch rng.Intn(4) {
		case 0: // uniform
		case 1: // one block differs
			b := rng.Intn(16)
			m.MvL0[b], m.MvL1[b], m.refIdxL0[b], m.refIdxL1[b] = pick(rng)
		case 2: // a few blocks differ
			for k := 0; k < 1+rng.Intn(6); k++ {
				b := rng.Intn(16)
				m.MvL0[b], m.MvL1[b], m.refIdxL0[b], m.refIdxL1[b] = pick(rng)
			}
		default: // every block independent
			for b := 0; b < 16; b++ {
				m.MvL0[b], m.MvL1[b], m.refIdxL0[b], m.refIdxL1[b] = pick(rng)
			}
		}
		got := motionSegments(&m, nil)
		want := motionSegmentsMerged(&m, nil)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("round %d: shortcut %+v, merging %+v", i, got, want)
		}
	}
}
