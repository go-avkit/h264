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
	// half of the quadrants carry one prediction, as a partition does
	for q := 0; q < 4; q++ {
		if r.Intn(2) == 0 {
			continue
		}
		b := q * 4
		for i := 1; i < 4; i++ {
			m.RefPicL0[b+i], m.MvL0[b+i] = m.RefPicL0[b], m.MvL0[b]
			m.RefPicL1[b+i], m.MvL1[b+i] = m.RefPicL1[b], m.MvL1[b]
		}
	}
	return m
}

// TestStrengthsMatchPerGroup pins mbStrengths.compute (every strength of a
// macroblock in one pass, from masks) to edgeStrength, the per-group
// transcription of 8.7.2.1, on every edge of random macroblocks in both
// directions, with and without the neighbours and the uniform-quadrant
// shortcut, and with the edges the filter visits (step, last) as it picks them.
func TestStrengthsMatchPerGroup(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	refs := []*frame.Picture{nil, {}, {}}
	for n := 0; n < 30000; n++ {
		cur, lm, tm := randomMB(r, refs), randomMB(r, refs), randomMB(r, refs)
		var left, top *MB
		if r.Intn(4) != 0 {
			left = lm
		}
		if r.Intn(4) != 0 {
			top = tm
		}
		step, last := 1, 4
		if cur.Transform8x8 {
			step = 2
		}
		if r.Intn(5) == 0 {
			last = 1
		}
		var uni uint8
		if !cur.Intra && last > 1 && step == 1 && r.Intn(4) != 0 {
			uni = quadUniform(cur)
		}
		var lnz, tnz uint16
		if left != nil {
			lnz = nzMask(left)
		}
		if top != nil {
			tnz = nzMask(top)
		}
		var s mbStrengths
		s.compute(cur, left, top, nzMask(cur), lnz, tnz, uni, last, step)
		for e := 0; e < 4; e++ {
			for dir := 0; dir < 2; dir++ {
				var want [4]uint8
				wantAny := false
				visited := e < last && e%step == 0
				p := cur
				if e == 0 {
					p = left
					if dir == 1 {
						p = top
					}
				}
				if visited && p != nil {
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
				}
				if got := s.bs[dir][e]; got != want || (s.any[dir]>>uint(e)&1 != 0) != wantAny {
					t.Fatalf("n=%d e=%d dir=%d: got %v/%v, want %v/%v", n, e, dir, got, s.any[dir]>>uint(e)&1, want, wantAny)
				}
			}
		}
	}
}

// TestNzMaskIsRaster pins the bit order of nzMask: bit row*4+col is the
// lumaNonZero of the block at that raster position.
func TestNzMaskIsRaster(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	refs := []*frame.Picture{nil, {}}
	for n := 0; n < 2000; n++ {
		m := randomMB(r, refs)
		got := nzMask(m)
		for row := 0; row < 4; row++ {
			for col := 0; col < 4; col++ {
				want := lumaNonZero(m, BlkIdxAt(col*4, row*4))
				if (got>>uint(row*4+col)&1 != 0) != want {
					t.Fatalf("n=%d row=%d col=%d: mask %#x, want %v", n, row, col, got, want)
				}
			}
		}
	}
}
