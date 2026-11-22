//go:build amd64 && !purego

package simd

import (
	"math/rand"
	"testing"
)

type namedTap struct {
	name string
	fn   sixTapFn
	w, h int
	kind int
}

const (
	tapHoriz = iota
	tapVert
	tapHV
)

func avx2TapKernels(t *testing.T) []namedTap {
	t.Helper()
	if !hasAVX2 {
		t.Skip("processor does not support AVX2")
	}
	return []namedTap{
		{"sixTapHoriz16x16AVX2", sixTapHoriz16x16AVX2, 16, 16, tapHoriz},
		{"sixTapHoriz16x8AVX2", sixTapHoriz16x8AVX2, 16, 8, tapHoriz},
		{"sixTapVert16x16AVX2", sixTapVert16x16AVX2, 16, 16, tapVert},
		{"sixTapVert16x8AVX2", sixTapVert16x8AVX2, 16, 8, tapVert},
		{"sixTapHV16x16AVX2", sixTapHV16x16AVX2, 16, 16, tapHV},
		{"sixTapHV16x8AVX2", sixTapHV16x8AVX2, 16, 8, tapHV},
	}
}

func sseTapKernels() []namedTap {
	return []namedTap{
		{"sixTapHoriz16x16", sixTapHoriz16x16, 16, 16, tapHoriz},
		{"sixTapHoriz16x8", sixTapHoriz16x8, 16, 8, tapHoriz},
		{"sixTapVert16x16", sixTapVert16x16, 16, 16, tapVert},
		{"sixTapVert16x8", sixTapVert16x8, 16, 8, tapVert},
		{"sixTapHV16x16", sixTapHV16x16, 16, 16, tapHV},
		{"sixTapHV16x8", sixTapHV16x8, 16, 8, tapHV},
	}
}

func tapReference(k namedTap, dst []byte, dstStride int, src []byte, srcStride, srcOff int) {
	switch k.kind {
	case tapHoriz:
		sixTapHorizGeneric(dst, dstStride, 0, src, srcStride, srcOff, k.w, k.h)
	case tapVert:
		sixTapVertGeneric(dst, dstStride, 0, src, srcStride, srcOff, k.w, k.h)
	default:
		sixTapHVGeneric(dst, dstStride, 0, src, srcStride, srcOff, k.w, k.h)
	}
}

func checkTapKernels(t *testing.T, kernels []namedTap, fill func(rng *rand.Rand, x, y int) int) {
	t.Helper()
	rng := rand.New(rand.NewSource(20260902))
	for _, k := range kernels {
		for iter := 0; iter < 400; iter++ {
			src, stride, off := mcPlane(rng, k.w, k.h, 8, func(x, y int) int { return fill(rng, x, y) })
			got := make([]byte, k.w*k.h)
			want := make([]byte, k.w*k.h)
			k.fn(got, k.w, src[off:], stride)
			tapReference(k, want, k.w, src, stride, off)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s iter %d: sample %d = %d, generic = %d", k.name, iter, i, got[i], want[i])
				}
			}
		}
	}
}

func TestSixTapAVX2MatchesGeneric(t *testing.T) {
	checkTapKernels(t, avx2TapKernels(t), func(rng *rand.Rand, x, y int) int { return rng.Intn(256) })
}

func TestSixTapSSEMatchesGeneric(t *testing.T) {
	checkTapKernels(t, sseTapKernels(), func(rng *rand.Rand, x, y int) int { return rng.Intn(256) })
}

func TestSixTapAVX2Extremes(t *testing.T) {
	kernels := avx2TapKernels(t)
	patterns := []func(x, y int) int{
		func(x, y int) int { return 0 },
		func(x, y int) int { return 255 },
		func(x, y int) int {
			if (x+y)%2 == 0 {
				return 0
			}
			return 255
		},
		func(x, y int) int {
			if x%4 < 2 {
				return 255
			}
			return 0
		},
		func(x, y int) int {
			if y%4 < 2 {
				return 255
			}
			return 0
		},
	}
	rng := rand.New(rand.NewSource(1))
	for _, k := range kernels {
		for pi, p := range patterns {
			src, stride, off := mcPlane(rng, k.w, k.h, 8, p)
			got := make([]byte, k.w*k.h)
			want := make([]byte, k.w*k.h)
			k.fn(got, k.w, src[off:], stride)
			tapReference(k, want, k.w, src, stride, off)
			for i := range want {
				if got[i] != want[i] {
					t.Fatalf("%s pattern %d: sample %d = %d, generic = %d", k.name, pi, i, got[i], want[i])
				}
			}
		}
	}
}

