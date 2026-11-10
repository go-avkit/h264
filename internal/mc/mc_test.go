package mc

import (
	"math/rand"
	"testing"
)

func refClip1(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

type getter func(x, y int) int

func refB1(get getter, x, y int) int {
	return get(x-2, y) - 5*get(x-1, y) + 20*get(x, y) + 20*get(x+1, y) - 5*get(x+2, y) + get(x+3, y)
}

func refH1(get getter, x, y int) int {
	return get(x, y-2) - 5*get(x, y-1) + 20*get(x, y) + 20*get(x, y+1) - 5*get(x, y+2) + get(x, y+3)
}

func refB(get getter, x, y int) int {
	return refClip1((refB1(get, x, y) + 16) >> 5)
}

func refH(get getter, x, y int) int {
	return refClip1((refH1(get, x, y) + 16) >> 5)
}

func refJ(get getter, x, y int) int {
	cc := refB1(get, x, y-2)
	dd := refB1(get, x, y-1)
	h1 := refB1(get, x, y)
	m1 := refB1(get, x, y+1)
	ee := refB1(get, x, y+2)
	ff := refB1(get, x, y+3)
	j1 := cc - 5*dd + 20*h1 + 20*m1 - 5*ee + ff
	return refClip1((j1 + 512) >> 10)
}

func wrongJ(get getter, x, y int) int {
	cc := refB(get, x, y-2)
	dd := refB(get, x, y-1)
	h1 := refB(get, x, y)
	m1 := refB(get, x, y+1)
	ee := refB(get, x, y+2)
	ff := refB(get, x, y+3)
	j1 := cc - 5*dd + 20*h1 + 20*m1 - 5*ee + ff
	return refClip1((j1 + 16) >> 5)
}

func refLumaTable(get getter, x, y int) [4][4]int {
	g := get(x, y)
	hh := get(x+1, y)
	mm := get(x, y+1)
	b := refB(get, x, y)
	h := refH(get, x, y)
	j := refJ(get, x, y)
	m := refH(get, x+1, y)
	s := refB(get, x, y+1)
	a := (g + b + 1) >> 1
	c := (hh + b + 1) >> 1
	d := (g + h + 1) >> 1
	n := (mm + h + 1) >> 1
	e := (b + h + 1) >> 1
	f := (b + j + 1) >> 1
	gg := (b + m + 1) >> 1
	i := (h + j + 1) >> 1
	k := (j + m + 1) >> 1
	p := (h + s + 1) >> 1
	q := (j + s + 1) >> 1
	r := (m + s + 1) >> 1
	var tbl [4][4]int
	tbl[0][0] = g
	tbl[0][1] = a
	tbl[0][2] = b
	tbl[0][3] = c
	tbl[1][0] = d
	tbl[1][1] = e
	tbl[1][2] = f
	tbl[1][3] = gg
	tbl[2][0] = h
	tbl[2][1] = i
	tbl[2][2] = j
	tbl[2][3] = k
	tbl[3][0] = n
	tbl[3][1] = p
	tbl[3][2] = q
	tbl[3][3] = r
	return tbl
}

func refLumaVal(get getter, x, y, xFrac, yFrac int) int {
	tbl := refLumaTable(get, x, y)
	return tbl[yFrac][xFrac]
}

func refChromaVal(get getter, x, y, xFrac, yFrac int) int {
	a := get(x, y)
	b := get(x+1, y)
	c := get(x, y+1)
	d := get(x+1, y+1)
	return ((8-xFrac)*(8-yFrac)*a + xFrac*(8-yFrac)*b + (8-xFrac)*yFrac*c + xFrac*yFrac*d + 32) >> 6
}

type plane struct {
	buf    []byte
	stride int
	margin int
}

func newPlane(w, h, margin int, fill func(x, y int) int) *plane {
	stride := w + 2*margin
	rows := h + 2*margin
	buf := make([]byte, stride*rows)
	for y := 0; y < rows; y++ {
		for x := 0; x < stride; x++ {
			buf[y*stride+x] = byte(fill(x-margin, y-margin))
		}
	}
	return &plane{buf: buf, stride: stride, margin: margin}
}

func (p *plane) off(x, y int) int {
	return (y+p.margin)*p.stride + (x + p.margin)
}

func (p *plane) get(x, y int) int {
	return int(p.buf[p.off(x, y)])
}

var lumaSizes = [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
var chromaSizes = [][2]int{{8, 8}, {8, 4}, {4, 8}, {4, 4}, {4, 2}, {2, 4}, {2, 2}}

const lumaMargin = 24
const chromaMargin = 16

func TestLumaExhaustiveCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	count := 0
	for _, sz := range lumaSizes {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 4; yFrac++ {
			for xFrac := 0; xFrac < 4; xFrac++ {
				for trial := 0; trial < 60; trial++ {
					p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
					mvx := (0 << 2) | xFrac
					mvy := (0 << 2) | yFrac
					dst := make([]byte, w*h)
					PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, mvx, mvy)
					for y := 0; y < h; y++ {
						for x := 0; x < w; x++ {
							want := refLumaVal(p.get, x, y, xFrac, yFrac)
							got := int(dst[y*w+x])
							if want != got {
								t.Fatalf("size %dx%d frac(%d,%d) trial %d pixel (%d,%d): want %d got %d", w, h, xFrac, yFrac, trial, x, y, want, got)
							}
							if want < 0 || want > 255 {
								t.Fatalf("reference out of range: %d", want)
							}
						}
					}
					count++
				}
			}
		}
	}
	for extra := 0; extra < 14000; extra++ {
		sz := lumaSizes[rng.Intn(len(lumaSizes))]
		w, h := sz[0], sz[1]
		mvx := rng.Intn(4)
		mvy := rng.Intn(4)
		xFrac := mvx & 3
		yFrac := mvy & 3
		p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
		dst := make([]byte, w*h)
		PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, mvx, mvy)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := refLumaVal(p.get, x, y, xFrac, yFrac)
				got := int(dst[y*w+x])
				if want != got {
					t.Fatalf("random size %dx%d frac(%d,%d) pixel (%d,%d): want %d got %d", w, h, xFrac, yFrac, x, y, want, got)
				}
			}
		}
		count++
	}
	if count < 20000 {
		t.Fatalf("only ran %d cross-check cases, need at least 20000", count)
	}
}

