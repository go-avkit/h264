package decoder

import (
	"fmt"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/mc"
)

type subMbInfo struct {
	numParts int
	w        int
	h        int
}

var subMbTable = [4]subMbInfo{
	{1, 8, 8},
	{2, 8, 4},
	{2, 4, 8},
	{4, 4, 4},
}

type mbPart struct {
	x, y, w, h int
}

func partitionsOf(kind int) []mbPart {
	switch kind {
	case mbTypeP16x16:
		return []mbPart{{0, 0, 16, 16}}
	case mbTypeP16x8:
		return []mbPart{{0, 0, 16, 8}, {0, 8, 16, 8}}
	case mbTypeP8x16:
		return []mbPart{{0, 0, 8, 16}, {8, 0, 8, 16}}
	}
	return nil
}

func (d *sliceDecoder) readRefIdx(maxMinus1 uint32) (int8, error) {
	if maxMinus1 == 0 {
		return 0, nil
	}
	v, err := d.r.ReadTE(maxMinus1)
	if err != nil {
		return 0, err
	}
	if v > maxMinus1 {
		return 0, fmt.Errorf("%w: ref_idx_l0 %d", ErrCorrupt, v)
	}
	return int8(v), nil
}

func (d *sliceDecoder) readMVD() ([2]int16, error) {
	x, err := d.r.ReadSE()
	if err != nil {
		return [2]int16{}, err
	}
	y, err := d.r.ReadSE()
	if err != nil {
		return [2]int16{}, err
	}
	if x < -32768 || x > 32767 || y < -32768 || y > 32767 {
		return [2]int16{}, fmt.Errorf("%w: motion vector difference out of range", ErrCorrupt)
	}
	return [2]int16{int16(x), int16(y)}, nil
}

func (d *sliceDecoder) decodeInterMB(info mbTypeInfo, res *mbResidual) error {
	d.cur.kind = info.kind
	d.cur.Intra = false
	maxRef := uint32(d.numRefIdxActive - 1)

	if info.kind == mbTypeP8x8 || info.kind == mbTypeP8x8Ref0 {
		if err := d.decodeP8x8(info, maxRef); err != nil {
			return err
		}
	} else {
		parts := partitionsOf(info.kind)
		refs := make([]int8, len(parts))
		for i := range parts {
			r, err := d.readRefIdx(maxRef)
			if err != nil {
				return err
			}
			refs[i] = r
		}
		for i, p := range parts {
			mvd, err := d.readMVD()
			if err != nil {
				return err
			}
			mvp := d.predictMV(0, p.x, p.y, p.w, p.h, refs[i], i, info.kind)
			mv := [2]int16{mvp[0] + mvd[0], mvp[1] + mvd[1]}
			d.storeMotion(0, p.x, p.y, p.w, p.h, mv, refs[i])
		}
	}

	if err := d.parseCBP(false); err != nil {
		return err
	}
	if err := d.readTransformSize8x8(); err != nil {
		return err
	}
	if d.cur.cbpLuma != 0 || d.cur.cbpChroma != 0 {
		if err := d.parseQPDelta(); err != nil {
			return err
		}
		if err := d.readResidual(res, false); err != nil {
			return err
		}
	}
	d.cur.QPY = d.qpY
	d.motionCompensate()
	d.addInterResidual(res)
	return nil
}

func (d *sliceDecoder) decodeP8x8(info mbTypeInfo, maxRef uint32) error {
	var sub [4]subMbInfo
	for i := 0; i < 4; i++ {
		v, err := d.r.ReadUE()
		if err != nil {
			return err
		}
		if v > 3 {
			return fmt.Errorf("%w: sub_mb_type %d", ErrCorrupt, v)
		}
		sub[i] = subMbTable[v]
		if sub[i].numParts != 1 {
			d.subPartsAtLeast8x8 = false
		}
	}
	var refs [4]int8
	for i := 0; i < 4; i++ {
		if info.kind == mbTypeP8x8Ref0 {
			refs[i] = 0
			continue
		}
		r, err := d.readRefIdx(maxRef)
		if err != nil {
			return err
		}
		refs[i] = r
	}
	for i := 0; i < 4; i++ {
		ox := i % 2 * 8
		oy := i / 2 * 8
		s := sub[i]
		cols := 8 / s.w
		for p := 0; p < s.numParts; p++ {
			px := ox + p%cols*s.w
			py := oy + p/cols*s.h
			mvd, err := d.readMVD()
			if err != nil {
				return err
			}
			mvp := d.predictMV(0, px, py, s.w, s.h, refs[i], p, mbTypeP8x8)
			mv := [2]int16{mvp[0] + mvd[0], mvp[1] + mvd[1]}
			d.storeMotion(0, px, py, s.w, s.h, mv, refs[i])
		}
	}
	return nil
}

