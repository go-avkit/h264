package simd

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
)

var blockSizes = [][2]int{
	{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}, {4, 2}, {2, 4}, {2, 2},
}

func randomPlane(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}

func TestSADMatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	const stride = 64
	const rows = 64
	for iter := 0; iter < 4000; iter++ {
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		for _, size := range blockSizes {
			w, h := size[0], size[1]
			srcOff := rng.Intn(stride-w) + rng.Intn(rows-h)*stride
			refOff := rng.Intn(stride-w) + rng.Intn(rows-h)*stride
			got := SAD(src, stride, srcOff, ref, stride, refOff, w, h)
			want := SADGeneric(src, stride, srcOff, ref, stride, refOff, w, h)
			if got != want {
				t.Fatalf("%dx%d at src %d ref %d: SAD = %d, generic = %d", w, h, srcOff, refOff, got, want)
			}
		}
	}
}

func TestSADExtremes(t *testing.T) {
	const stride = 32
	const rows = 32
	cases := []struct {
		name     string
		srcFill  byte
		refFill  byte
		expected func(w, h int) int
	}{
		{"identical", 128, 128, func(w, h int) int { return 0 }},
		{"maximum", 255, 0, func(w, h int) int { return w * h * 255 }},
		{"maximum reversed", 0, 255, func(w, h int) int { return w * h * 255 }},
		{"one apart", 100, 101, func(w, h int) int { return w * h }},
	}
	for _, c := range cases {
		src := make([]byte, stride*rows)
		ref := make([]byte, stride*rows)
		for i := range src {
			src[i] = c.srcFill
			ref[i] = c.refFill
		}
		for _, size := range blockSizes {
			w, h := size[0], size[1]
			got := SAD(src, stride, 0, ref, stride, 0, w, h)
			want := c.expected(w, h)
			if got != want {
				t.Errorf("%s %dx%d: SAD = %d, want %d", c.name, w, h, got, want)
			}
			if g := SADGeneric(src, stride, 0, ref, stride, 0, w, h); g != want {
				t.Errorf("%s %dx%d: generic SAD = %d, want %d", c.name, w, h, g, want)
			}
		}
	}
}

func TestSADReadsOnlyItsBlock(t *testing.T) {
	const stride = 48
	const rows = 48
	for _, size := range blockSizes {
		w, h := size[0], size[1]
		src := make([]byte, stride*rows)
		ref := make([]byte, stride*rows)
		for i := range src {
			src[i] = 200
			ref[i] = 40
		}
		off := 8*stride + 8
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				src[off+y*stride+x] = 10
				ref[off+y*stride+x] = 10
			}
		}
		if got := SAD(src, stride, off, ref, stride, off, w, h); got != 0 {
			t.Errorf("%dx%d: SAD read outside its block, got %d", w, h, got)
		}
	}
}

func TestSADFallsBackWhenSliceIsShort(t *testing.T) {
	const stride = 16
	src := make([]byte, stride*16)
	ref := make([]byte, stride*16)
	for i := range src {
		src[i] = byte(i)
		ref[i] = byte(i * 3)
	}
	got := SAD(src, stride, 0, ref, stride, 0, 16, 16)
	want := SADGeneric(src, stride, 0, ref, stride, 0, 16, 16)
	if got != want {
		t.Fatalf("tight buffer: SAD = %d, generic = %d", got, want)
	}
}

func TestSADStrideIndependence(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, stride := range []int{16, 17, 32, 33, 64, 129} {
		rows := 20
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		for _, size := range blockSizes {
			w, h := size[0], size[1]
			if w > stride {
				continue
			}
			got := SAD(src, stride, 0, ref, stride, 0, w, h)
			want := SADGeneric(src, stride, 0, ref, stride, 0, w, h)
			if got != want {
				t.Errorf("stride %d %dx%d: SAD = %d, generic = %d", stride, w, h, got, want)
			}
		}
	}
}

func TestSADMixedStrides(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	src := randomPlane(rng, 100*40)
	ref := randomPlane(rng, 67*40)
	for _, size := range blockSizes {
		w, h := size[0], size[1]
		got := SAD(src, 100, 0, ref, 67, 0, w, h)
		want := SADGeneric(src, 100, 0, ref, 67, 0, w, h)
		if got != want {
			t.Errorf("%dx%d with differing strides: SAD = %d, generic = %d", w, h, got, want)
		}
	}
}

func TestAcceleratedIsReported(t *testing.T) {
	t.Logf("assembly kernels in use: %v", Accelerated())
}

func FuzzSADAgainstGeneric(f *testing.F) {
	f.Add(int64(1), 0, 0)
	f.Add(int64(500), 3, 1)
	f.Fuzz(func(t *testing.T, seed int64, sizeIdx, offIdx int) {
		if sizeIdx < 0 {
			sizeIdx = -sizeIdx
		}
		if offIdx < 0 {
			offIdx = -offIdx
		}
		size := blockSizes[sizeIdx%len(blockSizes)]
		w, h := size[0], size[1]
		const stride = 40
		const rows = 40
		rng := rand.New(rand.NewSource(seed))
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		off := offIdx % ((rows - h) * stride)
		if off > (rows-h)*stride-w {
			off = (rows-h)*stride - w
		}
		got := SAD(src, stride, off, ref, stride, off, w, h)
		want := SADGeneric(src, stride, off, ref, stride, off, w, h)
		if got != want {
			t.Fatalf("%dx%d at %d: SAD = %d, generic = %d", w, h, off, got, want)
		}
	})
}

