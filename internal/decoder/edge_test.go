package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/mc"
)

// predictLumaBlock is predictLumaBlockAvg without the average.
func predictLumaBlock(ref *frame.Picture, dst []byte, dstStride, dstOff, x, y, w, h, mvx, mvy int) {
	var emu [emuLumaSide * emuLumaSide]byte
	predictLumaBlockAvg(ref, &emu, dst, dstStride, dstOff, x, y, w, h, mvx, mvy, false)
}

// randomPicture is a picture of wMB x hMB macroblocks whose visible samples
// come from fill and whose border is left as zeros (nothing extended it).
func randomPicture(wMB, hMB int, fill func() byte) *frame.Picture {
	p := frame.NewPicture(wMB, hMB)
	buf := make([]byte, p.Size())
	for i := range buf {
		buf[i] = fill()
	}
	p.CopyIn(buf)
	return p
}

// padded returns a copy of p with its border extended, the way every
// reference picture used to be prepared.
func padded(p *frame.Picture) *frame.Picture {
	q := *p
	q.Y = bytes.Clone(p.Y)
	q.Cb = bytes.Clone(p.Cb)
	q.Cr = bytes.Clone(p.Cr)
	q.ExtendBorders()
	return &q
}

// TestEdgeEmulationMatchesAPaddedReference predicts blocks at every kind of
// position (inside, straddling each edge and corner, wholly outside), with
// vectors clamped exactly as predictOne clamps them, from a picture with no
// border and from a padded copy, through the old call (mc.Predict* on the padded
// planes) and the new one. They must agree sample for sample.
func TestEdgeEmulationMatchesAPaddedReference(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	fills := map[string]func() byte{
		"random": func() byte { return byte(rng.Intn(256)) },
		"binary": func() byte { return byte(255 * rng.Intn(2)) },
	}
	sizes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
	const wMB, hMB = 5, 4
	for name, fill := range fills {
		ref := randomPicture(wMB, hMB, fill)
		pad := padded(ref)
		outside, inside := 0, 0
		for n := 0; n < 20000; n++ {
			sz := sizes[rng.Intn(len(sizes))]
			w, h := sz[0], sz[1]
			x := rng.Intn(ref.Width/w) * w
			y := rng.Intn(ref.Height/h) * h
			mvx := rng.Intn(4*(ref.Width+2*frame.LumaMargin)) - 2*(ref.Width+frame.LumaMargin)
			mvy := rng.Intn(4*(ref.Height+2*frame.LumaMargin)) - 2*(ref.Height+frame.LumaMargin)
			mvx = clampComponent(mvx, lumaTapBefore-frame.LumaMargin-x, ref.Width+frame.LumaMargin-lumaTapAfter-x-w, 2)
			mvy = clampComponent(mvy, lumaTapBefore-frame.LumaMargin-y, ref.Height+frame.LumaMargin-lumaTapAfter-y-h, 2)

			want := make([]byte, 7*16*16)
			got := make([]byte, 7*16*16)
			mc.PredictLuma(want, 16, 3, pad.Y, pad.StrideY, pad.LumaOffset(x, y), w, h, mvx, mvy)
			predictLumaBlock(ref, got, 16, 3, x, y, w, h, mvx, mvy)
			if !bytes.Equal(want, got) {
				t.Fatalf("%s luma %dx%d at (%d,%d) mv (%d,%d): emulated edge differs from the padded reference", name, w, h, x, y, mvx, mvy)
			}
			if lx, ly := x+mvx>>2, y+mvy>>2; lx-2 < 0 || ly-2 < 0 || lx+w+3 > ref.Width || ly+h+3 > ref.Height {
				outside++
			} else {
				inside++
			}

			cx, cy, cw, ch := x/2, y/2, w/2, h/2
			cmvx := clampComponent(mvx, -frame.ChromaMargin-cx, ref.Width/2+frame.ChromaMargin-chromaTapAfter-cx-cw, 3)
			cmvy := clampComponent(mvy, -frame.ChromaMargin-cy, ref.Height/2+frame.ChromaMargin-chromaTapAfter-cy-ch, 3)
			for _, pl := range []struct{ ref, pad []byte }{{ref.Cb, pad.Cb}, {ref.Cr, pad.Cr}} {
				wantC := make([]byte, 7*8*8)
				gotC := make([]byte, 7*8*8)
				mc.PredictChroma(wantC, 8, 2, pl.pad, pad.StrideC, pad.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
				predictChromaBlock(ref, pl.ref, gotC, 8, 2, cx, cy, cw, ch, cmvx, cmvy)
				if !bytes.Equal(wantC, gotC) {
					t.Fatalf("%s chroma %dx%d at (%d,%d) mv (%d,%d): emulated edge differs from the padded reference", name, cw, ch, cx, cy, cmvx, cmvy)
				}
			}
			// both planes in one call, with the footprints rebuilt where needed
			var emuC [2][emuChromaSide * emuChromaSide]byte
			gotCb, gotCr := make([]byte, 7*8*8), make([]byte, 7*8*8)
			wantCb, wantCr := make([]byte, 7*8*8), make([]byte, 7*8*8)
			mc.PredictChroma(wantCb, 8, 2, pad.Cb, pad.StrideC, pad.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
			mc.PredictChroma(wantCr, 8, 2, pad.Cr, pad.StrideC, pad.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
			predictChromaPair(ref, &emuC, gotCb, gotCr, 8, 2, cx, cy, cw, ch, cmvx, cmvy)
			if !bytes.Equal(wantCb, gotCb) || !bytes.Equal(wantCr, gotCr) {
				t.Fatalf("%s chroma pair %dx%d at (%d,%d) mv (%d,%d): differs from the padded reference", name, cw, ch, cx, cy, cmvx, cmvy)
			}
		}
		if outside < 2000 || inside < 2000 {
			t.Fatalf("%s: %d blocks outside and %d inside; the test needs both", name, outside, inside)
		}
	}
}

// TestEmulateEdgeIsAClampedRead checks the block builder against a direct
// read with both coordinates clamped, for blocks wholly outside on every side,
// straddling, and wider than the picture.
func TestEmulateEdgeIsAClampedRead(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for _, pic := range [][2]int{{1, 1}, {2, 3}, {16, 16}, {40, 24}} {
		pw, ph := pic[0], pic[1]
		const margin = 3
		stride := pw + 2*margin
		plane := make([]byte, stride*(ph+2*margin))
		origin := margin*stride + margin
		for y := 0; y < ph; y++ {
			for x := 0; x < pw; x++ {
				plane[origin+y*stride+x] = byte(rng.Intn(256))
			}
		}
		for n := 0; n < 3000; n++ {
			w, h := 1+rng.Intn(21), 1+rng.Intn(21)
			x0, y0 := rng.Intn(pw+2*w+10)-w-5, rng.Intn(ph+2*h+10)-h-5
			dst := make([]byte, 24*h)
			mc.EmulateEdge(dst, 24, plane, stride, origin, pw, ph, x0, y0, w, h)
			for j := 0; j < h; j++ {
				for i := 0; i < w; i++ {
					xx, yy := min(max(x0+i, 0), pw-1), min(max(y0+j, 0), ph-1)
					if want := plane[origin+yy*stride+xx]; dst[j*24+i] != want {
						t.Fatalf("pic %dx%d block %dx%d at (%d,%d): sample (%d,%d) is %d, want %d", pw, ph, w, h, x0, y0, i, j, dst[j*24+i], want)
					}
				}
			}
		}
	}
}

// specClip is Clip1 for 8-bit samples.
func specClip(v int) int { return min(max(v, 0), 255) }

// specLuma is the luma prediction sample at full-sample position (x, y) and
// fraction (xf, yf), written from 8.4.2.2.1: get reads the reference with
// both coordinates clamped into the picture (equations 8-239, 8-240), the half
// samples are equations 8-241 to 8-249 and the quarter samples 8-250 to 8-261,
// selected by Table 8-12. j1 is taken from the horizontal b1 values (8-246).
func specLuma(get func(x, y int) int, x, y, xf, yf int) int {
	b1 := func(x, y int) int {
		return get(x-2, y) - 5*get(x-1, y) + 20*get(x, y) + 20*get(x+1, y) - 5*get(x+2, y) + get(x+3, y)
	}
	h1 := func(x, y int) int {
		return get(x, y-2) - 5*get(x, y-1) + 20*get(x, y) + 20*get(x, y+1) - 5*get(x, y+2) + get(x, y+3)
	}
	G, H, M := get(x, y), get(x+1, y), get(x, y+1)
	b := specClip((b1(x, y) + 16) >> 5)
	s := specClip((b1(x, y+1) + 16) >> 5)
	h := specClip((h1(x, y) + 16) >> 5)
	m := specClip((h1(x+1, y) + 16) >> 5)
	j1 := b1(x, y-2) - 5*b1(x, y-1) + 20*b1(x, y) + 20*b1(x, y+1) - 5*b1(x, y+2) + b1(x, y+3)
	j := specClip((j1 + 512) >> 10)
	avg := func(p, q int) int { return (p + q + 1) >> 1 }
	tbl := [4][4]int{ // [yFrac][xFrac], Table 8-12
		{G, avg(G, b), b, avg(H, b)},
		{avg(G, h), avg(b, h), avg(b, j), avg(b, m)},
		{h, avg(h, j), j, avg(j, m)},
		{avg(M, h), avg(h, s), avg(j, s), avg(m, s)},
	}
	return tbl[yf][xf]
}

// TestPredictOneFarOutsideMatchesTheClampedRead drives predictOne with vectors
// that are NOT clamped by the test, up to +-2048 quarter samples (+-512 luma
// samples, far past the 32-sample margin), on pictures from one macroblock
// wide or high up, for every block size and both lists' first reference, and
// compares luma and both chroma planes with a direct read whose coordinates are
// clamped into the picture (8-239, 8-240, 8-262 to 8-269) and the equations of
// 8.4.2.2. A picture is never narrower than a 16x16 block, so a block as wide
// as the picture is the narrowest case. It fails if predictOne's own vector
// clamp is wrong: a clamp that lets the block back into the picture changes the
// samples.
func TestPredictOneFarOutsideMatchesTheClampedRead(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	sizes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
	pics := [][2]int{{1, 1}, {1, 3}, {3, 1}, {4, 3}}
	for _, pm := range pics {
		ref := randomPicture(pm[0], pm[1], func() byte { return byte(rng.Intn(256)) })
		sd := &sliceDecoder{refList: []*frame.Picture{ref}}
		getY := func(x, y int) int {
			return int(ref.Y[ref.LumaOffset(min(max(x, 0), ref.Width-1), min(max(y, 0), ref.Height-1))])
		}
		for n := 0; n < 6000; n++ {
			sz := sizes[rng.Intn(len(sizes))]
			w, h := sz[0], sz[1]
			x := rng.Intn(ref.Width/w) * w
			y := rng.Intn(ref.Height/h) * h
			mvx, mvy := rng.Intn(4097)-2048, rng.Intn(4097)-2048
			switch n % 4 { // keep many vectors near the picture as well
			case 1:
				mvx = rng.Intn(81)*4 - 160 + rng.Intn(4)
			case 2:
				mvy = rng.Intn(81)*4 - 160 + rng.Intn(4)
			}
			seg := motionSegment{x: x, y: y, w: w, h: h, mv: [2][2]int16{{int16(mvx), int16(mvy)}}, ref: [2]int8{0, -1}}
			dy := make([]byte, 16*16)
			dcb := make([]byte, 8*8)
			dcr := make([]byte, 8*8)
			if !sd.predictOne(0, seg, x, y, dy, 16, 0, dcb, dcr, 8, 0) {
				t.Fatal("predictOne found no reference")
			}
			lx, ly := x+mvx>>2, y+mvy>>2
			for j := 0; j < h; j++ {
				for i := 0; i < w; i++ {
					if want := specLuma(getY, lx+i, ly+j, mvx&3, mvy&3); int(dy[j*16+i]) != want {
						t.Fatalf("pic %dx%d MBs luma %dx%d at (%d,%d) mv (%d,%d) sample (%d,%d): got %d, want %d",
							pm[0], pm[1], w, h, x, y, mvx, mvy, i, j, dy[j*16+i], want)
					}
				}
			}
			cx, cy, cw, ch := x/2, y/2, w/2, h/2
			px, py := cx+mvx>>3, cy+mvy>>3
			xf, yf := mvx&7, mvy&7
			for _, pl := range []struct {
				name string
				src  []byte
				dst  []byte
			}{{"Cb", ref.Cb, dcb}, {"Cr", ref.Cr, dcr}} {
				get := func(x, y int) int {
					return int(pl.src[ref.ChromaOffset(min(max(x, 0), ref.Width/2-1), min(max(y, 0), ref.Height/2-1))])
				}
				for j := 0; j < ch; j++ {
					for i := 0; i < cw; i++ {
						a, b, c, d := get(px+i, py+j), get(px+i+1, py+j), get(px+i, py+j+1), get(px+i+1, py+j+1)
						want := ((8-xf)*(8-yf)*a + xf*(8-yf)*b + (8-xf)*yf*c + xf*yf*d + 32) >> 6
						if int(pl.dst[j*8+i]) != want {
							t.Fatalf("pic %dx%d MBs %s %dx%d at (%d,%d) mv (%d,%d) sample (%d,%d): got %d, want %d",
								pm[0], pm[1], pl.name, cw, ch, cx, cy, mvx, mvy, i, j, pl.dst[j*8+i], want)
						}
					}
				}
			}
		}
	}
}

// TestBiLumaPutThenAverageMatchesTheEquations is the luma half of the fused
// bi-prediction path: list 0 replaces the samples, list 1 is averaged into
// them (8-273), with vectors up to +-2048 quarter samples from two different
// reference pictures, against the clamped-read reference.
func TestBiLumaPutThenAverageMatchesTheEquations(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	sizes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}, {4, 16}}
	r0 := randomPicture(3, 2, func() byte { return byte(rng.Intn(256)) })
	r1 := randomPicture(3, 2, func() byte { return byte(rng.Intn(256)) })
	get := func(p *frame.Picture) func(x, y int) int {
		return func(x, y int) int {
			return int(p.Y[p.LumaOffset(min(max(x, 0), p.Width-1), min(max(y, 0), p.Height-1))])
		}
	}
	g0, g1 := get(r0), get(r1)
	sd := &sliceDecoder{refList: []*frame.Picture{r0}, refListL1: []*frame.Picture{r1}}
	for n := 0; n < 8000; n++ {
		sz := sizes[rng.Intn(len(sizes))]
		w, h := sz[0], sz[1]
		x := rng.Intn(r0.Width/w) * w
		y := rng.Intn(r0.Height/h) * h
		var mv [2][2]int
		for l := 0; l < 2; l++ {
			mv[l] = [2]int{rng.Intn(4097) - 2048, rng.Intn(4097) - 2048}
			if n%3 == l {
				mv[l] = [2]int{rng.Intn(65)*4 - 128 + rng.Intn(4), rng.Intn(65)*4 - 128 + rng.Intn(4)}
			}
		}
		seg := motionSegment{x: x, y: y, w: w, h: h, ref: [2]int8{0, 0}}
		for l := 0; l < 2; l++ {
			seg.mv[l] = [2]int16{int16(mv[l][0]), int16(mv[l][1])}
		}
		dst := make([]byte, 16*16)
		sd.predictLumaSeg(r0, seg, 0, x, y, dst, 16, 0, false)
		sd.predictLumaSeg(r1, seg, 1, x, y, dst, 16, 0, true)
		for j := 0; j < h; j++ {
			for i := 0; i < w; i++ {
				a := specLuma(g0, x+mv[0][0]>>2+i, y+mv[0][1]>>2+j, mv[0][0]&3, mv[0][1]&3)
				b := specLuma(g1, x+mv[1][0]>>2+i, y+mv[1][1]>>2+j, mv[1][0]&3, mv[1][1]&3)
				if want := (a + b + 1) >> 1; int(dst[j*16+i]) != want {
					t.Fatalf("%dx%d at (%d,%d) mv %v sample (%d,%d): got %d, want %d", w, h, x, y, mv, i, j, dst[j*16+i], want)
				}
			}
		}
	}
}