func (d *sliceDecoder) decodePSkip() error {
	d.cur.kind = mbTypePSkip
	d.cur.Intra = false
	d.cur.cbpLuma = 0
	d.cur.cbpChroma = 0
	d.cur.QPY = d.qpY
	mv := d.skipMV()
	d.storeMotion(0, 0, 0, 16, 16, mv, 0)
	d.motionCompensate()
	return nil
}

func (d *sliceDecoder) listFor(list int) []*frame.Picture {
	if list == 0 {
		return d.refList
	}
	return d.refListL1
}

func (d *sliceDecoder) refPictureIn(list int, idx int8) *frame.Picture {
	l := d.listFor(list)
	if len(l) == 0 {
		return nil
	}
	if int(idx) >= len(l) || idx < 0 {
		return l[0]
	}
	return l[idx]
}

func clampComponent(v, lo, hi, fracBits int) int {
	frac := v & (1<<uint(fracBits) - 1)
	iv := v >> uint(fracBits)
	if iv < lo {
		iv = lo
	}
	if iv > hi {
		iv = hi
	}
	return iv<<uint(fracBits) | frac
}

const (
	lumaTapBefore  = 2
	lumaTapAfter   = 3
	chromaTapAfter = 1
)

const (
	weightNone = iota
	weightExplicit
	weightImplicit
)

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (d *sliceDecoder) implicitWeights(ref0, ref1 int8) (int, int) {
	p0 := d.refPictureIn(0, ref0)
	p1 := d.refPictureIn(1, ref1)
	if p0 == nil || p1 == nil || p0.LongTerm || p1.LongTerm {
		return 32, 32
	}
	td := clip3(-128, 127, p1.POC-p0.POC)
	if td == 0 {
		return 32, 32
	}
	tb := clip3(-128, 127, d.pic.POC-p0.POC)
	abs := td / 2
	if abs < 0 {
		abs = -abs
	}
	tx := (16384 + abs) / td
	dist := clip3(-1024, 1023, (tb*tx+32)>>6)
	w1 := dist >> 2
	if w1 < -64 || w1 > 128 {
		return 32, 32
	}
	return 64 - w1, w1
}

type predBuffer struct {
	y  [256]byte
	cb [64]byte
	cr [64]byte
}

func (d *sliceDecoder) predictOne(list int, seg motionSegment, x, y int,
	dstY []byte, strideY, offY int, dstCb, dstCr []byte, strideC, offC int) bool {

	ref := d.refPictureIn(list, seg.ref[list])
	if ref == nil {
		return false
	}
	d.predictLumaSeg(ref, seg, list, x, y, dstY, strideY, offY, false)
	predictChromaSeg(ref, &d.emuChroma, seg, list, x, y, dstCb, dstCr, strideC, offC)
	return true
}

// predictLumaSeg predicts the luma of seg from ref, list's vector clamped to
// the reference's border (the margin that used to be padded), into dst, or
// averaged into dst when avg is set.
func (d *sliceDecoder) predictLumaSeg(ref *frame.Picture, seg motionSegment, list, x, y int, dst []byte, stride, off int, avg bool) {
	mv := seg.mv[list]
	mvx := clampComponent(int(mv[0]),
		lumaTapBefore-frame.LumaMargin-x,
		ref.Width+frame.LumaMargin-lumaTapAfter-x-seg.w, 2)
	mvy := clampComponent(int(mv[1]),
		lumaTapBefore-frame.LumaMargin-y,
		ref.Height+frame.LumaMargin-lumaTapAfter-y-seg.h, 2)
	predictLumaBlockAvg(ref, &d.emuLuma, dst, stride, off, x, y, seg.w, seg.h, mvx, mvy, avg)
}