func TestSixTapAVX2ExactMinimumMargin(t *testing.T) {
	kernels := avx2TapKernels(t)
	rng := rand.New(rand.NewSource(2))
	for _, k := range kernels {
		stride := k.w + 5
		rows := k.h + 5
		buf := make([]byte, stride*rows)
		for i := range buf {
			buf[i] = byte(rng.Intn(256))
		}
		off := 2*stride + 2
		got := make([]byte, k.w*k.h)
		want := make([]byte, k.w*k.h)
		k.fn(got, k.w, buf[off:], stride)
		tapReference(k, want, k.w, buf, stride, off)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: sample %d = %d, generic = %d", k.name, i, got[i], want[i])
			}
		}
	}
}

func FuzzSixTapAVX2AgainstGeneric(f *testing.F) {
	f.Add(make([]byte, 32*32), 0)
	f.Fuzz(func(t *testing.T, buf []byte, sel int) {
		if !hasAVX2 {
			t.Skip("processor does not support AVX2")
		}
		kernels := []namedTap{
			{"sixTapHoriz16x16AVX2", sixTapHoriz16x16AVX2, 16, 16, tapHoriz},
			{"sixTapHoriz16x8AVX2", sixTapHoriz16x8AVX2, 16, 8, tapHoriz},
			{"sixTapVert16x16AVX2", sixTapVert16x16AVX2, 16, 16, tapVert},
			{"sixTapVert16x8AVX2", sixTapVert16x8AVX2, 16, 8, tapVert},
			{"sixTapHV16x16AVX2", sixTapHV16x16AVX2, 16, 16, tapHV},
			{"sixTapHV16x8AVX2", sixTapHV16x8AVX2, 16, 8, tapHV},
		}
		if sel < 0 {
			sel = -sel
		}
		k := kernels[sel%len(kernels)]
		stride := k.w + 6
		rows := k.h + 6
		if len(buf) < stride*rows {
			t.Skip()
		}
		off := 2*stride + 2
		got := make([]byte, k.w*k.h)
		want := make([]byte, k.w*k.h)
		k.fn(got, k.w, buf[off:], stride)
		tapReference(k, want, k.w, buf, stride, off)
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("%s: sample %d = %d, generic = %d", k.name, i, got[i], want[i])
			}
		}
	})
}

func TestCPUFeatureDetectionIsConsistent(t *testing.T) {
	if hasAVX2 && !hasSSE41 {
		t.Fatal("AVX2 reported without SSE4.1")
	}
	t.Logf("sse4.1=%v avx2=%v", hasSSE41, hasAVX2)
}

func benchTapKernel(b *testing.B, fn sixTapFn, w, h int) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, w*h)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(dst, w, src[off:], stride)
	}
}

func BenchmarkSixTapHoriz16x16SSE(b *testing.B) { benchTapKernel(b, sixTapHoriz16x16, 16, 16) }
func BenchmarkSixTapVert16x16SSE(b *testing.B)  { benchTapKernel(b, sixTapVert16x16, 16, 16) }
func BenchmarkSixTapHV16x16SSE(b *testing.B)    { benchTapKernel(b, sixTapHV16x16, 16, 16) }

func BenchmarkSixTapHoriz16x16AVX2(b *testing.B) {
	if !hasAVX2 {
		b.Skip("processor does not support AVX2")
	}
	benchTapKernel(b, sixTapHoriz16x16AVX2, 16, 16)
}

func BenchmarkSixTapVert16x16AVX2(b *testing.B) {
	if !hasAVX2 {
		b.Skip("processor does not support AVX2")
	}
	benchTapKernel(b, sixTapVert16x16AVX2, 16, 16)
}

func BenchmarkSixTapHV16x16AVX2(b *testing.B) {
	if !hasAVX2 {
		b.Skip("processor does not support AVX2")
	}
	benchTapKernel(b, sixTapHV16x16AVX2, 16, 16)
}
