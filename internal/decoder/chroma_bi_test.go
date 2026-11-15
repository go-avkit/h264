package decoder

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/mc"
)

// The bi-prediction of the chroma of a segment as motionCompensate does it
// (list 0 straight into the picture, list 1 averaged on top of it) equals the
// path it replaced (each list into its own block, then the rounding average of
// 8-273 into the picture), for every segment shape a macroblock gives,
// including the merged ones (8x2 and 2x8 chroma, 6 wide or high), fractions of
// zero, and vectors that reach beyond the picture.
func TestChromaBiPathEqualsTwoBuffers(t *testing.T) {
	rng := rand.New(rand.NewSource(91))
	const wMB, hMB = 5, 4
	r0 := randomPicture(wMB, hMB, func() byte { return byte(rng.Intn(256)) })
	r1 := randomPicture(wMB, hMB, func() byte { return byte(rng.Intn(256)) })
	shapes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}, {16, 4}, {4, 16}, {12, 16}, {8, 12}, {16, 12}}
	var emu [2][emuChromaSide * emuChromaSide]byte
	zeroFrac := 0
	for n := 0; n < 4000; n++ {
		sz := shapes[rng.Intn(len(shapes))]
		w, h := sz[0], sz[1]
		x := rng.Intn(r0.Width/4-w/4+1) * 4
		y := rng.Intn(r0.Height/4-h/4+1) * 4
		seg := motionSegment{x: 0, y: 0, w: w, h: h}
		for l := 0; l < 2; l++ {
			for c := 0; c < 2; c++ {
				v := rng.Intn(4*(r0.Width+40)) - 2*(r0.Width+40)
				if rng.Intn(4) == 0 {
					v = 8 * (rng.Intn(9) - 4) // a whole chroma sample: both fractions zero
				}
				seg.mv[l][c] = int16(v)
			}
		}
		if seg.mv[0][0]&7 == 0 && seg.mv[0][1]&7 == 0 {
			zeroFrac++
		}
		cw, ch := w/2, h/2
		pic := frame.NewPicture(wMB, hMB)
		for i := range pic.Cb {
			pic.Cb[i], pic.Cr[i] = byte(rng.Intn(256)), byte(rng.Intn(256))
		}
		old := *pic
		old.Cb, old.Cr = append([]byte(nil), pic.Cb...), append([]byte(nil), pic.Cr...)
		off := pic.ChromaOffset(x/2, y/2)

		// the new path
		predictChromaSeg(r0, &emu, seg, 0, x, y, pic.Cb, pic.Cr, pic.StrideC, off, false)
		predictChromaSeg(r1, &emu, seg, 1, x, y, pic.Cb, pic.Cr, pic.StrideC, off, true)

		// the two-buffer path
		var a, b predBuffer
		predictChromaSeg(r0, &emu, seg, 0, x, y, a.cb[:], a.cr[:], 8, 0, false)
		predictChromaSeg(r1, &emu, seg, 1, x, y, b.cb[:], b.cr[:], 8, 0, false)
		mc.Average(old.Cb, old.StrideC, off, a.cb[:], 8, 0, b.cb[:], 8, 0, cw, ch)
		mc.Average(old.Cr, old.StrideC, off, a.cr[:], 8, 0, b.cr[:], 8, 0, cw, ch)

		if !bytes.Equal(pic.Cb, old.Cb) || !bytes.Equal(pic.Cr, old.Cr) {
			t.Fatalf("segment %dx%d at (%d,%d), mv %v: the fused chroma average differs from the two-buffer path", w, h, x, y, seg.mv)
		}
	}
	if zeroFrac < 100 {
		t.Fatalf("only %d segments had a whole-sample vector: the zero-fraction case is not covered", zeroFrac)
	}
}