func TestLumaIntegerPositionIsCopy(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	for _, sz := range lumaSizes {
		w, h := sz[0], sz[1]
		p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
		dst := make([]byte, w*h)
		PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, 0, 0)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := p.get(x, y)
				got := int(dst[y*w+x])
				if want != got {
					t.Fatalf("integer copy mismatch size %dx%d (%d,%d): want %d got %d", w, h, x, y, want, got)
				}
			}
		}
	}
}

func TestLumaConstantPlanePreserved(t *testing.T) {
	constants := []int{0, 1, 127, 128, 255}
	for _, c := range constants {
		for _, sz := range lumaSizes {
			w, h := sz[0], sz[1]
			p := newPlane(w, h, lumaMargin, func(x, y int) int { return c })
			for yFrac := 0; yFrac < 4; yFrac++ {
				for xFrac := 0; xFrac < 4; xFrac++ {
					dst := make([]byte, w*h)
					PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
					for y := 0; y < h; y++ {
						for x := 0; x < w; x++ {
							got := int(dst[y*w+x])
							if got != c {
								t.Fatalf("constant %d size %dx%d frac(%d,%d) pixel (%d,%d): got %d", c, w, h, xFrac, yFrac, x, y, got)
							}
						}
					}
				}
			}
		}
	}
}

func TestLumaCentreUsesUnclippedIntermediate(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	w, h := 8, 8
	p := newPlane(w, h, lumaMargin, func(x, y int) int {
		if rng.Intn(2) == 0 {
			return 0
		}
		return 255
	})
	dst := make([]byte, w*h)
	PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, 2, 2)

	diff := false
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := refJ(p.get, x, y)
			got := int(dst[y*w+x])
			if want != got {
				t.Fatalf("centre position mismatch at (%d,%d): want %d got %d", x, y, want, got)
			}
			wj := wrongJ(p.get, x, y)
			if wj != want {
				diff = true
			}
		}
	}
	if !diff {
		t.Fatalf("test does not discriminate: wrong-clipped-intermediate result matches correct result everywhere")
	}
}