func randomBlock(rng *rand.Rand) *[16]int32 {
	var b [16]int32
	for i := range b {
		b[i] = int32(rng.Intn(4001) - 2000)
	}
	return &b
}

func extremeBlocks() []*[16]int32 {
	var lo, hi, mixed [16]int32
	for i := range lo {
		lo[i] = math.MinInt32
		hi[i] = math.MaxInt32
		if i%2 == 0 {
			mixed[i] = math.MinInt32
		} else {
			mixed[i] = math.MaxInt32
		}
	}
	var zero [16]int32
	return []*[16]int32{&lo, &hi, &mixed, &zero}
}

func blocksEqual(a, b *[16]int32) bool {
	return *a == *b
}

func TestForward4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1001))
	for iter := 0; iter < 5000; iter++ {
		orig := randomBlock(rng)
		got := *orig
		want := *orig
		Forward4x4(&got)
		forward4x4Generic(&want)
		if !blocksEqual(&got, &want) {
			t.Fatalf("iter %d: Forward4x4 = %v, generic = %v", iter, got, want)
		}
	}
}

func TestForward4x4MatchesGenericOnExtremes(t *testing.T) {
	for _, orig := range extremeBlocks() {
		got := *orig
		want := *orig
		Forward4x4(&got)
		forward4x4Generic(&want)
		if !blocksEqual(&got, &want) {
			t.Fatalf("extreme block %v: Forward4x4 = %v, generic = %v", *orig, got, want)
		}
	}
}

func TestInverse4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1002))
	for iter := 0; iter < 5000; iter++ {
		orig := randomBlock(rng)
		got := *orig
		want := *orig
		Inverse4x4(&got)
		inverse4x4Generic(&want)
		if !blocksEqual(&got, &want) {
			t.Fatalf("iter %d: Inverse4x4 = %v, generic = %v", iter, got, want)
		}
	}
}

func TestInverse4x4MatchesGenericOnExtremes(t *testing.T) {
	for _, orig := range extremeBlocks() {
		got := *orig
		want := *orig
		Inverse4x4(&got)
		inverse4x4Generic(&want)
		if !blocksEqual(&got, &want) {
			t.Fatalf("extreme block %v: Inverse4x4 = %v, generic = %v", *orig, got, want)
		}
	}
}

func randomTable(rng *rand.Rand, lo, hi int32) *[16]int32 {
	var t [16]int32
	for i := range t {
		t[i] = lo + int32(rng.Intn(int(hi-lo+1)))
	}
	return &t
}

func TestQuant4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1003))
	for iter := 0; iter < 5000; iter++ {
		orig := randomBlock(rng)
		mf := randomTable(rng, 2500, 14000)
		qbits := uint32(15 + rng.Intn(9))
		f := int32(1)<<qbits/3 + int32(rng.Intn(3))

		got := *orig
		want := *orig
		Quant4x4(&got, mf, f, qbits)
		quant4x4Generic(&want, mf, f, qbits)
		if !blocksEqual(&got, &want) {
			t.Fatalf("iter %d: Quant4x4 = %v, generic = %v (mf=%v f=%d qbits=%d)", iter, got, want, *mf, f, qbits)
		}
	}
}

func TestQuant4x4MatchesGenericOnExtremes(t *testing.T) {
	rng := rand.New(rand.NewSource(1004))
	mf := randomTable(rng, 2500, 14000)
	for _, orig := range extremeBlocks() {
		for qbits := uint32(15); qbits <= 23; qbits++ {
			f := int32(1) << qbits / 3
			got := *orig
			want := *orig
			Quant4x4(&got, mf, f, qbits)
			quant4x4Generic(&want, mf, f, qbits)
			if !blocksEqual(&got, &want) {
				t.Fatalf("extreme block %v qbits=%d: Quant4x4 = %v, generic = %v", *orig, qbits, got, want)
			}
		}
	}
}

func TestDequant4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1005))
	for iter := 0; iter < 5000; iter++ {
		orig := randomBlock(rng)
		scale := randomTable(rng, 160, 480)
		shift := uint32(rng.Intn(9))

		got := *orig
		want := *orig
		Dequant4x4(&got, scale, shift)
		dequant4x4Generic(&want, scale, shift)
		if !blocksEqual(&got, &want) {
			t.Fatalf("iter %d: Dequant4x4 = %v, generic = %v (scale=%v shift=%d)", iter, got, want, *scale, shift)
		}
	}
}

func TestDequant4x4MatchesGenericOnExtremes(t *testing.T) {
	rng := rand.New(rand.NewSource(1006))
	scale := randomTable(rng, 160, 480)
	for _, orig := range extremeBlocks() {
		for shift := uint32(0); shift <= 8; shift++ {
			got := *orig
			want := *orig
			Dequant4x4(&got, scale, shift)
			dequant4x4Generic(&want, scale, shift)
			if !blocksEqual(&got, &want) {
				t.Fatalf("extreme block %v shift=%d: Dequant4x4 = %v, generic = %v", *orig, shift, got, want)
			}
		}
	}
}