func predictChromaSeg(ref *frame.Picture, emu *[2][emuChromaSide * emuChromaSide]byte, seg motionSegment, list, x, y int, dstCb, dstCr []byte, strideC, offC int) {
	mv := seg.mv[list]
	cx, cy := x/2, y/2
	cw, ch := seg.w/2, seg.h/2
	cmvx := clampComponent(int(mv[0]),
		-frame.ChromaMargin-cx,
		ref.Width/2+frame.ChromaMargin-chromaTapAfter-cx-cw, 3)
	cmvy := clampComponent(int(mv[1]),
		-frame.ChromaMargin-cy,
		ref.Height/2+frame.ChromaMargin-chromaTapAfter-cy-ch, 3)
	predictChromaPair(ref, emu, dstCb, dstCr, strideC, offC, cx, cy, cw, ch, cmvx, cmvy)
}

func (d *sliceDecoder) weightEntry(list int, ref int8) *weightEntry {
	if d.weights == nil {
		return nil
	}
	l := d.weights.L0
	if list == 1 {
		l = d.weights.L1
	}
	if int(ref) < 0 || int(ref) >= len(l) {
		return nil
	}
	return &l[ref]
}

func (d *sliceDecoder) weightUni(list int, seg motionSegment, x, y int) {
	if d.weightMode != weightExplicit {
		return
	}
	e := d.weightEntry(list, seg.ref[list])
	if e == nil {
		return
	}
	cx, cy := x/2, y/2
	cw, ch := seg.w/2, seg.h/2
	mc.WeightUni(d.pic.Y, d.pic.StrideY, d.pic.LumaOffset(x, y), seg.w, seg.h,
		int(e.LumaWeight), int(e.LumaOffset), int(d.weights.LumaLog2WeightDenom))
	logWDC := int(d.weights.ChromaLog2WeightDenom)
	mc.WeightUni(d.pic.Cb, d.pic.StrideC, d.pic.ChromaOffset(cx, cy), cw, ch,
		int(e.ChromaWeight[0]), int(e.ChromaOffset[0]), logWDC)
	mc.WeightUni(d.pic.Cr, d.pic.StrideC, d.pic.ChromaOffset(cx, cy), cw, ch,
		int(e.ChromaWeight[1]), int(e.ChromaOffset[1]), logWDC)
}

// biIsPlainAverage reports whether combineBi would reduce the two predictions
// of seg to the default average of 8.4.2.3.1: no weighting at all, or implicit
// weights that came out (32, 32) (8-280, 8-281).
func (d *sliceDecoder) biIsPlainAverage(seg motionSegment) bool {
	switch d.weightMode {
	case weightImplicit:
		w0, w1 := d.implicitWeights(seg.ref[0], seg.ref[1])
		return w0 == 32 && w1 == 32
	case weightExplicit:
		return false
	}
	return true
}

