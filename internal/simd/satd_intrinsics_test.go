//go:build amd64 && !purego && goexperiment.simd && simdintrinsics

package simd

import (
	"math/rand"
	"testing"
)

func TestSATD4x4IntrinsicsMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(20260902))
	const stride = 24
	src := make([]byte, stride*8)
	ref := make([]byte, stride*8)
	for iter := 0; iter < 200000; iter++ {
		for i := range src {
			src[i] = byte(rng.Intn(256))
			ref[i] = byte(rng.Intn(256))
		}
		want := satd4x4Generic(src, stride, ref, stride)
		if got := satd4x4Intrinsics(src, stride, ref, stride); got != want {
			t.Fatalf("iter %d: intrinsics = %d, generic = %d", iter, got, want)
		}
	}
}

func TestSATD4x4IntrinsicsExtremes(t *testing.T) {
	const stride = 16
	cases := [][2]byte{{0, 255}, {255, 0}, {0, 0}, {255, 255}, {128, 127}}
	for _, c := range cases {
		src := make([]byte, stride*8)
		ref := make([]byte, stride*8)
		for i := range src {
			src[i] = c[0]
			ref[i] = c[1]
		}
		want := satd4x4Generic(src, stride, ref, stride)
		if got := satd4x4Intrinsics(src, stride, ref, stride); got != want {
			t.Errorf("fill %v: intrinsics = %d, generic = %d", c, got, want)
		}
	}
}

func FuzzSATD4x4Intrinsics(f *testing.F) {
	f.Add(make([]byte, 128), make([]byte, 128), 8)
	f.Fuzz(func(t *testing.T, a, b []byte, stride int) {
		if stride < 4 || stride > 64 {
			t.Skip()
		}
		need := 3*stride + 4
		if len(a) < need || len(b) < need {
			t.Skip()
		}
		want := satd4x4Generic(a, stride, b, stride)
		if got := satd4x4Intrinsics(a, stride, b, stride); got != want {
			t.Fatalf("intrinsics = %d, generic = %d", got, want)
		}
	})
}

func BenchmarkSATD4x4Intrinsics(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	const stride = 32
	src := make([]byte, stride*8)
	ref := make([]byte, stride*8)
	for i := range src {
		src[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}
	b.ResetTimer()
	sink := 0
	for i := 0; i < b.N; i++ {
		sink += satd4x4Intrinsics(src, stride, ref, stride)
	}
	_ = sink
}

func TestSATD4x4IntrinsicsVariantsMatch(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	const stride = 32
	src := make([]byte, stride*8)
	ref := make([]byte, stride*8)
	for iter := 0; iter < 50000; iter++ {
		for i := range src {
			src[i] = byte(rng.Intn(256))
			ref[i] = byte(rng.Intn(256))
		}
		want := satd4x4Generic(src, stride, ref, stride)
		if got := satd4x4IntrinsicsFast(src, stride, ref, stride); got != want {
			t.Fatalf("fast = %d, generic = %d", got, want)
		}
		if got := satd4x4IntrinsicsWide(src, stride, ref, stride); got != want {
			t.Fatalf("wide = %d, generic = %d", got, want)
		}
	}
}

func benchIntr(b *testing.B, fn func([]byte, int, []byte, int) int) {
	rng := rand.New(rand.NewSource(3))
	const stride = 32
	src := make([]byte, stride*8)
	ref := make([]byte, stride*8)
	for i := range src {
		src[i] = byte(rng.Intn(256))
		ref[i] = byte(rng.Intn(256))
	}
	b.ResetTimer()
	sink := 0
	for i := 0; i < b.N; i++ {
		sink += fn(src, stride, ref, stride)
	}
	_ = sink
}

func BenchmarkSATD4x4IntrinsicsFast(b *testing.B) { benchIntr(b, satd4x4IntrinsicsFast) }
func BenchmarkSATD4x4IntrinsicsWide(b *testing.B) { benchIntr(b, satd4x4IntrinsicsWide) }