func TestDequant4x4BothBranchesExercised(t *testing.T) {
	var b [16]int32
	for i := range b {
		b[i] = 100
	}
	var scale [16]int32
	for i := range scale {
		scale[i] = 200
	}

	left := b
	Dequant4x4(&left, &scale, 5)
	wantLeft := b
	dequant4x4Generic(&wantLeft, &scale, 5)
	if !blocksEqual(&left, &wantLeft) {
		t.Fatalf("shift>=4 branch: Dequant4x4 = %v, generic = %v", left, wantLeft)
	}
	if left[0] != 100*200<<1 {
		t.Fatalf("shift=5 sanity: got %d, want %d", left[0], 100*200<<1)
	}

	right := b
	Dequant4x4(&right, &scale, 2)
	wantRight := b
	dequant4x4Generic(&wantRight, &scale, 2)
	if !blocksEqual(&right, &wantRight) {
		t.Fatalf("shift<4 branch: Dequant4x4 = %v, generic = %v", right, wantRight)
	}
}

func randomPlaneOfLen(rng *rand.Rand, n int) []byte {
	return randomPlane(rng, n)
}

func TestAddResidual4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(1007))
	for iter := 0; iter < 4000; iter++ {
		stride := 8 + rng.Intn(40)
		offset := rng.Intn(stride)
		plane := randomPlaneOfLen(rng, offset+3*stride+4+rng.Intn(20))
		var deltas [16]int32
		for i := range deltas {
			deltas[i] = int32(rng.Intn(2001) - 1000)
		}

		got := make([]byte, len(plane))
		copy(got, plane)
		want := make([]byte, len(plane))
		copy(want, plane)

		AddResidual4x4(got, stride, offset, &deltas)
		addResidual4x4Generic(want, stride, offset, &deltas)

		if string(got) != string(want) {
			t.Fatalf("iter %d: AddResidual4x4 = %v, generic = %v (stride=%d offset=%d)", iter, got, want, stride, offset)
		}
	}
}

func TestAddResidual4x4MatchesGenericOnExtremeDeltas(t *testing.T) {
	stride, offset := 9, 4
	basePlane := make([]byte, offset+3*stride+4+5)
	for i := range basePlane {
		basePlane[i] = byte(i * 17)
	}
	for _, orig := range extremeBlocks() {
		got := make([]byte, len(basePlane))
		copy(got, basePlane)
		want := make([]byte, len(basePlane))
		copy(want, basePlane)

		AddResidual4x4(got, stride, offset, orig)
		addResidual4x4Generic(want, stride, offset, orig)

		if string(got) != string(want) {
			t.Fatalf("extreme deltas %v: AddResidual4x4 = %v, generic = %v", *orig, got, want)
		}
	}
}

func TestAddResidual4x4FallsBackWhenSliceIsShort(t *testing.T) {
	stride, offset := 8, 0
	plane := make([]byte, offset+3*stride+4)
	for i := range plane {
		plane[i] = byte(i * 3)
	}
	var deltas [16]int32
	for i := range deltas {
		deltas[i] = int32(i*7 - 50)
	}

	got := make([]byte, len(plane))
	copy(got, plane)
	want := make([]byte, len(plane))
	copy(want, plane)

	AddResidual4x4(got, stride, offset, &deltas)
	addResidual4x4Generic(want, stride, offset, &deltas)
	if string(got) != string(want) {
		t.Fatalf("tight buffer: AddResidual4x4 = %v, generic = %v", got, want)
	}
}

func FuzzQuant4x4AgainstGeneric(f *testing.F) {
	f.Add(int64(1), int32(0), int32(0), int32(0), int32(0), uint32(15), int32(1000))
	f.Fuzz(func(t *testing.T, seed int64, v0, v1, v2, v3 int32, qbitsIn uint32, f int32) {
		rng := rand.New(rand.NewSource(seed))
		var b [16]int32
		b[0], b[1], b[2], b[3] = v0, v1, v2, v3
		for i := 4; i < 16; i++ {
			b[i] = int32(rng.Intn(8001) - 4000)
		}
		mf := randomTable(rng, 2500, 14000)
		qbits := 15 + (qbitsIn % 9)

		got := b
		want := b
		Quant4x4(&got, mf, f, qbits)
		quant4x4Generic(&want, mf, f, qbits)
		if !blocksEqual(&got, &want) {
			t.Fatalf("Quant4x4 = %v, generic = %v (mf=%v f=%d qbits=%d)", got, want, *mf, f, qbits)
		}
	})
}

func FuzzAddResidual4x4AgainstGeneric(f *testing.F) {
	f.Add(int64(1), 8, 0)
	f.Add(int64(2), 64, 40)
	f.Fuzz(func(t *testing.T, seed int64, strideIn, offsetIn int) {
		rng := rand.New(rand.NewSource(seed))
		stride := strideIn % 200
		if stride < 4 {
			stride = 4
		}
		if stride < 0 {
			stride = -stride
		}
		offset := offsetIn % 50
		if offset < 0 {
			offset = -offset
		}
		plane := randomPlaneOfLen(rng, offset+3*stride+4+rng.Intn(30))
		var deltas [16]int32
		for i := range deltas {
			deltas[i] = int32(rng.Intn(4001) - 2000)
		}

		got := make([]byte, len(plane))
		copy(got, plane)
		want := make([]byte, len(plane))
		copy(want, plane)

		AddResidual4x4(got, stride, offset, &deltas)
		addResidual4x4Generic(want, stride, offset, &deltas)
		if string(got) != string(want) {
			t.Fatalf("AddResidual4x4 = %v, generic = %v (stride=%d offset=%d)", got, want, stride, offset)
		}
	})
}

