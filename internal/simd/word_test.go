package simd

import (
	"bytes"
	"math/rand"
	"testing"
)

// TestAvg8IsTheRoundingAverageInEveryLane drives all 65536 pairs of samples
// through every lane position, with the other lanes holding the extremes that
// would carry or borrow across a lane boundary.
func TestAvg8IsTheRoundingAverageInEveryLane(t *testing.T) {
	for lane := 0; lane < 8; lane++ {
		for _, other := range [][2]uint64{{0, 0}, {255, 255}, {0, 255}, {255, 0}, {1, 254}, {128, 127}} {
			for a := uint64(0); a < 256; a++ {
				for b := uint64(0); b < 256; b++ {
					var wa, wb uint64
					for l := 0; l < 8; l++ {
						va, vb := other[0], other[1]
						if l == lane {
							va, vb = a, b
						}
						wa |= va << (8 * l)
						wb |= vb << (8 * l)
					}
					got := avg8(wa, wb)
					for l := 0; l < 8; l++ {
						va, vb := (wa>>(8*l))&255, (wb>>(8*l))&255
						if g, want := (got>>(8*l))&255, (va+vb+1)>>1; g != want {
							t.Fatalf("lane %d of (%d, %d) next to %v: got %d, want %d", l, va, vb, other, g, want)
						}
					}
				}
			}
		}
	}
}

// TestAvgBytesGenericAllWidths covers every width up to 33 (the word loop, its
// tail, and both), unequal strides and offsets, and guard bytes around the
// destination block.
func TestAvgBytesGenericAllWidths(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	for w := 1; w <= 33; w++ {
		for _, h := range []int{1, 3, 16} {
			const aStride, bStride, dStride = 40, 37, 45
			a := make([]byte, aStride*h+3)
			b := make([]byte, bStride*h+5)
			for i := range a {
				a[i] = byte(rng.Intn(256))
			}
			for i := range b {
				b[i] = byte(rng.Intn(256))
			}
			dst := make([]byte, dStride*h+2)
			for i := range dst {
				dst[i] = 0xAA
			}
			avgBytesGeneric(dst, dStride, 1, a, aStride, 3, b, bStride, 5, w, h)
			for i := range dst {
				y, x := (i-1)/dStride, (i-1)%dStride
				inside := i >= 1 && y < h && x < w
				if !inside {
					if dst[i] != 0xAA {
						t.Fatalf("w=%d h=%d: byte %d outside the block was written", w, h, i)
					}
					continue
				}
				want := byte((int(a[3+y*aStride+x]) + int(b[5+y*bStride+x]) + 1) >> 1)
				if dst[i] != want {
					t.Fatalf("w=%d h=%d (%d,%d): got %d, want %d", w, h, x, y, dst[i], want)
				}
			}
		}
	}
}

func TestCopyBlockGenericAllWidths(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	for w := 1; w <= 33; w++ {
		for _, h := range []int{1, 3, 16} {
			const sStride, dStride = 40, 45
			src := make([]byte, sStride*h+3)
			for i := range src {
				src[i] = byte(rng.Intn(256))
			}
			dst := make([]byte, dStride*h+2)
			for i := range dst {
				dst[i] = 0xAA
			}
			want := bytes.Clone(dst)
			for y := 0; y < h; y++ {
				copy(want[1+y*dStride:1+y*dStride+w], src[3+y*sStride:])
			}
			copyBlockGeneric(dst, dStride, 1, src, sStride, 3, w, h)
			if !bytes.Equal(dst, want) {
				t.Fatalf("w=%d h=%d: copy differs from the reference", w, h)
			}
		}
	}
}

func benchAvg(b *testing.B, w, h int) {
	x := make([]byte, 16*16*2)
	y := make([]byte, 16*16*2)
	d := make([]byte, 16*16*2)
	for i := range x {
		x[i], y[i] = byte(i*7), byte(i*13)
	}
	b.SetBytes(int64(w * h))
	for i := 0; i < b.N; i++ {
		avgBytesGeneric(d, 16, 0, x, 16, 0, y, 16, 0, w, h)
	}
}

func BenchmarkAvgBytesGeneric16x16(b *testing.B) { benchAvg(b, 16, 16) }
func BenchmarkAvgBytesGeneric8x8(b *testing.B)   { benchAvg(b, 8, 8) }
func BenchmarkAvgBytesGeneric4x4(b *testing.B)   { benchAvg(b, 4, 4) }

func benchCopy(b *testing.B, w, h int) {
	s := make([]byte, 64*32)
	d := make([]byte, 64*32)
	b.SetBytes(int64(w * h))
	for i := 0; i < b.N; i++ {
		copyBlockGeneric(d, 64, 0, s, 64, 0, w, h)
	}
}

func BenchmarkCopyBlockGeneric16x16(b *testing.B) { benchCopy(b, 16, 16) }
func BenchmarkCopyBlockGeneric8x8(b *testing.B)   { benchCopy(b, 8, 8) }
func BenchmarkCopyBlockGeneric4x4(b *testing.B)   { benchCopy(b, 4, 4) }
