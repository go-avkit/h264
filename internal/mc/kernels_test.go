package mc

import (
	"math/rand"
	"testing"
)

// TestPredictLumaAvgAveragesIntoTheDestination checks every quarter position
// and size, including 4x16 (a merged run of equal motion, outside the kernels'
// seven sizes), against the reference of mc_test.go and equation 8-273.
func TestPredictLumaAvgAveragesIntoTheDestination(t *testing.T) {
	rng := rand.New(rand.NewSource(21))
	sizes := append([][2]int{{4, 16}}, lumaSizes...)
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 4; yFrac++ {
			for xFrac := 0; xFrac < 4; xFrac++ {
				for trial := 0; trial < 20; trial++ {
					p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
					const stride = 20
					dst := make([]byte, stride*h+3)
					for i := range dst {
						dst[i] = byte(rng.Intn(256))
					}
					before := append([]byte(nil), dst...)
					PredictLumaAvg(dst, stride, 2, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
					for i := range dst {
						y, x := (i-2)/stride, (i-2)%stride
						inside := i >= 2 && y < h && x < w
						if !inside {
							if dst[i] != before[i] {
								t.Fatalf("%dx%d frac(%d,%d): byte %d outside the block changed", w, h, xFrac, yFrac, i)
							}
							continue
						}
						want := (int(before[i]) + refLumaVal(p.get, x, y, xFrac, yFrac) + 1) >> 1
						if int(dst[i]) != want {
							t.Fatalf("%dx%d frac(%d,%d) (%d,%d): got %d, want %d", w, h, xFrac, yFrac, x, y, dst[i], want)
						}
					}
				}
			}
		}
	}
}

// TestPredictLumaOutsideTheKernelSizes covers the sizes only the portable code
// predicts.
func TestPredictLumaOutsideTheKernelSizes(t *testing.T) {
	rng := rand.New(rand.NewSource(22))
	for _, sz := range [][2]int{{4, 16}, {16, 4}, {2, 2}} {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 4; yFrac++ {
			for xFrac := 0; xFrac < 4; xFrac++ {
				p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
				dst := make([]byte, w*h)
				PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, 4*3+xFrac, -4*2+yFrac)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						if want := refLumaVal(p.get, x+3, y-2, xFrac, yFrac); int(dst[y*w+x]) != want {
							t.Fatalf("%dx%d frac(%d,%d) (%d,%d): got %d, want %d", w, h, xFrac, yFrac, x, y, dst[y*w+x], want)
						}
					}
				}
			}
		}
	}
}
