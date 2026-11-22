package simd

import (
	"bytes"
	"math"
	"math/rand"
	"testing"
)

func randomPlane(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}

func TestAcceleratedIsReported(t *testing.T) {
	t.Logf("assembly kernels in use: %v", Accelerated())
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

var lumaMCTestSizes = [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
var chromaMCTestSizes = [][2]int{{8, 8}, {8, 4}, {8, 2}, {4, 8}, {4, 4}, {4, 2}, {2, 4}, {2, 2}}

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
