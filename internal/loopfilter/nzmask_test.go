package loopfilter

import (
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

func randomMB(r *rand.Rand, refs []*frame.Picture) *MB {
	m := &MB{Decoded: true, Intra: r.Intn(6) == 0, IPCM: r.Intn(12) == 0,
		Transform8x8: r.Intn(2) == 0, Bipredictive: r.Intn(2) == 0}
	for i := range m.NzY {
		if r.Intn(4) == 0 {
			m.NzY[i] = uint8(1 + r.Intn(3))
		}
	}
	for i := range m.MvL0 {
		m.RefPicL0[i] = refs[r.Intn(len(refs))]
		m.RefPicL1[i] = refs[r.Intn(len(refs))]
		for c := 0; c < 2; c++ {
			m.MvL0[i][c] = int16(r.Intn(9) - 4)
			m.MvL1[i][c] = int16(r.Intn(9) - 4)
		}
	}
	return m
}

// TestEdgeStrengthsMatchesPerGroup pins edgeStrengths (one mask per
// macroblock, four groups per call) to edgeStrength, the per-group
// transcription of 8.7.2.1, on every internal and macroblock edge of random
// macroblocks, in both directions.
func TestEdgeStrengthsMatchesPerGroup(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	refs := []*frame.Picture{nil, {}, {}}
	for n := 0; n < 20000; n++ {
		cur, nb := randomMB(r, refs), randomMB(r, refs)
		cnz, nnz := nzMask(cur), nzMask(nb)
		for e := 0; e < 4; e++ {
			for dir := 0; dir < 2; dir++ {
				p, pnz, pr := cur, cnz, e-1
				if e == 0 {
					p, pnz, pr = nb, nnz, 3
				}
				var got [4]uint8
				var any bool
				if dir == 0 {
					got, any = edgeStrengths(p, cur, pnz, cnz, pr, e, 4, e == 0)
				} else {
					got, any = edgeStrengths(p, cur, pnz, cnz, pr*4, e*4, 1, e == 0)
				}
				var want [4]uint8
				wantAny := false
				for g := 0; g < 4; g++ {
					x, y, px, py := e*4, g*4, e*4-4, g*4
					if e == 0 {
						px = 12
					}
					if dir == 1 {
						x, y, px, py = g*4, e*4, g*4, e*4-4
						if e == 0 {
							py = 12
						}
					}
					want[g] = edgeStrength(p, cur, BlkIdxAt(px, py), BlkIdxAt(x, y), e == 0)
					wantAny = wantAny || want[g] != 0
				}
				if got != want || any != wantAny {
					t.Fatalf("n=%d e=%d dir=%d: got %v/%v, want %v/%v", n, e, dir, got, any, want, wantAny)
				}
			}
		}
	}
}
