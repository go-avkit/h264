package mc

import (
	"math/rand"
	"testing"
)

// TestPredictQuarterMatchesTheEquations drives predictQuarter directly (the
// amd64 kernels bypass it in PredictLuma) over every quarter position and block
// size, with a destination stride wider than the block, against the reference
// of mc_test.go, which states 8-241 to 8-261 term by term. The fills include
// the patterns that push the six-tap intermediates to their bounds and both
// clip ends.
func TestPredictQuarterMatchesTheEquations(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	fills := map[string]func(x, y int) int{
		"random": func(x, y int) int { return rng.Intn(256) },
		"zero":   func(x, y int) int { return 0 },
		"full":   func(x, y int) int { return 255 },
		"checks": func(x, y int) int { return 255 * ((x + y) & 1) },
		"cols":   func(x, y int) int { return 255 * (x & 1) },
		"rows":   func(x, y int) int { return 255 * (y & 1) },
		"edge":   func(x, y int) int { return 255 * ((x>>1 + y>>1) & 1) },
	}
	const dstStride = 20
	for name, fill := range fills {
		for _, sz := range lumaSizes {
			w, h := sz[0], sz[1]
			p := newPlane(w, h, lumaMargin, fill)
			for yFrac := 0; yFrac < 4; yFrac++ {
				for xFrac := 0; xFrac < 4; xFrac++ {
					if xFrac&1 == 0 && yFrac&1 == 0 {
						continue
					}
					dst := make([]byte, dstStride*h+1)
					for i := range dst {
						dst[i] = 0xAA
					}
					predictQuarter(dst, dstStride, 1, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
					for y := 0; y < h; y++ {
						for x := 0; x < dstStride; x++ {
							got := int(dst[1+y*dstStride+x])
							if x >= w {
								if got != 0xAA {
									t.Fatalf("%s %dx%d frac(%d,%d): wrote outside the block at (%d,%d)", name, w, h, xFrac, yFrac, x, y)
								}
								continue
							}
							if want := refLumaVal(p.get, x, y, xFrac, yFrac); got != want {
								t.Fatalf("%s %dx%d frac(%d,%d) (%d,%d): got %d, want %d", name, w, h, xFrac, yFrac, x, y, got, want)
							}
						}
					}
				}
			}
		}
	}
}

func BenchmarkPredictLuma16x16Quarter(b *testing.B) {
	p := benchLumaPlane()
	dst := make([]byte, 256)
	for _, f := range [][2]int{{1, 0}, {0, 1}, {2, 1}, {1, 2}, {1, 1}, {3, 3}} {
		b.Run(string(rune('0'+f[0]))+string(rune('0'+f[1])), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				PredictLuma(dst, 16, 0, p.buf, p.stride, p.off(8, 8), 16, 16, f[0], f[1])
			}
		})
	}
}
