package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/mc"
)

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