func (d *sliceDecoder) combineBi(seg motionSegment, x, y int) {
	cx, cy := x/2, y/2
	cw, ch := seg.w/2, seg.h/2
	a, b := &d.predA, &d.predB
	lumaOff := d.pic.LumaOffset(x, y)
	chromaOff := d.pic.ChromaOffset(cx, cy)

	switch d.weightMode {
	case weightExplicit:
		e0 := d.weightEntry(0, seg.ref[0])
		e1 := d.weightEntry(1, seg.ref[1])
		if e0 == nil || e1 == nil {
			break
		}
		mc.WeightBi(d.pic.Y, d.pic.StrideY, lumaOff, a.y[:], 16, 0, b.y[:], 16, 0,
			seg.w, seg.h, int(e0.LumaWeight), int(e1.LumaWeight),
			int(e0.LumaOffset), int(e1.LumaOffset), int(d.weights.LumaLog2WeightDenom))
		logWDC := int(d.weights.ChromaLog2WeightDenom)
		mc.WeightBi(d.pic.Cb, d.pic.StrideC, chromaOff, a.cb[:], 8, 0, b.cb[:], 8, 0,
			cw, ch, int(e0.ChromaWeight[0]), int(e1.ChromaWeight[0]),
			int(e0.ChromaOffset[0]), int(e1.ChromaOffset[0]), logWDC)
		mc.WeightBi(d.pic.Cr, d.pic.StrideC, chromaOff, a.cr[:], 8, 0, b.cr[:], 8, 0,
			cw, ch, int(e0.ChromaWeight[1]), int(e1.ChromaWeight[1]),
			int(e0.ChromaOffset[1]), int(e1.ChromaOffset[1]), logWDC)
		return

	case weightImplicit:
		w0, w1 := d.implicitWeights(seg.ref[0], seg.ref[1])
		mc.WeightBi(d.pic.Y, d.pic.StrideY, lumaOff, a.y[:], 16, 0, b.y[:], 16, 0,
			seg.w, seg.h, w0, w1, 0, 0, 5)
		mc.WeightBi(d.pic.Cb, d.pic.StrideC, chromaOff, a.cb[:], 8, 0, b.cb[:], 8, 0,
			cw, ch, w0, w1, 0, 0, 5)
		mc.WeightBi(d.pic.Cr, d.pic.StrideC, chromaOff, a.cr[:], 8, 0, b.cr[:], 8, 0,
			cw, ch, w0, w1, 0, 0, 5)
		return
	}

	mc.Average(d.pic.Y, d.pic.StrideY, lumaOff, a.y[:], 16, 0, b.y[:], 16, 0, seg.w, seg.h)
	mc.Average(d.pic.Cb, d.pic.StrideC, chromaOff, a.cb[:], 8, 0, b.cb[:], 8, 0, cw, ch)
	mc.Average(d.pic.Cr, d.pic.StrideC, chromaOff, a.cr[:], 8, 0, b.cr[:], 8, 0, cw, ch)
}

func (d *sliceDecoder) motionCompensate() {
	baseX, baseY := d.mbx*16, d.mby*16
	d.segments = motionSegments(d.cur, d.segments)
	for _, seg := range d.segments {
		x, y := baseX+seg.x, baseY+seg.y
		use0 := seg.ref[0] >= 0
		use1 := seg.ref[1] >= 0
		switch {
		case use0 && use1 && mc.FusedAverage && d.biIsPlainAverage(seg):
			// Luma: list 0 goes straight into the picture and list 1 is
			// averaged into it, no block in between. Chroma keeps the two
			// buffers.
			r0, r1 := d.refPictureIn(0, seg.ref[0]), d.refPictureIn(1, seg.ref[1])
			if r0 == nil || r1 == nil {
				break
			}
			lumaOff := d.pic.LumaOffset(x, y)
			d.predictLumaSeg(r0, seg, 0, x, y, d.pic.Y, d.pic.StrideY, lumaOff, false)
			d.predictLumaSeg(r1, seg, 1, x, y, d.pic.Y, d.pic.StrideY, lumaOff, true)
			predictChromaSeg(r0, &d.emuChroma, seg, 0, x, y, d.predA.cb[:], d.predA.cr[:], 8, 0)
			predictChromaSeg(r1, &d.emuChroma, seg, 1, x, y, d.predB.cb[:], d.predB.cr[:], 8, 0)
			cx, cy, cw, ch := x/2, y/2, seg.w/2, seg.h/2
			chromaOff := d.pic.ChromaOffset(cx, cy)
			mc.Average(d.pic.Cb, d.pic.StrideC, chromaOff, d.predA.cb[:], 8, 0, d.predB.cb[:], 8, 0, cw, ch)
			mc.Average(d.pic.Cr, d.pic.StrideC, chromaOff, d.predA.cr[:], 8, 0, d.predB.cr[:], 8, 0, cw, ch)
		case use0 && use1:
			okA := d.predictOne(0, seg, x, y, d.predA.y[:], 16, 0, d.predA.cb[:], d.predA.cr[:], 8, 0)
			okB := d.predictOne(1, seg, x, y, d.predB.y[:], 16, 0, d.predB.cb[:], d.predB.cr[:], 8, 0)
			if okA && okB {
				d.combineBi(seg, x, y)
			}
		case use0, use1:
			list := 0
			if use1 {
				list = 1
			}
			if d.predictOne(list, seg, x, y, d.pic.Y, d.pic.StrideY, d.pic.LumaOffset(x, y),
				d.pic.Cb, d.pic.Cr, d.pic.StrideC, d.pic.ChromaOffset(x/2, y/2)) {
				d.weightUni(list, seg, x, y)
			}
		}
	}
}