func TestLumaOutputRange(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for _, sz := range lumaSizes {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 4; yFrac++ {
			for xFrac := 0; xFrac < 4; xFrac++ {
				p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
				dst := make([]byte, w*h)
				PredictLuma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				for _, v := range dst {
					if int(v) < 0 || int(v) > 255 {
						t.Fatalf("sample out of range: %d", v)
					}
				}
			}
		}
	}
}

func TestLumaDestinationIsolation(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for _, sz := range lumaSizes {
		w, h := sz[0], sz[1]
		p := newPlane(w, h, lumaMargin, func(x, y int) int { return rng.Intn(256) })
		dstW, dstH := w+8, h+8
		dstStride := dstW
		dst := make([]byte, dstStride*dstH)
		for i := range dst {
			dst[i] = 0xAA
		}
		dstOff := 4*dstStride + 4
		PredictLuma(dst, dstStride, dstOff, p.buf, p.stride, p.off(0, 0), w, h, 1, 3)
		for y := 0; y < dstH; y++ {
			for x := 0; x < dstW; x++ {
				inside := x >= 4 && x < 4+w && y >= 4 && y < 4+h
				if !inside && dst[y*dstStride+x] != 0xAA {
					t.Fatalf("write outside block at (%d,%d): %d", x, y, dst[y*dstStride+x])
				}
			}
		}
	}
}

func TestLumaNegativeMotionVector(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	w, h := 8, 8
	p := newPlane(64, 64, lumaMargin, func(x, y int) int { return rng.Intn(256) })
	blockX, blockY := 32, 32

	if -5>>2 != -2 {
		t.Fatalf("assumption about arithmetic shift broken")
	}
	if -5&3 != 3 {
		t.Fatalf("assumption about mask on negative broken")
	}

	mvx := -5
	mvy := -9
	intDX := mvx >> 2
	intDY := mvy >> 2
	xFrac := mvx & 3
	yFrac := mvy & 3

	dst := make([]byte, w*h)
	PredictLuma(dst, w, 0, p.buf, p.stride, p.off(blockX, blockY), w, h, mvx, mvy)

	get := func(x, y int) int { return p.get(blockX+intDX+x, blockY+intDY+y) }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := refLumaVal(get, x, y, xFrac, yFrac)
			got := int(dst[y*w+x])
			if want != got {
				t.Fatalf("negative mv mismatch at (%d,%d): want %d got %d", x, y, want, got)
			}
		}
	}
}

func TestChromaExhaustiveCrossCheck(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	for _, sz := range chromaSizes {
		w, h := sz[0], sz[1]
		for yFrac := 0; yFrac < 8; yFrac++ {
			for xFrac := 0; xFrac < 8; xFrac++ {
				p := newPlane(w, h, chromaMargin, func(x, y int) int { return rng.Intn(256) })
				dst := make([]byte, w*h)
				PredictChroma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
				for y := 0; y < h; y++ {
					for x := 0; x < w; x++ {
						want := refChromaVal(p.get, x, y, xFrac, yFrac)
						got := int(dst[y*w+x])
						if want != got {
							t.Fatalf("chroma size %dx%d frac(%d,%d) pixel (%d,%d): want %d got %d", w, h, xFrac, yFrac, x, y, want, got)
						}
					}
				}
			}
		}
	}
}

func TestChromaIntegerPositionIsCopy(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for _, sz := range chromaSizes {
		w, h := sz[0], sz[1]
		p := newPlane(w, h, chromaMargin, func(x, y int) int { return rng.Intn(256) })
		dst := make([]byte, w*h)
		PredictChroma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, 0, 0)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				want := p.get(x, y)
				got := int(dst[y*w+x])
				if want != got {
					t.Fatalf("chroma integer copy mismatch size %dx%d (%d,%d): want %d got %d", w, h, x, y, want, got)
				}
			}
		}
	}
}

