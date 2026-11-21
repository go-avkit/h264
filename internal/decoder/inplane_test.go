package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/mc"
)

// TestInPlaneRouteEqualsThePlainRoute predicts blocks into a picture through the
// proved planes and through the plain mc.Predict* functions on a copy of it, for
// every block size, fraction and both the put and the average. The whole planes
// must agree, so a sample written outside the block fails too. A refusal (false)
// must leave the destination untouched.
func TestInPlaneRouteEqualsThePlainRoute(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	fill := func() byte { return byte(rng.Intn(256)) }
	ref := randomPicture(5, 4, fill)
	sizes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
	lumaDone, chromaDone := 0, 0
	for n := 0; n < 4000; n++ {
		pic := randomPicture(5, 4, fill)
		origY := bytes.Clone(pic.Y)
		wantY, wantCb, wantCr := bytes.Clone(pic.Y), bytes.Clone(pic.Cb), bytes.Clone(pic.Cr)
		sz := sizes[rng.Intn(len(sizes))]
		w, h := sz[0], sz[1]
		x := rng.Intn(ref.Width/w) * w
		y := rng.Intn(ref.Height/h) * h
		mvx := rng.Intn(4*(ref.Width+8)) - 2*(ref.Width+8)
		mvy := rng.Intn(4*(ref.Height+8)) - 2*(ref.Height+8)
		avg := rng.Intn(2) == 0

		lx, ly := x+mvx>>2, y+mvy>>2
		if lumaInside(ref, lx, ly, w, h) {
			if avg {
				mc.PredictLumaAvg(wantY, pic.StrideY, pic.LumaOffset(x, y), ref.Y, ref.StrideY, ref.LumaOffset(x, y), w, h, mvx, mvy)
			} else {
				mc.PredictLuma(wantY, pic.StrideY, pic.LumaOffset(x, y), ref.Y, ref.StrideY, ref.LumaOffset(x, y), w, h, mvx, mvy)
			}
			if predictLumaInPlace(pic, ref, x, y, w, h, mvx, mvy, avg) {
				lumaDone++
				if !bytes.Equal(pic.Y, wantY) {
					t.Fatalf("luma %dx%d at (%d,%d) mv (%d,%d) avg=%v: in-place route differs from the plain route", w, h, x, y, mvx, mvy, avg)
				}
			} else if !bytes.Equal(pic.Y, origY) {
				t.Fatalf("luma %dx%d at (%d,%d) mv (%d,%d): a refused block changed the destination", w, h, x, y, mvx, mvy)
			}
		} else if predictLumaInPlace(pic, ref, x, y, w, h, mvx, mvy, avg) {
			t.Fatalf("luma %dx%d at (%d,%d) mv (%d,%d): predicted in place with the footprint outside the picture", w, h, x, y, mvx, mvy)
		}

		cx, cy, cw, ch := x/2, y/2, w/2, h/2
		cmvx, cmvy := mvx, mvy
		if chromaInside(ref, cx+cmvx>>3, cy+cmvy>>3, cw, ch) {
			if avg {
				mc.PredictChromaPairAvg(wantCb, wantCr, pic.StrideC, pic.ChromaOffset(cx, cy), ref.Cb, ref.Cr, ref.StrideC, ref.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
			} else {
				mc.PredictChroma(wantCb, pic.StrideC, pic.ChromaOffset(cx, cy), ref.Cb, ref.StrideC, ref.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
				mc.PredictChroma(wantCr, pic.StrideC, pic.ChromaOffset(cx, cy), ref.Cr, ref.StrideC, ref.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
			}
			if predictChromaInPlace(pic, ref, cx, cy, cw, ch, cmvx, cmvy, avg) {
				chromaDone++
				if !bytes.Equal(pic.Cb, wantCb) || !bytes.Equal(pic.Cr, wantCr) {
					t.Fatalf("chroma %dx%d at (%d,%d) mv (%d,%d) avg=%v: in-place route differs from the plain route", cw, ch, cx, cy, cmvx, cmvy, avg)
				}
			}
		} else if predictChromaInPlace(pic, ref, cx, cy, cw, ch, cmvx, cmvy, avg) {
			t.Fatalf("chroma %dx%d at (%d,%d) mv (%d,%d): predicted in place with the footprint outside the picture", cw, ch, cx, cy, cmvx, cmvy)
		}
	}
	if mc.FusedAverage && lumaDone < 200 {
		t.Fatalf("only %d luma blocks went through the planes; the test needs inside footprints", lumaDone)
	}
	t.Logf("%d luma and %d chroma blocks through the planes", lumaDone, chromaDone)
}

// TestInsideBlocksAreReadInPlace: a block whose footprint lies inside the
// reference never touches the room for the rebuilt footprint, and one that
// straddles the edge does. The two give the same samples, so only the room tells
// them apart.
func TestInsideBlocksAreReadInPlace(t *testing.T) {
	rng := rand.New(rand.NewSource(13))
	ref := randomPicture(3, 3, func() byte { return byte(rng.Intn(256)) })
	for _, c := range []struct {
		name     string
		x, y     int
		touching bool
	}{{"inside", 16, 16, false}, {"left edge", 0, 16, true}, {"top edge", 16, 0, true}} {
		var emu [emuLumaSide * emuLumaSide]byte
		for i := range emu {
			emu[i] = 0xAA
		}
		dst := make([]byte, 16*16)
		predictLumaBlockAvg(ref, &emu, dst, 16, 0, c.x, c.y, 8, 8, 0, 0, false)
		touched := false
		for _, b := range emu {
			touched = touched || b != 0xAA
		}
		if touched != c.touching {
			t.Errorf("%s: footprint rebuilt = %v, want %v", c.name, touched, c.touching)
		}
	}
}

// TestInPlaneRouteNeedsBothPicturesProved: a picture built without NewPicture, or
// whose proof was cleared, is never written or read through the planes.
func TestInPlaneRouteNeedsBothPicturesProved(t *testing.T) {
	rng := rand.New(rand.NewSource(12))
	fill := func() byte { return byte(rng.Intn(256)) }
	for _, c := range []struct {
		name             string
		clearPic, clearR bool
	}{{"destination unproved", true, false}, {"reference unproved", false, true}} {
		ref := randomPicture(4, 4, fill)
		pic := randomPicture(4, 4, fill)
		if !ref.MCProved || !pic.MCProved {
			t.Fatal("NewPicture left a picture unproved")
		}
		ref.MCProved = !c.clearR
		pic.MCProved = !c.clearPic
		before := bytes.Clone(pic.Y)
		beforeC := bytes.Clone(pic.Cb)
		if predictLumaInPlace(pic, ref, 16, 16, 16, 16, 4, 4, false) || predictLumaInPlace(pic, ref, 16, 16, 16, 16, 4, 4, true) {
			t.Fatalf("%s: luma predicted through the planes", c.name)
		}
		if predictChromaInPlace(pic, ref, 8, 8, 8, 8, 4, 4, false) || predictChromaInPlace(pic, ref, 8, 8, 8, 8, 4, 4, true) {
			t.Fatalf("%s: chroma predicted through the planes", c.name)
		}
		if !bytes.Equal(before, pic.Y) || !bytes.Equal(beforeC, pic.Cb) {
			t.Fatalf("%s: a refused block changed the destination", c.name)
		}
	}
}
