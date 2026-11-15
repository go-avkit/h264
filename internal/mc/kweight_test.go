package mc

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestWeightKernelSizesMatchTheEquations drives the weighting at the sizes the
// kernels take (and 4x16, which they do not), with parameters across the legal
// ranges including the extremes, random and saturating samples, a destination
// that is a separate block and one that is the first prediction itself,
// against equations 8-274 and 8-276 as written in mc_test.go.
func TestWeightKernelSizesMatchTheEquations(t *testing.T) {
	rng := rand.New(rand.NewSource(31))
	sizes := append([][2]int{{4, 16}}, lumaSizes...)
	weights := []int{-128, -64, -1, 0, 1, 31, 32, 33, 64, 127, 128}
	offsets := []int{-128, -17, -1, 0, 1, 20, 127}
	sample := func() byte {
		switch rng.Intn(4) {
		case 0:
			return 0
		case 1:
			return 255
		}
		return byte(rng.Intn(256))
	}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		for logWD := 0; logWD <= 7; logWD++ {
			for _, w0 := range weights {
				for _, o0 := range offsets {
					w1, o1 := weights[rng.Intn(len(weights))], offsets[rng.Intn(len(offsets))]
					const stride = 24
					a := make([]byte, stride*h+4)
					b := make([]byte, stride*h+4)
					for i := range a {
						a[i], b[i] = sample(), sample()
					}
					// bi, separate destination
					dst := make([]byte, 2*stride*h)
					for i := range dst {
						dst[i] = 0xAA
					}
					WeightBi(dst, 2*stride, 3, a, stride, 2, b, stride, 1, w, h, w0, w1, o0, o1, logWD)
					// bi, in place
					inPlace := append([]byte(nil), a...)
					WeightBi(inPlace, stride, 2, inPlace, stride, 2, b, stride, 1, w, h, w0, w1, o0, o1, logWD)
					// uni
					uni := append([]byte(nil), a...)
					WeightUni(uni, stride, 2, w, h, w0, o0, logWD)
					for y := 0; y < h; y++ {
						for x := 0; x < w; x++ {
							pa, pb := int(a[2+y*stride+x]), int(b[1+y*stride+x])
							if want := specWeightBi(pa, pb, w0, w1, o0, o1, logWD); int(dst[3+y*2*stride+x]) != want {
								t.Fatalf("bi %dx%d logWD %d w (%d,%d) o (%d,%d) pixel (%d,%d): got %d, want %d", w, h, logWD, w0, w1, o0, o1, x, y, dst[3+y*2*stride+x], want)
							}
							if want := specWeightBi(pa, pb, w0, w1, o0, o1, logWD); int(inPlace[2+y*stride+x]) != want {
								t.Fatalf("bi in place %dx%d logWD %d w (%d,%d) o (%d,%d) pixel (%d,%d): got %d, want %d", w, h, logWD, w0, w1, o0, o1, x, y, inPlace[2+y*stride+x], want)
							}
							skip := o0 == 0 && w0 == 1<<uint(logWD)
							want := specWeightUni(pa, w0, o0, logWD)
							if skip {
								want = pa
							}
							if int(uni[2+y*stride+x]) != want {
								t.Fatalf("uni %dx%d logWD %d w %d o %d pixel (%d,%d): got %d, want %d", w, h, logWD, w0, o0, x, y, uni[2+y*stride+x], want)
							}
						}
					}
					// nothing outside the block moved
					for i := range dst {
						y, x := (i-3)/(2*stride), (i-3)%(2*stride)
						if (i < 3 || y >= h || x >= w) && dst[i] != 0xAA {
							t.Fatalf("bi %dx%d: byte %d outside the block was written", w, h, i)
						}
					}
				}
			}
		}
	}
}

// TestWeightBiSharedBuffersTakeThePortablePath: the kernel panics when the
// destination and the second prediction share memory, so WeightBi must choose
// the portable path then, and its samples must be the portable result (asserted
// on the samples, not left to a panic).
func TestWeightBiSharedBuffersTakeThePortablePath(t *testing.T) {
	rng := rand.New(rand.NewSource(32))
	const w, h, stride = 8, 8, 16
	for n := 0; n < 200; n++ {
		buf := make([]byte, 3*stride*h)
		for i := range buf {
			buf[i] = byte(rng.Intn(256))
		}
		w0, w1 := rng.Intn(129)-32, rng.Intn(129)-32
		o0, o1 := rng.Intn(40)-20, rng.Intn(40)-20
		logWD := 1 + rng.Intn(6)
		a := make([]byte, stride*h)
		for i := range a {
			a[i] = byte(rng.Intn(256))
		}
		// b is the block at offset 0 of buf, dst the block two rows down and
		// three columns right in the SAME array: their byte ranges intersect,
		// which the kernel refuses. The portable loop runs row by row, sample
		// by sample, reading b as it stands, which is what want replays.
		const dOff = 2*stride + 3
		want := append([]byte(nil), buf...)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				pa, pb := int(a[y*stride+x]), int(want[y*stride+x])
				want[dOff+y*stride+x] = byte(specWeightBi(pa, pb, w0, w1, o0, o1, logWD))
			}
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("WeightBi reached the kernel with overlapping memory: %v", r)
				}
			}()
			WeightBi(buf, stride, dOff, a, stride, 0, buf, stride, 0, w, h, w0, w1, o0, o1, logWD)
		}()
		if !bytes.Equal(buf, want) {
			t.Fatalf("weights (%d,%d) offsets (%d,%d) logWD %d: shared-memory WeightBi differs from the portable result", w0, w1, o0, o1, logWD)
		}
	}
}
