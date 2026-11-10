package loopfilter

import (
	"math/rand/v2"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

// internalEdgesUnfiltered is a shortcut for "edgeStrength is 0 on every edge
// inside the macroblock". Whatever the macroblock, if it says true then the
// full computation must agree; if it says false nothing is claimed.
func TestInternalEdgesUnfilteredAgreesWithEdgeStrength(t *testing.T) {
	refs := []*frame.Picture{nil, {}, {}}
	rng := rand.New(rand.NewPCG(1, 2))
	shortcut := 0
	for n := 0; n < 200000; n++ {
		m := &MB{Intra: rng.IntN(8) == 0, IPCM: rng.IntN(40) == 0, Bipredictive: rng.IntN(2) == 0, Transform8x8: rng.IntN(2) == 0}
		// Mostly uniform macroblocks, so the shortcut is taken often enough to test.
		base := func() (*frame.Picture, [2]int16) {
			return refs[rng.IntN(len(refs))], [2]int16{int16(rng.IntN(9) - 4), int16(rng.IntN(9) - 4)}
		}
		r0, v0 := base()
		r1, v1 := base()
		for b := 0; b < 16; b++ {
			m.RefPicL0[b], m.MvL0[b], m.RefPicL1[b], m.MvL1[b] = r0, v0, r1, v1
			if rng.IntN(12) == 0 {
				m.RefPicL0[b], m.MvL0[b] = base()
			}
			if rng.IntN(12) == 0 {
				m.RefPicL1[b], m.MvL1[b] = base()
			}
			if rng.IntN(30) == 0 {
				m.NzY[b] = 1
			}
		}
		if !m.internalEdgesUnfiltered() {
			continue
		}
		shortcut++
		for y := 0; y < 16; y += 4 {
			for x := 0; x < 16; x += 4 {
				q := BlkIdxAt(x, y)
				if x > 0 {
					if s := edgeStrength(m, m, BlkIdxAt(x-4, y), q, false); s != 0 {
						t.Fatalf("vertical edge at x=%d y=%d has strength %d, shortcut said 0: %+v", x, y, s, m)
					}
				}
				if y > 0 {
					if s := edgeStrength(m, m, BlkIdxAt(x, y-4), q, false); s != 0 {
						t.Fatalf("horizontal edge at x=%d y=%d has strength %d, shortcut said 0: %+v", x, y, s, m)
					}
				}
			}
		}
	}
	if shortcut < 1000 {
		t.Fatalf("the shortcut was taken only %d times; the test does not reach it", shortcut)
	}
}
