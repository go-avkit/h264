package decoder

import (
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

// staleSlot is a slot as the previous picture left it: decoded, with motion.
func staleSlot(m *mbState, seed int8, intra bool) {
	*m = mbState{}
	m.Decoded = true
	m.Intra = intra
	m.SliceID = 3
	for b := 0; b < 16; b++ {
		m.refIdxL0[b], m.refIdxL1[b] = seed+int8(b%3), seed
		m.MvL0[b] = [2]int16{int16(seed) * 5, int16(b)}
		m.MvL1[b] = [2]int16{int16(-seed), int16(b) * 2}
		m.RefPicL0[b] = &frame.Picture{POC: 40 + int(seed)}
		m.RefPicL1[b] = &frame.Picture{POC: 50 + int(seed)}
	}
}

// decodeInto does what startMB leaves in a slot, then stores one inter block
// motion, so two grids can be compared after one picture's worth of decoding.
func decodeInto(g *mbGrid, idx int, intra bool) {
	m := &g.mbs[idx]
	*m = mbState{}
	m.SliceID = 1
	m.refIdxL0, m.refIdxL1 = noRef16, noRef16
	m.Intra = intra
	if !intra {
		m.refIdxL0[0], m.MvL0[0] = 1, [2]int16{7, -9}
		m.RefPicL0[0] = &frame.Picture{POC: 12}
	}
	m.Decoded = true
}

// TestFreshGridReadsAsTheClearedGrid pins what replaced the whole-grid clear:
// a grid recycled from a picture whose slots hold stale motion must give the
// same neighbours and the same picture motion as a zeroed one, for slots the
// new picture decodes and for slots it never reaches.
func TestFreshGridReadsAsTheClearedGrid(t *testing.T) {
	const w, h = 3, 2
	for _, intra := range []bool{false, true} {
		for _, staleIntra := range []bool{false, true} {
			d := &Decoder{grid: newMBGrid(w, h)}
			for i := range d.grid.mbs {
				staleSlot(&d.grid.mbs[i], int8(i+1), staleIntra)
			}
			g := d.freshGrid(w, h)
			if g != d.grid {
				t.Fatal("a grid of the same geometry was not reused")
			}
			clean := newMBGrid(w, h)

			for i := range g.mbs {
				if g.mbs[i].Decoded {
					t.Fatalf("slot %d of a recycled grid is Decoded", i)
				}
				if g.neighbour(i%w, i/w, 0, 0, 3) != nil {
					t.Fatalf("slot %d: a stale slot of the old picture is a neighbour", i)
				}
			}

			for _, i := range []int{1, 4} {
				decodeInto(g, i, intra && i == 4)
				decodeInto(clean, i, intra && i == 4)
			}
			got := g.motion(frame.NewMotion(w, h))
			want := clean.motion(frame.NewMotion(w, h))
			if !reflect.DeepEqual(got, want) {
				t.Errorf("intra=%v staleIntra=%v: recycled grid gives other motion than a cleared one", intra, staleIntra)
			}
		}
	}
}
