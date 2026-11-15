package simd

import (
	"math/rand"
	"testing"
)

// centreOracle is the centre half sample j written the way 8.4.2.2.1 states
// it in equation 8-246: six horizontal intermediates (aa, bb, b1, s1, gg, hh,
// equation 8-241 applied to six rows), then the same taps vertically over
// them, Clip1((j1 + 512) >> 10), equation 8-247. It shares nothing with
// sixTapHVGeneric but the tap values.
func centreOracle(src []byte, stride, off int) byte {
	b1 := func(row int) int {
		p := off + row*stride
		return int(src[p-2]) - 5*int(src[p-1]) + 20*int(src[p]) + 20*int(src[p+1]) - 5*int(src[p+2]) + int(src[p+3])
	}
	j1 := b1(-2) - 5*b1(-1) + 20*b1(0) + 20*b1(1) - 5*b1(2) + b1(3)
	v := (j1 + 512) >> 10
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// TestSixTapHVGenericMatchesTheEquations compares the widths and heights
// {2, 4, 8, 16} in every combination (not every integer from 2 to 16: the
// sizes H.264 predicts are among them), with random samples and with the extremes that drive the
// intermediates to their bounds (-2550 .. 10710) and the clip at both ends.
func TestSixTapHVGenericMatchesTheEquations(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const stride = 48
	fills := map[string]func(i int) byte{
		"random": func(int) byte { return byte(rng.Intn(256)) },
		"zero":   func(int) byte { return 0 },
		"full":   func(int) byte { return 255 },
		"checks": func(i int) byte {
			if (i/stride+i%stride)%2 == 0 {
				return 255
			}
			return 0
		},
		"columns": func(i int) byte {
			if i%stride%2 == 0 {
				return 255
			}
			return 0
		},
		"rows": func(i int) byte {
			if i/stride%2 == 0 {
				return 255
			}
			return 0
		},
	}
	for name, fill := range fills {
		for _, w := range []int{2, 4, 8, 16} {
			for _, h := range []int{2, 4, 8, 16} {
				src := make([]byte, stride*(h+8))
				for i := range src {
					src[i] = fill(i)
				}
				off := 3*stride + 4
				dst := make([]byte, 20*h)
				for i := range dst {
					dst[i] = 0xAA
				}
				sixTapHVGeneric(dst, 20, 1, src, stride, off, w, h)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						want := centreOracle(src, stride, off+y*stride+x)
						if got := dst[1+y*20+x]; got != want {
							t.Fatalf("%s %dx%d (%d,%d): got %d, want %d", name, w, h, x, y, got, want)
						}
					}
					if dst[y*20] != 0xAA || dst[1+y*20+w] != 0xAA {
						t.Fatalf("%s %dx%d row %d: wrote outside the block", name, w, h, y)
					}
				}
			}
		}
	}
}

// TestSixTapHVGenericBeyondSixteen covers a block taller than the stack
// buffer holds, which H.264 never asks for but the function accepts.
func TestSixTapHVGenericBeyondSixteen(t *testing.T) {
	const stride, w, h = 40, 24, 20
	src := make([]byte, stride*(h+8))
	rng := rand.New(rand.NewSource(2))
	for i := range src {
		src[i] = byte(rng.Intn(256))
	}
	off := 3*stride + 4
	dst := make([]byte, w*h)
	sixTapHVGeneric(dst, w, 0, src, stride, off, w, h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if want := centreOracle(src, stride, off+y*stride+x); dst[y*w+x] != want {
				t.Fatalf("(%d,%d): got %d, want %d", x, y, dst[y*w+x], want)
			}
		}
	}
}