func BenchmarkForward4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := *randomBlock(rng)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		Forward4x4(&blk)
	}
}

func BenchmarkForward4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := *randomBlock(rng)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		forward4x4Generic(&blk)
	}
}

func BenchmarkInverse4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	src := *randomBlock(rng)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		Inverse4x4(&blk)
	}
}

func BenchmarkInverse4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	src := *randomBlock(rng)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		inverse4x4Generic(&blk)
	}
}

func BenchmarkQuant4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	src := *randomBlock(rng)
	mf := randomTable(rng, 2500, 14000)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		Quant4x4(&blk, mf, 1<<20/3, 20)
	}
}

func BenchmarkQuant4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(3))
	src := *randomBlock(rng)
	mf := randomTable(rng, 2500, 14000)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		quant4x4Generic(&blk, mf, 1<<20/3, 20)
	}
}

func BenchmarkDequant4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(4))
	src := *randomBlock(rng)
	scale := randomTable(rng, 160, 480)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		Dequant4x4(&blk, scale, 2)
	}
}

func BenchmarkDequant4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(4))
	src := *randomBlock(rng)
	scale := randomTable(rng, 160, 480)
	blk := src
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		blk = src
		dequant4x4Generic(&blk, scale, 2)
	}
}

func BenchmarkAddResidual4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	stride, offset := 16, 17
	srcPlane := randomPlaneOfLen(rng, offset+3*stride+4)
	plane := make([]byte, len(srcPlane))
	deltas := *randomBlock(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(plane, srcPlane)
		AddResidual4x4(plane, stride, offset, &deltas)
	}
}

func BenchmarkAddResidual4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	stride, offset := 16, 17
	srcPlane := randomPlaneOfLen(rng, offset+3*stride+4)
	plane := make([]byte, len(srcPlane))
	deltas := *randomBlock(rng)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		copy(plane, srcPlane)
		addResidual4x4Generic(plane, stride, offset, &deltas)
	}
}

func BenchmarkSAD16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 64*64)
	ref := randomPlane(rng, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SAD(src, 64, 0, ref, 64, 0, 16, 16)
	}
}

func BenchmarkSAD16x16Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 64*64)
	ref := randomPlane(rng, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SADGeneric(src, 64, 0, ref, 64, 0, 16, 16)
	}
}

func BenchmarkSAD8x8(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 64*64)
	ref := randomPlane(rng, 64*64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SAD(src, 64, 0, ref, 64, 0, 8, 8)
	}
}

var lumaMCTestSizes = [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
var chromaMCTestSizes = [][2]int{{8, 8}, {8, 4}, {8, 2}, {4, 8}, {4, 4}, {4, 2}, {2, 4}, {2, 2}}

func TestSATD4x4MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(20260901))
	const stride = 32
	const rows = 32
	for iter := 0; iter < 8000; iter++ {
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		srcOff := rng.Intn(stride-4) + rng.Intn(rows-4)*stride
		refOff := rng.Intn(stride-4) + rng.Intn(rows-4)*stride
		got := SATD4x4(src, stride, srcOff, ref, stride, refOff)
		want := satd4x4Generic(src[srcOff:], stride, ref[refOff:], stride)
		if got != want {
			t.Fatalf("iter %d: SATD4x4 = %d, generic = %d", iter, got, want)
		}
	}
}

func TestSATD4x4Extremes(t *testing.T) {
	const stride = 16
	cases := []struct {
		srcFill, refFill byte
	}{
		{128, 128}, {255, 0}, {0, 255}, {100, 101}, {0, 0}, {255, 255},
	}
	for _, c := range cases {
		src := make([]byte, stride*8)
		ref := make([]byte, stride*8)
		for i := range src {
			src[i] = c.srcFill
			ref[i] = c.refFill
		}
		got := SATD4x4(src, stride, 0, ref, stride, 0)
		want := satd4x4Generic(src, stride, ref, stride)
		if got != want {
			t.Errorf("fill %d/%d: SATD4x4 = %d, generic = %d", c.srcFill, c.refFill, got, want)
		}
	}
	rng := rand.New(rand.NewSource(5))
	src := make([]byte, stride*8)
	ref := make([]byte, stride*8)
	for i := range src {
		if rng.Intn(2) == 0 {
			src[i] = 0
		} else {
			src[i] = 255
		}
		if rng.Intn(2) == 0 {
			ref[i] = 0
		} else {
			ref[i] = 255
		}
	}
	got := SATD4x4(src, stride, 0, ref, stride, 0)
	want := satd4x4Generic(src, stride, ref, stride)
	if got != want {
		t.Errorf("checkerboard extremes: SATD4x4 = %d, generic = %d", got, want)
	}
}

func TestSATD4x4NonNegative(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	const stride = 16
	for i := 0; i < 2000; i++ {
		src := randomPlane(rng, stride*8)
		ref := randomPlane(rng, stride*8)
		got := SATD4x4(src, stride, 0, ref, stride, 0)
		if got < 0 {
			t.Fatalf("SATD4x4 returned negative value: %d", got)
		}
	}
}