func TestChromaConstantPlanePreserved(t *testing.T) {
	constants := []int{0, 1, 127, 128, 255}
	for _, c := range constants {
		for _, sz := range chromaSizes {
			w, h := sz[0], sz[1]
			p := newPlane(w, h, chromaMargin, func(x, y int) int { return c })
			for yFrac := 0; yFrac < 8; yFrac++ {
				for xFrac := 0; xFrac < 8; xFrac++ {
					dst := make([]byte, w*h)
					PredictChroma(dst, w, 0, p.buf, p.stride, p.off(0, 0), w, h, xFrac, yFrac)
					for y := 0; y < h; y++ {
						for x := 0; x < w; x++ {
							got := int(dst[y*w+x])
							if got != c {
								t.Fatalf("chroma constant %d size %dx%d frac(%d,%d) pixel (%d,%d): got %d", c, w, h, xFrac, yFrac, x, y, got)
							}
						}
					}
				}
			}
		}
	}
}

func TestChromaDestinationIsolation(t *testing.T) {
	rng := rand.New(rand.NewSource(8))
	for _, sz := range chromaSizes {
		w, h := sz[0], sz[1]
		p := newPlane(w, h, chromaMargin, func(x, y int) int { return rng.Intn(256) })
		dstW, dstH := w+8, h+8
		dstStride := dstW
		dst := make([]byte, dstStride*dstH)
		for i := range dst {
			dst[i] = 0x55
		}
		dstOff := 3*dstStride + 3
		PredictChroma(dst, dstStride, dstOff, p.buf, p.stride, p.off(0, 0), w, h, 3, 5)
		for y := 0; y < dstH; y++ {
			for x := 0; x < dstW; x++ {
				inside := x >= 3 && x < 3+w && y >= 3 && y < 3+h
				if !inside && dst[y*dstStride+x] != 0x55 {
					t.Fatalf("chroma write outside block at (%d,%d): %d", x, y, dst[y*dstStride+x])
				}
			}
		}
	}
}

func TestChromaNegativeMotionVector(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	w, h := 4, 4
	p := newPlane(32, 32, chromaMargin, func(x, y int) int { return rng.Intn(256) })
	blockX, blockY := 16, 16

	mvx := -11
	mvy := -3
	intDX := mvx >> 3
	intDY := mvy >> 3
	xFrac := mvx & 7
	yFrac := mvy & 7

	dst := make([]byte, w*h)
	PredictChroma(dst, w, 0, p.buf, p.stride, p.off(blockX, blockY), w, h, mvx, mvy)

	get := func(x, y int) int { return p.get(blockX+intDX+x, blockY+intDY+y) }
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			want := refChromaVal(get, x, y, xFrac, yFrac)
			got := int(dst[y*w+x])
			if want != got {
				t.Fatalf("chroma negative mv mismatch at (%d,%d): want %d got %d", x, y, want, got)
			}
		}
	}
}

func TestAverageExact(t *testing.T) {
	rng := rand.New(rand.NewSource(10))
	w, h := 13, 7
	aBuf := make([]byte, w*h)
	bBuf := make([]byte, w*h)
	for i := range aBuf {
		aBuf[i] = byte(rng.Intn(256))
		bBuf[i] = byte(rng.Intn(256))
	}
	dst := make([]byte, w*h)
	Average(dst, w, 0, aBuf, w, 0, bBuf, w, 0, w, h)
	for i := range dst {
		want := (int(aBuf[i]) + int(bBuf[i]) + 1) >> 1
		if int(dst[i]) != want {
			t.Fatalf("average mismatch at %d: want %d got %d", i, want, dst[i])
		}
	}

	aBuf[0] = 0
	bBuf[0] = 1
	Average(dst, w, 0, aBuf, w, 0, bBuf, w, 0, w, h)
	if dst[0] != 1 {
		t.Fatalf("average rounding mismatch: want 1 got %d", dst[0])
	}
}

func TestAverageDestinationIsolation(t *testing.T) {
	w, h := 6, 5
	aBuf := make([]byte, w*h)
	bBuf := make([]byte, w*h)
	for i := range aBuf {
		aBuf[i] = byte(i)
		bBuf[i] = byte(i * 2)
	}
	dstW, dstH := w+6, h+6
	dstStride := dstW
	dst := make([]byte, dstStride*dstH)
	for i := range dst {
		dst[i] = 0xF0
	}
	dstOff := 3*dstStride + 3
	Average(dst, dstStride, dstOff, aBuf, w, 0, bBuf, w, 0, w, h)
	for y := 0; y < dstH; y++ {
		for x := 0; x < dstW; x++ {
			inside := x >= 3 && x < 3+w && y >= 3 && y < 3+h
			if !inside && dst[y*dstStride+x] != 0xF0 {
				t.Fatalf("average write outside block at (%d,%d): %d", x, y, dst[y*dstStride+x])
			}
		}
	}
}

