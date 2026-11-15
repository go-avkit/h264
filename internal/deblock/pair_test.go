package deblock

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestChromaPairMatchesTheScalarFilter filters Cb and Cr together, with
// different thresholds for each plane (8-309, 8-310), every combination of
// boundary strengths 0..4 per group (including the mixes of bS 4 with lower
// strengths that the kernels do not take), both orientations, against the
// scalar filter of this package applied to each plane.
func TestChromaPairMatchesTheScalarFilter(t *testing.T) {
	rng := rand.New(rand.NewSource(41))
	const stride, rows = 40, 40
	const off = 16*stride + 16
	fill := func() []byte {
		b := make([]byte, stride*rows)
		base := rng.Intn(200)
		for i := range b {
			b[i] = byte(base + rng.Intn(24)) // close values: the thresholds pass often
		}
		return b
	}
	for _, vertical := range []bool{true, false} {
		for code := 0; code < 5*5*5*5; code++ {
			bS := [4]uint8{uint8(code % 5), uint8(code / 5 % 5), uint8(code / 25 % 5), uint8(code / 125 % 5)}
			for n := 0; n < 6; n++ {
				iaCb, ibCb := 16+rng.Intn(36), 16+rng.Intn(36)
				iaCr, ibCr := 16+rng.Intn(36), 16+rng.Intn(36)
				cb, cr := fill(), fill()
				wantCb, wantCr := bytes.Clone(cb), bytes.Clone(cr)
				if vertical {
					FilterChromaPairVertical(cb, cr, stride, off, bS, iaCb, ibCb, iaCr, ibCr)
					filterEdge(wantCb, off, stride, 1, 8, 2, bS, iaCb, ibCb, true)
					filterEdge(wantCr, off, stride, 1, 8, 2, bS, iaCr, ibCr, true)
				} else {
					FilterChromaPairHorizontal(cb, cr, stride, off, bS, iaCb, ibCb, iaCr, ibCr)
					filterEdge(wantCb, off, 1, stride, 8, 2, bS, iaCb, ibCb, true)
					filterEdge(wantCr, off, 1, stride, 8, 2, bS, iaCr, ibCr, true)
				}
				if !bytes.Equal(cb, wantCb) || !bytes.Equal(cr, wantCr) {
					t.Fatalf("vertical=%v bS=%v indexes Cb (%d,%d) Cr (%d,%d): the pair differs from the scalar filter",
						vertical, bS, iaCb, ibCb, iaCr, ibCr)
				}
			}
		}
	}
}

// TestLumaEdgeMixedStrengthsMatchTheScalarFilter covers the bS patterns the
// kernels do not take as one call.
func TestLumaEdgeMixedStrengthsMatchTheScalarFilter(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	const stride, rows = 48, 40
	const off = 12*stride + 16
	for _, vertical := range []bool{true, false} {
		for code := 0; code < 5*5*5*5; code++ {
			bS := [4]uint8{uint8(code % 5), uint8(code / 5 % 5), uint8(code / 25 % 5), uint8(code / 125 % 5)}
			for n := 0; n < 3; n++ {
				ia, ib := 16+rng.Intn(36), 16+rng.Intn(36)
				p := make([]byte, stride*rows)
				base := rng.Intn(200)
				for i := range p {
					p[i] = byte(base + rng.Intn(24))
				}
				want := bytes.Clone(p)
				if vertical {
					FilterLumaEdgeVertical(p, stride, off, bS, ia, ib)
					filterEdge(want, off, stride, 1, 16, 4, bS, ia, ib, false)
				} else {
					FilterLumaEdgeHorizontal(p, stride, off, bS, ia, ib)
					filterEdge(want, off, 1, stride, 16, 4, bS, ia, ib, false)
				}
				if !bytes.Equal(p, want) {
					t.Fatalf("vertical=%v bS=%v indexes (%d,%d): differs from the scalar filter", vertical, bS, ia, ib)
				}
			}
		}
	}
}