func FuzzSATD4x4AgainstGeneric(f *testing.F) {
	f.Add(int64(1), 0, 0)
	f.Add(int64(500), 5, 3)
	f.Fuzz(func(t *testing.T, seed int64, srcOffIdx, refOffIdx int) {
		const stride = 24
		const rows = 24
		rng := rand.New(rand.NewSource(seed))
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		max := (rows-4)*stride - 4
		if max <= 0 {
			return
		}
		srcOffIdx %= max
		if srcOffIdx < 0 {
			srcOffIdx = -srcOffIdx
		}
		refOffIdx %= max
		if refOffIdx < 0 {
			refOffIdx = -refOffIdx
		}
		got := SATD4x4(src, stride, srcOffIdx, ref, stride, refOffIdx)
		want := satd4x4Generic(src[srcOffIdx:], stride, ref[refOffIdx:], stride)
		if got != want {
			t.Fatalf("SATD4x4 = %d, generic = %d", got, want)
		}
	})
}

func BenchmarkSATD4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 16*8)
	ref := randomPlane(rng, 16*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SATD4x4(src, 16, 0, ref, 16, 0)
	}
}

func BenchmarkSATD4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 16*8)
	ref := randomPlane(rng, 16*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		satd4x4Generic(src, 16, ref, 16)
	}
}

func TestSATD8x8MatchesGenericOnRandomInput(t *testing.T) {
	rng := rand.New(rand.NewSource(20260902))
	const stride = 32
	const rows = 32
	for iter := 0; iter < 8000; iter++ {
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		srcOff := rng.Intn(stride-8) + rng.Intn(rows-8)*stride
		refOff := rng.Intn(stride-8) + rng.Intn(rows-8)*stride
		got := SATD8x8(src, stride, srcOff, ref, stride, refOff)
		want := satd8x8Generic(src[srcOff:], stride, ref[refOff:], stride)
		if got != want {
			t.Fatalf("iter %d: SATD8x8 = %d, generic = %d", iter, got, want)
		}
	}
}

func TestSATD8x8MatchesFourSATD4x4(t *testing.T) {
	rng := rand.New(rand.NewSource(20260903))
	const stride = 32
	const rows = 32
	for iter := 0; iter < 4000; iter++ {
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		srcOff := rng.Intn(stride-8) + rng.Intn(rows-8)*stride
		refOff := rng.Intn(stride-8) + rng.Intn(rows-8)*stride
		got := SATD8x8(src, stride, srcOff, ref, stride, refOff)
		want := 0
		for _, o := range [4][2]int{{0, 0}, {4, 0}, {0, 4}, {4, 4}} {
			want += SATD4x4(src, stride, srcOff+o[1]*stride+o[0], ref, stride, refOff+o[1]*stride+o[0])
		}
		if got != want {
			t.Fatalf("iter %d: SATD8x8 = %d, sum of four SATD4x4 = %d", iter, got, want)
		}
	}
}

func TestSATD8x8Extremes(t *testing.T) {
	const stride = 24
	cases := []struct {
		srcFill, refFill byte
	}{
		{128, 128}, {255, 0}, {0, 255}, {100, 101}, {0, 0}, {255, 255},
	}
	for _, c := range cases {
		src := make([]byte, stride*16)
		ref := make([]byte, stride*16)
		for i := range src {
			src[i] = c.srcFill
			ref[i] = c.refFill
		}
		got := SATD8x8(src, stride, 0, ref, stride, 0)
		want := satd8x8Generic(src, stride, ref, stride)
		if got != want {
			t.Errorf("fill %d/%d: SATD8x8 = %d, generic = %d", c.srcFill, c.refFill, got, want)
		}
	}
	rng := rand.New(rand.NewSource(7))
	src := make([]byte, stride*16)
	ref := make([]byte, stride*16)
	for i := range src {
		if rng.Intn(2) == 0 {
			src[i] = 0
		} else {
			src[i] = 255
		}
		if rng.Intn(2) == 0 {
			ref[i] = 0
		} else {
			ref[i] = 255
		}
	}
	got := SATD8x8(src, stride, 0, ref, stride, 0)
	want := satd8x8Generic(src, stride, ref, stride)
	if got != want {
		t.Errorf("checkerboard extremes: SATD8x8 = %d, generic = %d", got, want)
	}
}

func TestSATD8x8NonNegative(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	const stride = 24
	for i := 0; i < 2000; i++ {
		src := randomPlane(rng, stride*16)
		ref := randomPlane(rng, stride*16)
		got := SATD8x8(src, stride, 0, ref, stride, 0)
		if got < 0 {
			t.Fatalf("SATD8x8 returned negative value: %d", got)
		}
	}
}

func FuzzSATD8x8AgainstGeneric(f *testing.F) {
	f.Add(int64(1), 0, 0)
	f.Add(int64(500), 5, 3)
	f.Fuzz(func(t *testing.T, seed int64, srcOffIdx, refOffIdx int) {
		const stride = 32
		const rows = 32
		rng := rand.New(rand.NewSource(seed))
		src := randomPlane(rng, stride*rows)
		ref := randomPlane(rng, stride*rows)
		max := (rows-8)*stride - 8
		if max <= 0 {
			return
		}
		srcOffIdx %= max
		if srcOffIdx < 0 {
			srcOffIdx = -srcOffIdx
		}
		refOffIdx %= max
		if refOffIdx < 0 {
			refOffIdx = -refOffIdx
		}
		got := SATD8x8(src, stride, srcOffIdx, ref, stride, refOffIdx)
		want := satd8x8Generic(src[srcOffIdx:], stride, ref[refOffIdx:], stride)
		if got != want {
			t.Fatalf("SATD8x8 = %d, generic = %d", got, want)
		}
	})
}