func benchLumaPlane() *plane {
	rng := rand.New(rand.NewSource(42))
	return newPlane(16, 16, lumaMargin, func(x, y int) int { return rng.Intn(256) })
}

func BenchmarkPredictLuma16x16_00(b *testing.B) {
	p := benchLumaPlane()
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredictLuma(dst, 16, 0, p.buf, p.stride, p.off(0, 0), 16, 16, 0, 0)
	}
}

func BenchmarkPredictLuma16x16_20(b *testing.B) {
	p := benchLumaPlane()
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredictLuma(dst, 16, 0, p.buf, p.stride, p.off(0, 0), 16, 16, 2, 0)
	}
}

func BenchmarkPredictLuma16x16_02(b *testing.B) {
	p := benchLumaPlane()
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredictLuma(dst, 16, 0, p.buf, p.stride, p.off(0, 0), 16, 16, 0, 2)
	}
}

func BenchmarkPredictLuma16x16_22(b *testing.B) {
	p := benchLumaPlane()
	dst := make([]byte, 16*16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredictLuma(dst, 16, 0, p.buf, p.stride, p.off(0, 0), 16, 16, 2, 2)
	}
}

func BenchmarkPredictChroma8x8(b *testing.B) {
	rng := rand.New(rand.NewSource(43))
	p := newPlane(8, 8, chromaMargin, func(x, y int) int { return rng.Intn(256) })
	dst := make([]byte, 8*8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PredictChroma(dst, 8, 0, p.buf, p.stride, p.off(0, 0), 8, 8, 4, 4)
	}
}

func refWeightUni(v, weight, offset, logWD int) int {
	if logWD >= 1 {
		return clip1(((v*weight + 1<<uint(logWD-1)) >> uint(logWD)) + offset)
	}
	return clip1(v*weight + offset)
}

func TestWeightUniMatchesTheSpecFormula(t *testing.T) {
	src := make([]byte, 8*8)
	for i := range src {
		src[i] = byte(i * 251 % 256)
	}
	for logWD := 0; logWD <= 7; logWD++ {
		for _, weight := range []int{-128, -1, 0, 1, 47, 127} {
			for _, offset := range []int{-128, -1, 0, 5, 127} {
				got := append([]byte(nil), src...)
				WeightUni(got, 8, 0, 8, 8, weight, offset, logWD)
				for i := range src {
					want := refWeightUni(int(src[i]), weight, offset, logWD)
					if int(got[i]) != want {
						t.Fatalf("WeightUni(logWD %d weight %d offset %d) sample %d = %d, want %d",
							logWD, weight, offset, i, got[i], want)
					}
				}
			}
		}
	}
}

func TestWeightUniLeavesTheRestOfThePlaneAlone(t *testing.T) {
	plane := make([]byte, 16*16)
	for i := range plane {
		plane[i] = 100
	}
	WeightUni(plane, 16, 16*4+4, 8, 8, 1, 7, 0)
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			inside := y >= 4 && y < 12 && x >= 4 && x < 12
			if inside != (plane[y*16+x] == 100) {
				continue
			}
			t.Fatalf("sample (%d,%d) = %d, inside %v", x, y, plane[y*16+x], inside)
		}
	}
}

