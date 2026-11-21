package deblock

import (
	"bytes"
	"math/rand"
	"testing"
)

// The classified entry points must equal the plain ones for every strength of
// the class they are given: all 4, or none 4.
func TestClassifiedMatchesPlain(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const stride, off = 64, 64*8 + 8
	fill := func() []byte {
		p := make([]byte, stride*32)
		base := byte(rng.Intn(200))
		for i := range p {
			p[i] = base + byte(rng.Intn(24))
		}
		return p
	}
	before := classifiedFallbacks.Load()
	defer func() {
		took := classifiedFallbacks.Load() - before
		if kernelsLF && took != 0 {
			t.Errorf("%d classified calls fell back to the plain filters with the kernels on", took)
		}
		if !kernelsLF && took != 4*4000 {
			t.Errorf("%d fallbacks, want %d without the kernels", took, 4*4000)
		}
	}()
	for n := 0; n < 4000; n++ {
		strong := n&1 == 0
		var bS [4]uint8
		for g := range bS {
			if strong {
				bS[g] = 4
			} else {
				bS[g] = uint8(rng.Intn(4))
			}
		}
		ia, ib := rng.Intn(52), rng.Intn(52)
		ia2, ib2 := rng.Intn(52), rng.Intn(52)
		src, srcC := fill(), fill()
		check := func(name string, plain, cls func(y, c []byte)) {
			y1, c1 := append([]byte(nil), src...), append([]byte(nil), srcC...)
			y2, c2 := append([]byte(nil), src...), append([]byte(nil), srcC...)
			plain(y1, c1)
			cls(y2, c2)
			if !bytes.Equal(y1, y2) || !bytes.Equal(c1, c2) {
				t.Fatalf("%s: strong=%v bS=%v ia=%d ib=%d differ", name, strong, bS, ia, ib)
			}
		}
		check("lumaV",
			func(y, c []byte) { FilterLumaEdgeVertical(y, stride, off, bS, ia, ib) },
			func(y, c []byte) { FilterLumaEdgeVerticalClassified(y, stride, off, bS, strong, ia, ib) })
		check("lumaH",
			func(y, c []byte) { FilterLumaEdgeHorizontal(y, stride, off, bS, ia, ib) },
			func(y, c []byte) { FilterLumaEdgeHorizontalClassified(y, stride, off, bS, strong, ia, ib) })
		check("chromaV",
			func(y, c []byte) { FilterChromaPairVertical(y, c, stride, off, bS, ia, ib, ia2, ib2) },
			func(y, c []byte) { FilterChromaPairVerticalClassified(y, c, stride, off, bS, strong, ia, ib, ia2, ib2) })
		check("chromaH",
			func(y, c []byte) { FilterChromaPairHorizontal(y, c, stride, off, bS, ia, ib, ia2, ib2) },
			func(y, c []byte) {
				FilterChromaPairHorizontalClassified(y, c, stride, off, bS, strong, ia, ib, ia2, ib2)
			})
	}
}