func BenchmarkSATD8x8(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 16*16)
	ref := randomPlane(rng, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SATD8x8(src, 16, 0, ref, 16, 0)
	}
}

func BenchmarkSATD8x8AsFourSATD4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 16*16)
	ref := randomPlane(rng, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		total := 0
		for _, o := range [4][2]int{{0, 0}, {4, 0}, {0, 4}, {4, 4}} {
			total += SATD4x4(src, 16, o[1]*16+o[0], ref, 16, o[1]*16+o[0])
		}
		_ = total
	}
}

func BenchmarkSATD8x8Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src := randomPlane(rng, 16*16)
	ref := randomPlane(rng, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		satd8x8Generic(src, 16, ref, 16)
	}
}

func mcPlane(rng *rand.Rand, w, h, margin int, fill func(x, y int) int) ([]byte, int, int) {
	stride := w + 2*margin
	rows := h + 2*margin
	buf := make([]byte, stride*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < stride; x++ {
			buf[y*stride+x] = byte(fill(x-margin, y-margin) & 0xFF)
		}
	}
	off := margin*stride + margin
	return buf, stride, off
}

func TestSixTapHorizMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(101))
	for _, sz := range lumaMCTestSizes {
		w, h := sz[0], sz[1]
		for trial := 0; trial < 50; trial++ {
			src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
			got := make([]byte, w*h)
			want := make([]byte, w*h)
			SixTapHoriz(got, w, 0, src, stride, off, w, h)
			sixTapHorizGeneric(want, w, 0, src, stride, off, w, h)
			if !bytes.Equal(got, want) {
				t.Fatalf("size %dx%d trial %d: SixTapHoriz mismatch\ngot  %v\nwant %v", w, h, trial, got, want)
			}
		}
	}
}

func TestSixTapVertMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(102))
	for _, sz := range lumaMCTestSizes {
		w, h := sz[0], sz[1]
		for trial := 0; trial < 50; trial++ {
			src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
			got := make([]byte, w*h)
			want := make([]byte, w*h)
			SixTapVert(got, w, 0, src, stride, off, w, h)
			sixTapVertGeneric(want, w, 0, src, stride, off, w, h)
			if !bytes.Equal(got, want) {
				t.Fatalf("size %dx%d trial %d: SixTapVert mismatch\ngot  %v\nwant %v", w, h, trial, got, want)
			}
		}
	}
}

func TestSixTapHVMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(103))
	for _, sz := range lumaMCTestSizes {
		w, h := sz[0], sz[1]
		for trial := 0; trial < 50; trial++ {
			src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
			got := make([]byte, w*h)
			want := make([]byte, w*h)
			SixTapHV(got, w, 0, src, stride, off, w, h)
			sixTapHVGeneric(want, w, 0, src, stride, off, w, h)
			if !bytes.Equal(got, want) {
				t.Fatalf("size %dx%d trial %d: SixTapHV mismatch\ngot  %v\nwant %v", w, h, trial, got, want)
			}
		}
	}
}

func TestSixTapExtremes(t *testing.T) {
	fills := []int{0, 1, 127, 128, 255}
	rng := rand.New(rand.NewSource(104))
	for _, fill := range fills {
		for _, sz := range lumaMCTestSizes {
			w, h := sz[0], sz[1]
			src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return fill })
			for _, kind := range []int{0, 1, 2} {
				got := make([]byte, w*h)
				want := make([]byte, w*h)
				switch kind {
				case 0:
					SixTapHoriz(got, w, 0, src, stride, off, w, h)
					sixTapHorizGeneric(want, w, 0, src, stride, off, w, h)
				case 1:
					SixTapVert(got, w, 0, src, stride, off, w, h)
					sixTapVertGeneric(want, w, 0, src, stride, off, w, h)
				case 2:
					SixTapHV(got, w, 0, src, stride, off, w, h)
					sixTapHVGeneric(want, w, 0, src, stride, off, w, h)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("fill %d size %dx%d kind %d mismatch", fill, w, h, kind)
				}
				for _, v := range got {
					if int(v) != fill {
						t.Fatalf("fill %d size %dx%d kind %d: constant plane not preserved, got %d", fill, w, h, kind, v)
					}
				}
			}
		}
	}
}

func TestSixTapOutputRange(t *testing.T) {
	rng := rand.New(rand.NewSource(105))
	for _, sz := range lumaMCTestSizes {
		w, h := sz[0], sz[1]
		src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
		for _, kind := range []int{0, 1, 2} {
			got := make([]byte, w*h)
			switch kind {
			case 0:
				SixTapHoriz(got, w, 0, src, stride, off, w, h)
			case 1:
				SixTapVert(got, w, 0, src, stride, off, w, h)
			case 2:
				SixTapHV(got, w, 0, src, stride, off, w, h)
			}
			for _, v := range got {
				if int(v) < 0 || int(v) > 255 {
					t.Fatalf("size %dx%d kind %d: sample out of range %d", w, h, kind, v)
				}
			}
		}
	}
}