func TestWeightBiMatchesTheSpecFormula(t *testing.T) {
	a := make([]byte, 8*8)
	b := make([]byte, 8*8)
	for i := range a {
		a[i] = byte(i * 7 % 256)
		b[i] = byte(255 - i*5%256)
	}
	dst := make([]byte, 8*8)
	for logWD := 0; logWD <= 7; logWD++ {
		for _, w := range [][2]int{{1, 1}, {64, 64}, {-32, 96}, {127, -128}} {
			for _, o := range [][2]int{{0, 0}, {3, -4}, {127, 127}} {
				WeightBi(dst, 8, 0, a, 8, 0, b, 8, 0, 8, 8, w[0], w[1], o[0], o[1], logWD)
				for i := range dst {
					want := clip1(((int(a[i])*w[0] + int(b[i])*w[1] + 1<<uint(logWD)) >> uint(logWD+1)) +
						(o[0]+o[1]+1)>>1)
					if int(dst[i]) != want {
						t.Fatalf("WeightBi(logWD %d w %v o %v) sample %d = %d, want %d",
							logWD, w, o, i, dst[i], want)
					}
				}
			}
		}
	}
}

func TestWeightUniWithUnitWeightIsIdentity(t *testing.T) {
	src := make([]byte, 64)
	for i := range src {
		src[i] = byte(i * 3 % 256)
	}
	got := append([]byte(nil), src...)
	WeightUni(got, 8, 0, 8, 8, 32, 0, 5)
	for i := range src {
		if got[i] != src[i] {
			t.Fatalf("sample %d = %d, want %d", i, got[i], src[i])
		}
	}
}

func specWeightUni(a, w, o, logWD int) int {
	if logWD >= 1 {
		return clipToByte((a*w+1<<uint(logWD-1))>>uint(logWD) + o)
	}
	return clipToByte(a*w + o)
}

func specWeightBi(a, b, w0, w1, o0, o1, logWD int) int {
	return clipToByte(((a*w0+b*w1+1<<uint(logWD))>>uint(logWD+1) + (o0+o1+1)>>1))
}

func ffmpegWeightBi(a, b, w0, w1, o0, o1, logWD int) int {
	off := ((o0 + o1 + 1) | 1) << uint(logWD)
	return clipToByte((a*w0 + b*w1 + off) >> uint(logWD+1))
}

func clipToByte(v int) int {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func TestWeightBiMatchesTheSpecificationAndTheReferenceDecoder(t *testing.T) {
	pixels := []int{0, 1, 17, 63, 128, 200, 214, 254, 255}
	for logWD := 0; logWD <= 7; logWD++ {
		for w0 := -128; w0 <= 127; w0 += 7 {
			for w1 := -128; w1 <= 127; w1 += 11 {
				for o0 := -128; o0 <= 127; o0 += 13 {
					for o1 := -128; o1 <= 127; o1 += 17 {
						for _, a := range pixels {
							for _, b := range pixels {
								src := []byte{byte(a)}
								ref := []byte{byte(b)}
								dst := make([]byte, 1)
								WeightBi(dst, 1, 0, src, 1, 0, ref, 1, 0, 1, 1, w0, w1, o0, o1, logWD)
								got := int(dst[0])
								if want := specWeightBi(a, b, w0, w1, o0, o1, logWD); got != want {
									t.Fatalf("WeightBi(%d,%d,w0=%d,w1=%d,o0=%d,o1=%d,logWD=%d) = %d, the specification says %d",
										a, b, w0, w1, o0, o1, logWD, got, want)
								}
								if want := ffmpegWeightBi(a, b, w0, w1, o0, o1, logWD); got != want {
									t.Fatalf("WeightBi(%d,%d,w0=%d,w1=%d,o0=%d,o1=%d,logWD=%d) = %d, the reference decoder's own arithmetic says %d",
										a, b, w0, w1, o0, o1, logWD, got, want)
								}
							}
						}
					}
				}
			}
		}
	}
}

func TestWeightUniMatchesTheSpecification(t *testing.T) {
	pixels := []int{0, 1, 17, 63, 128, 200, 214, 254, 255}
	for logWD := 0; logWD <= 7; logWD++ {
		for w := -128; w <= 127; w += 3 {
			for o := -128; o <= 127; o += 5 {
				for _, a := range pixels {
					dst := []byte{byte(a)}
					WeightUni(dst, 1, 0, 1, 1, w, o, logWD)
					if got, want := int(dst[0]), specWeightUni(a, w, o, logWD); got != want {
						t.Fatalf("WeightUni(%d,w=%d,o=%d,logWD=%d) = %d, the specification says %d",
							a, w, o, logWD, got, want)
					}
				}
			}
		}
	}
}
