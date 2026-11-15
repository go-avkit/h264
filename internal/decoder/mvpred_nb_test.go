package decoder

import (
	"math/rand"
	"testing"
)

func randomMotionMB(r *rand.Rand) *mbState {
	m := &mbState{}
	for i := 0; i < 16; i++ {
		m.refIdxL0[i], m.refIdxL1[i] = int8(r.Intn(4)-1), int8(r.Intn(4)-1)
		for c := 0; c < 2; c++ {
			m.MvL0[i][c], m.MvL1[i][c] = int16(r.Intn(41)-20), int16(r.Intn(41)-20)
		}
	}
	return m
}

// TestNeighboursForMatchesGeneric pins the direct neighbour lookup to the
// generic one (4 neighbourMotion calls) for every partition position and
// width, both lists, with every combination of absent neighbour macroblocks.
func TestNeighboursForMatchesGeneric(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for n := 0; n < 200; n++ {
		d := &sliceDecoder{cur: randomMotionMB(r)}
		pick := func(bit int) *mbState {
			if n>>uint(bit)&1 == 0 {
				return nil
			}
			return randomMotionMB(r)
		}
		d.nb.left, d.nb.top, d.nb.topLeft, d.nb.topRight = pick(0), pick(1), pick(2), pick(3)
		for list := 0; list < 2; list++ {
			for y := 0; y < 16; y += 4 {
				for x := 0; x < 16; x += 4 {
					for w := 4; x+w <= 16; w += 4 {
						ga, gb, gc := d.neighboursFor(list, x, y, w)
						wa, wb, wc := d.neighboursForRef(list, x, y, w)
						if ga != wa || gb != wb || gc != wc {
							t.Fatalf("n=%d list=%d (%d,%d) w=%d: got %v %v %v, want %v %v %v", n, list, x, y, w, ga, gb, gc, wa, wb, wc)
						}
					}
				}
			}
		}
	}
}
