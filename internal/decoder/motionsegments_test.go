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

// A macroblock type that fixes its partitions gives its segments without
// comparing blocks. Per 4x4 block, those segments must carry the motion that
// the merging of the blocks carries, and tile the macroblock exactly once.
func TestMotionSegmentsFromTheTypeEqualMerging(t *testing.T) {
	type bm struct {
		mv  [2][2]int16
		ref [2]int8
	}
	perBlock := func(segs []motionSegment) (out [4][4]bm, covered [4][4]int) {
		for _, s := range segs {
			for y := s.y; y < s.y+s.h; y += 4 {
				for x := s.x; x < s.x+s.w; x += 4 {
					out[y/4][x/4] = bm{s.mv, s.ref}
					covered[y/4][x/4]++
				}
			}
		}
		return
	}
	// halves: the 4x4 blocks (in raster) of each partition of the type.
	cases := []struct {
		name  string
		kinds []int
		parts [][]int
	}{
		{"16x16", []int{mbTypePSkip, mbTypeP16x16, mbTypeB16x16}, [][]int{{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}}},
		{"16x8", []int{mbTypeP16x8, mbTypeB16x8}, [][]int{{0, 1, 2, 3, 4, 5, 6, 7}, {8, 9, 10, 11, 12, 13, 14, 15}}},
		{"8x16", []int{mbTypeP8x16, mbTypeB8x16}, [][]int{{0, 1, 4, 5, 8, 9, 12, 13}, {2, 3, 6, 7, 10, 11, 14, 15}}},
	}
	rng := rand.New(rand.NewSource(7))
	for _, c := range cases {
		for _, kind := range c.kinds {
			for round := 0; round < 500; round++ {
				var m mbState
				m.kind = kind
				for _, blocks := range c.parts {
					mv0 := [2]int16{int16(rng.Intn(3)), int16(rng.Intn(3))}
					mv1 := [2]int16{int16(rng.Intn(3)), int16(rng.Intn(3))}
					r0, r1 := int8(rng.Intn(3)-1), int8(rng.Intn(3)-1)
					for _, raster := range blocks {
						z := zscanOf[raster/4][raster%4]
						m.MvL0[z], m.MvL1[z], m.refIdxL0[z], m.refIdxL1[z] = mv0, mv1, r0, r1
					}
				}
				got, cov := perBlock(motionSegments(&m, nil))
				want, _ := perBlock(motionSegmentsMerged(&m, nil))
				if got != want {
					t.Fatalf("%s kind %d round %d: per-block motion differs from merging", c.name, kind, round)
				}
				for y := range cov {
					for x := range cov[y] {
						if cov[y][x] != 1 {
							t.Fatalf("%s kind %d: block (%d,%d) covered %d times", c.name, kind, x, y, cov[y][x])
						}
					}
				}
			}
		}
	}
}