func TestSixTapExactMinimumMargin(t *testing.T) {
	rng := rand.New(rand.NewSource(106))
	for _, sz := range lumaMCTestSizes {
		w, h := sz[0], sz[1]
		stride := w + 5
		buf := randomPlane(rng, stride*(h+5))
		off := 2*stride + 2
		got := make([]byte, w*h)
		want := make([]byte, w*h)

		SixTapHoriz(got, w, 0, buf, stride, off, w, h)
		sixTapHorizGeneric(want, w, 0, buf, stride, off, w, h)
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d: exact-margin SixTapHoriz mismatch\ngot  %v\nwant %v", w, h, got, want)
		}
		SixTapVert(got, w, 0, buf, stride, off, w, h)
		sixTapVertGeneric(want, w, 0, buf, stride, off, w, h)
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d: exact-margin SixTapVert mismatch\ngot  %v\nwant %v", w, h, got, want)
		}
		SixTapHV(got, w, 0, buf, stride, off, w, h)
		sixTapHVGeneric(want, w, 0, buf, stride, off, w, h)
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d: exact-margin SixTapHV mismatch\ngot  %v\nwant %v", w, h, got, want)
		}
	}
}

func TestSixTapUnsupportedSizeFallsBackToGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(107))
	w, h := 12, 6
	src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
	got := make([]byte, w*h)
	want := make([]byte, w*h)
	SixTapHoriz(got, w, 0, src, stride, off, w, h)
	sixTapHorizGeneric(want, w, 0, src, stride, off, w, h)
	if !bytes.Equal(got, want) {
		t.Fatalf("unsupported size %dx%d: SixTapHoriz mismatch\ngot  %v\nwant %v", w, h, got, want)
	}
}

func FuzzSixTapAgainstGeneric(f *testing.F) {
	f.Add(int64(1), 0, 0)
	f.Add(int64(9), 3, 2)
	f.Fuzz(func(t *testing.T, seed int64, sizeIdx, kind int) {
		if sizeIdx < 0 {
			sizeIdx = -sizeIdx
		}
		if kind < 0 {
			kind = -kind
		}
		sz := lumaMCTestSizes[sizeIdx%len(lumaMCTestSizes)]
		w, h := sz[0], sz[1]
		rng := rand.New(rand.NewSource(seed))
		src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
		got := make([]byte, w*h)
		want := make([]byte, w*h)
		switch kind % 3 {
		case 0:
			SixTapHoriz(got, w, 0, src, stride, off, w, h)
			sixTapHorizGeneric(want, w, 0, src, stride, off, w, h)
		case 1:
			SixTapVert(got, w, 0, src, stride, off, w, h)
			sixTapVertGeneric(want, w, 0, src, stride, off, w, h)
		case 2:
			SixTapHV(got, w, 0, src, stride, off, w, h)
			sixTapHVGeneric(want, w, 0, src, stride, off, w, h)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d kind %d mismatch\ngot  %v\nwant %v", w, h, kind%3, got, want)
		}
	})
}

func TestBilinearChromaMatchesGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(201))
	for _, sz := range chromaMCTestSizes {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 8; yFrac++ {
			for xFrac := 0; xFrac < 8; xFrac++ {
				src, stride, off := mcPlane(rng, w, h, 4, func(x, y int) int { return rng.Intn(256) })
				got := make([]byte, w*h)
				want := make([]byte, w*h)
				BilinearChroma(got, w, 0, src, stride, off, w, h, xFrac, yFrac)
				bilinearChromaGeneric(want, w, 0, src, stride, off, w, h, xFrac, yFrac)
				if !bytes.Equal(got, want) {
					t.Fatalf("size %dx%d frac(%d,%d): mismatch\ngot  %v\nwant %v", w, h, xFrac, yFrac, got, want)
				}
			}
		}
	}
}

func TestBilinearChromaExtremes(t *testing.T) {
	fills := []int{0, 1, 127, 128, 255}
	rng := rand.New(rand.NewSource(202))
	for _, fill := range fills {
		for _, sz := range chromaMCTestSizes {
			w, h := sz[0], sz[1]
			src, stride, off := mcPlane(rng, w, h, 4, func(x, y int) int { return fill })
			for yFrac := 0; yFrac < 8; yFrac += 3 {
				for xFrac := 0; xFrac < 8; xFrac += 3 {
					got := make([]byte, w*h)
					BilinearChroma(got, w, 0, src, stride, off, w, h, xFrac, yFrac)
					for _, v := range got {
						if int(v) != fill {
							t.Fatalf("fill %d size %dx%d frac(%d,%d): constant plane not preserved, got %d", fill, w, h, xFrac, yFrac, v)
						}
					}
				}
			}
		}
	}
}

func TestBilinearChromaExactMinimumMargin(t *testing.T) {
	rng := rand.New(rand.NewSource(203))
	for _, sz := range chromaMCTestSizes {
		w, h := sz[0], sz[1]
		stride := w + 1
		buf := randomPlane(rng, stride*(h+1))
		got := make([]byte, w*h)
		want := make([]byte, w*h)
		BilinearChroma(got, w, 0, buf, stride, 0, w, h, 3, 5)
		bilinearChromaGeneric(want, w, 0, buf, stride, 0, w, h, 3, 5)
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d: exact-margin BilinearChroma mismatch\ngot  %v\nwant %v", w, h, got, want)
		}
	}
}

