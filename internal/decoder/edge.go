package decoder

import (
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/mc"
)

const (
	// A luma block of at most 16x16 reads 2 samples before and 3 after the
	// block in each direction (the six-tap filter of 8.4.2.2.1), so 21x21.
	emuLumaSide = 16 + lumaTapBefore + lumaTapAfter
	// A chroma block of at most 8x8 reads one more column and row (8.4.2.2.2).
	emuChromaSide = 8 + chromaTapAfter
)

// predictLumaBlockAvg predicts a w x h luma block at (x, y) displaced by the
// quarter-sample vector (mvx, mvy) from ref. While the footprint of the filter
// lies inside the picture the reference is read in place; otherwise the
// footprint is rebuilt with the coordinates clamped into the picture (8-239,
// 8-240) and predicted from that copy, so the reference needs no padded border.
//
// emu is the room for the rebuilt footprint. It belongs to the caller because
// the kernels keep their slices alive for escape analysis, which would move a
// local array to the heap on every call.
//
// With avg the prediction is averaged into the samples already in dst,
// (dst + p + 1) >> 1 (8-273), the second prediction of a bi-predicted block,
// instead of replacing them.
func predictLumaBlockAvg(ref *frame.Picture, emu *[emuLumaSide * emuLumaSide]byte, dst []byte, dstStride, dstOff, x, y, w, h, mvx, mvy int, avg bool) {
	lx, ly := x+mvx>>2, y+mvy>>2
	if lx-lumaTapBefore >= 0 && lx+w+lumaTapAfter <= ref.Width &&
		ly-lumaTapBefore >= 0 && ly+h+lumaTapAfter <= ref.Height {
		if avg {
			mc.PredictLumaAvg(dst, dstStride, dstOff, ref.Y, ref.StrideY, ref.LumaOffset(x, y), w, h, mvx, mvy)
		} else {
			mc.PredictLuma(dst, dstStride, dstOff, ref.Y, ref.StrideY, ref.LumaOffset(x, y), w, h, mvx, mvy)
		}
		return
	}
	mc.EmulateEdge(emu[:], emuLumaSide, ref.Y, ref.StrideY, ref.OriginY(), ref.Width, ref.Height,
		lx-lumaTapBefore, ly-lumaTapBefore, w+lumaTapBefore+lumaTapAfter, h+lumaTapBefore+lumaTapAfter)
	if avg {
		mc.PredictLumaAvg(dst, dstStride, dstOff, emu[:], emuLumaSide, lumaTapBefore*emuLumaSide+lumaTapBefore,
			w, h, mvx&3, mvy&3)
		return
	}
	mc.PredictLuma(dst, dstStride, dstOff, emu[:], emuLumaSide, lumaTapBefore*emuLumaSide+lumaTapBefore,
		w, h, mvx&3, mvy&3)
}

// predictChromaBlock is predictLumaBlockAvg for one chroma plane of ref (src, with
// the picture's chroma stride and origin), at chroma position (cx, cy) and the
// eighth-sample vector (cmvx, cmvy).
// predictChromaPair is predictChromaBlock for both planes at once (they share
// every parameter), with room for the two rebuilt footprints owned by the caller.
func predictChromaPair(ref *frame.Picture, emu *[2][emuChromaSide * emuChromaSide]byte, dstCb, dstCr []byte, dstStride, dstOff, cx, cy, cw, ch, cmvx, cmvy int) {
	px, py := cx+cmvx>>3, cy+cmvy>>3
	cwPic, chPic := ref.Width/2, ref.Height/2
	if px >= 0 && px+cw+chromaTapAfter <= cwPic && py >= 0 && py+ch+chromaTapAfter <= chPic {
		mc.PredictChromaPair(dstCb, dstCr, dstStride, dstOff, ref.Cb, ref.Cr, ref.StrideC, ref.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
		return
	}
	mc.EmulateEdge(emu[0][:], emuChromaSide, ref.Cb, ref.StrideC, ref.OriginC(), cwPic, chPic,
		px, py, cw+chromaTapAfter, ch+chromaTapAfter)
	mc.EmulateEdge(emu[1][:], emuChromaSide, ref.Cr, ref.StrideC, ref.OriginC(), cwPic, chPic,
		px, py, cw+chromaTapAfter, ch+chromaTapAfter)
	mc.PredictChromaPair(dstCb, dstCr, dstStride, dstOff, emu[0][:], emu[1][:], emuChromaSide, 0, cw, ch, cmvx&7, cmvy&7)
}

func predictChromaBlock(ref *frame.Picture, src []byte, dst []byte, dstStride, dstOff, cx, cy, cw, ch, cmvx, cmvy int) {
	px, py := cx+cmvx>>3, cy+cmvy>>3
	cwPic, chPic := ref.Width/2, ref.Height/2
	if px >= 0 && px+cw+chromaTapAfter <= cwPic && py >= 0 && py+ch+chromaTapAfter <= chPic {
		mc.PredictChroma(dst, dstStride, dstOff, src, ref.StrideC, ref.ChromaOffset(cx, cy), cw, ch, cmvx, cmvy)
		return
	}
	var emu [emuChromaSide * emuChromaSide]byte
	mc.EmulateEdge(emu[:], emuChromaSide, src, ref.StrideC, ref.OriginC(), cwPic, chPic,
		px, py, cw+chromaTapAfter, ch+chromaTapAfter)
	mc.PredictChroma(dst, dstStride, dstOff, emu[:], emuChromaSide, 0, cw, ch, cmvx&7, cmvy&7)
}