type motionSegment struct {
	x, y, w, h int
	mv         [2][2]int16
	ref        [2]int8
}

func sameMotion(m *mbState, a, b int) bool {
	return m.MvL0[a] == m.MvL0[b] && m.refIdxL0[a] == m.refIdxL0[b] &&
		m.MvL1[a] == m.MvL1[b] && m.refIdxL1[a] == m.refIdxL1[b]
}

// motionSegments merges the 4x4 blocks of a macroblock that share their motion
// into rectangles. A macroblock whose sixteen blocks share one motion (P_Skip,
// the 16x16 partitions, most of the macroblocks of a picture) is one segment: it
// is tested first, with no used-block bookkeeping.
func motionSegments(m *mbState, dst []motionSegment) []motionSegment {
	uniform := true
	for b := 1; b < 16; b++ {
		if !sameMotion(m, 0, b) {
			uniform = false
			break
		}
	}
	if uniform {
		return append(dst[:0], motionSegment{
			w: 16, h: 16,
			mv:  [2][2]int16{m.MvL0[0], m.MvL1[0]},
			ref: [2]int8{m.refIdxL0[0], m.refIdxL1[0]},
		})
	}
	return motionSegmentsMerged(m, dst)
}

func motionSegmentsMerged(m *mbState, dst []motionSegment) []motionSegment {
	segs := dst[:0]
	var used [16]bool
	for by := 0; by < 16; by += 4 {
		for bx := 0; bx < 16; bx += 4 {
			z := zscanOf[by>>2][bx>>2]
			if used[z] {
				continue
			}
			w := 4
			for bx+w < 16 {
				nz := zscanOf[by>>2][(bx+w)>>2]
				if used[nz] || !sameMotion(m, z, nz) {
					break
				}
				w += 4
			}
			h := 4
			for by+h < 16 {
				same := true
				for i := 0; i < w; i += 4 {
					nz := zscanOf[(by+h)>>2][(bx+i)>>2]
					if used[nz] || !sameMotion(m, z, nz) {
						same = false
						break
					}
				}
				if !same {
					break
				}
				h += 4
			}
			for yy := by; yy < by+h; yy += 4 {
				for xx := bx; xx < bx+w; xx += 4 {
					used[zscanOf[yy>>2][xx>>2]] = true
				}
			}
			segs = append(segs, motionSegment{
				x: bx, y: by, w: w, h: h,
				mv:  [2][2]int16{m.MvL0[z], m.MvL1[z]},
				ref: [2]int8{m.refIdxL0[z], m.refIdxL1[z]},
			})
		}
	}
	return segs
}

func (d *sliceDecoder) readTransformSize8x8() error {
	if !d.mayUse8x8Transform() {
		return nil
	}
	flag, err := d.r.ReadFlag()
	if err != nil {
		return err
	}
	d.cur.Transform8x8 = flag
	return nil
}

func (d *sliceDecoder) addInterResidual(res *mbResidual) {
	baseX, baseY := d.mbx*16, d.mby*16
	if d.cur.Transform8x8 {
		for i8 := 0; i8 < 4; i8++ {
			if d.cur.cbpLuma&(1<<uint(i8)) == 0 {
				continue
			}
			d.addLuma8x8(res, i8, d.pic.LumaOffset(baseX+i8%2*8, baseY+i8/2*8), false)
		}
		d.addChromaResidual(res)
		return
	}
	var mask uint16
	for blk := 0; blk < 16; blk++ {
		if d.cur.NzY[blk] != 0 {
			mask |= 1 << uint(blk)
		}
	}
	addBlocks4x4(d.pic.Y, d.pic.StrideY, d.pic.LumaOffset(baseX, baseY), 16, &res.luma, mask, d.qpY, d.scal.luma4x4(false), false)
	d.addChromaResidual(res)
}
