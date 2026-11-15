package mc

import (
	"bytes"
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

// TestSharedBuffersTakeThePortablePath: the kernels panic when the destination
// block and the source footprint share memory; PredictLuma and PredictLumaAvg
// must not reach them then. The destination lies in the same array as the
// reference, disjoint from the footprint (still the same backing array) and
// overlapping it.
func TestSharedBuffersTakeThePortablePath(t *testing.T) {
	rng := rand.New(rand.NewSource(23))
	const w, h = 8, 8
	p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
	for _, avg := range []bool{false, true} {
		for yFrac := 0; yFrac < 4; yFrac++ {
			for xFrac := 0; xFrac < 4; xFrac++ {
				// A second plane copy: the destination is its tail, the source its head.
				buf := append(append([]byte(nil), p.buf...), make([]byte, w*h)...)
				tail := len(p.buf)
				before := append([]byte(nil), buf[tail:]...)
				if avg {
					for i := range before {
						before[i] = byte(rng.Intn(256))
					}
					copy(buf[tail:], before)
					PredictLumaAvg(buf[tail:], w, 0, buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				} else {
					PredictLuma(buf[tail:], w, 0, buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				}
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						want := refLumaVal(p.get, x, y, xFrac, yFrac)
						if avg {
							want = (int(before[y*w+x]) + want + 1) >> 1
						}
						if int(buf[tail+y*w+x]) != want {
							t.Fatalf("avg=%v frac(%d,%d) (%d,%d): got %d, want %d", avg, xFrac, yFrac, x, y, buf[tail+y*w+x], want)
						}
					}
				}
				// Destination inside the source footprint: the result must be
				// the portable path's on the same memory, asserted on the
				// samples (a missing guard also panics in the kernel, but the
				// guard is tested by what it chooses, not by the panic).
				mix := append([]byte(nil), p.buf...)
				ref := append([]byte(nil), p.buf...)
				PredictLuma(mix[p.off(0, 0):], p.stride, 0, mix, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				predictLumaPortable(ref[p.off(0, 0):], p.stride, 0, ref, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				if !bytes.Equal(mix, ref) {
					t.Fatalf("PredictLuma frac(%d,%d) on overlapping memory differs from the portable path", xFrac, yFrac)
				}
				PredictLumaAvg(mix[p.off(0, 0):], p.stride, 0, mix, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				var tmp [256]byte
				predictLumaPortable(tmp[:w*h], w, 0, ref, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				Average(ref[p.off(0, 0):], p.stride, 0, ref[p.off(0, 0):], p.stride, 0, tmp[:w*h], w, 0, w, h)
				if !bytes.Equal(mix, ref) {
					t.Fatalf("PredictLumaAvg frac(%d,%d) on overlapping memory differs from the portable path", xFrac, yFrac)
				}
			}
		}
	}
}