func TestBilinearChromaUnsupportedSizeFallsBackToGeneric(t *testing.T) {
	rng := rand.New(rand.NewSource(204))
	w, h := 6, 3
	src, stride, off := mcPlane(rng, w, h, 4, func(x, y int) int { return rng.Intn(256) })
	got := make([]byte, w*h)
	want := make([]byte, w*h)
	BilinearChroma(got, w, 0, src, stride, off, w, h, 2, 6)
	bilinearChromaGeneric(want, w, 0, src, stride, off, w, h, 2, 6)
	if !bytes.Equal(got, want) {
		t.Fatalf("unsupported size %dx%d: BilinearChroma mismatch\ngot  %v\nwant %v", w, h, got, want)
	}
}

func FuzzBilinearChromaAgainstGeneric(f *testing.F) {
	f.Add(int64(1), 0, 0, 0)
	f.Add(int64(9), 3, 5, 2)
	f.Fuzz(func(t *testing.T, seed int64, sizeIdx, xFracIn, yFracIn int) {
		if sizeIdx < 0 {
			sizeIdx = -sizeIdx
		}
		sz := chromaMCTestSizes[sizeIdx%len(chromaMCTestSizes)]
		w, h := sz[0], sz[1]
		xFrac := ((xFracIn % 8) + 8) % 8
		yFrac := ((yFracIn % 8) + 8) % 8
		rng := rand.New(rand.NewSource(seed))
		src, stride, off := mcPlane(rng, w, h, 4, func(x, y int) int { return rng.Intn(256) })
		got := make([]byte, w*h)
		want := make([]byte, w*h)
		BilinearChroma(got, w, 0, src, stride, off, w, h, xFrac, yFrac)
		bilinearChromaGeneric(want, w, 0, src, stride, off, w, h, xFrac, yFrac)
		if !bytes.Equal(got, want) {
			t.Fatalf("size %dx%d frac(%d,%d) mismatch\ngot  %v\nwant %v", w, h, xFrac, yFrac, got, want)
		}
	})
}

func BenchmarkSixTapHoriz16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SixTapHoriz(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkSixTapHoriz16x16Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sixTapHorizGeneric(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkSixTapVert16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SixTapVert(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkSixTapVert16x16Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sixTapVertGeneric(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkSixTapHV16x16(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		SixTapHV(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkSixTapHV16x16Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 16, 16, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sixTapHVGeneric(dst, 16, 0, src, stride, off, 16, 16)
	}
}

func BenchmarkBilinearChroma8x8(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 8, 8, 4, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 8*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BilinearChroma(dst, 8, 0, src, stride, off, 8, 8, 3, 5)
	}
}

func BenchmarkBilinearChroma8x8Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, 8, 8, 4, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 8*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bilinearChromaGeneric(dst, 8, 0, src, stride, off, 8, 8, 3, 5)
	}
}

func benchSixTapSize(b *testing.B, w, h int, fn func(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int)) {
	rng := rand.New(rand.NewSource(1))
	src, stride, off := mcPlane(rng, w, h, 8, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, w*h)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		fn(dst, w, 0, src, stride, off, w, h)
	}
}

func BenchmarkSixTapHV4x4(b *testing.B)           { benchSixTapSize(b, 4, 4, SixTapHV) }
func BenchmarkSixTapHV4x4Generic(b *testing.B)    { benchSixTapSize(b, 4, 4, sixTapHVGeneric) }
func BenchmarkSixTapHV8x4(b *testing.B)           { benchSixTapSize(b, 8, 4, SixTapHV) }
func BenchmarkSixTapHV8x8(b *testing.B)           { benchSixTapSize(b, 8, 8, SixTapHV) }
func BenchmarkSixTapHV8x8Generic(b *testing.B)    { benchSixTapSize(b, 8, 8, sixTapHVGeneric) }
func BenchmarkSixTapVert4x4(b *testing.B)         { benchSixTapSize(b, 4, 4, SixTapVert) }
func BenchmarkSixTapVert8x8(b *testing.B)         { benchSixTapSize(b, 8, 8, SixTapVert) }
func BenchmarkSixTapHoriz8x8(b *testing.B)        { benchSixTapSize(b, 8, 8, SixTapHoriz) }
func BenchmarkSixTapHoriz8x8Generic(b *testing.B) { benchSixTapSize(b, 8, 8, sixTapHorizGeneric) }

func BenchmarkSAD4x4(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	const stride = 32
	src := randomPlane(rng, stride*16)
	ref := randomPlane(rng, stride*16)
	b.ResetTimer()
	sink := 0
	for i := 0; i < b.N; i++ {
		sink += SAD(src, stride, 0, ref, stride, 0, 4, 4)
	}
	_ = sink
}

func BenchmarkSAD4x4Generic(b *testing.B) {
	rng := rand.New(rand.NewSource(2))
	const stride = 32
	src := randomPlane(rng, stride*16)
	ref := randomPlane(rng, stride*16)
	b.ResetTimer()
	sink := 0
	for i := 0; i < b.N; i++ {
		sink += SADGeneric(src, stride, 0, ref, stride, 0, 4, 4)
	}
	_ = sink
}
