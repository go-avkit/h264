package loopfilter

import (
	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/deblock"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/parallel"
	"github.com/go-videodsp/kernels/lf"
)

type MB struct {
	Decoded        bool
	Intra          bool
	IPCM           bool
	QPY            int
	SliceID        int
	NzY            [16]uint8
	MvL0           [16][2]int16
	RefPicL0       [16]*frame.Picture
	MvL1           [16][2]int16
	RefPicL1       [16]*frame.Picture
	Bipredictive   bool
	Transform8x8   bool
	ChromaQPOffset [2]int
	DisableDeblock uint32
	AlphaOffset    int
	BetaOffset     int
}

var blkIdxGrid = [4][4]int{
	{0, 1, 4, 5},
	{2, 3, 6, 7},
	{8, 9, 12, 13},
	{10, 11, 14, 15},
}

func BlkIdxAt(x, y int) int { return blkIdxGrid[y>>2][x>>2] }

func (m *MB) filterQPY() int {
	if m.IPCM {
		return 0
	}
	return m.QPY
}

func lumaNonZero(m *MB, blk int) bool {
	if m.IPCM {
		return true
	}
	if m.Transform8x8 {
		base := blk &^ 3
		return m.NzY[base]|m.NzY[base+1]|m.NzY[base+2]|m.NzY[base+3] != 0
	}
	return m.NzY[blk] != 0
}

func mvApart(a, b [2]int16) bool {
	dx := int(a[0]) - int(b[0])
	dy := int(a[1]) - int(b[1])
	return dx <= -4 || dx >= 4 || dy <= -4 || dy >= 4
}

func motionDiffers(p, q *MB, pBlk, qBlk int) bool {
	v := p.RefPicL0[pBlk] != q.RefPicL0[qBlk]
	if !v && p.RefPicL0[pBlk] != nil {
		v = mvApart(p.MvL0[pBlk], q.MvL0[qBlk])
	}
	if !p.Bipredictive && !q.Bipredictive {
		return v
	}
	if !v {
		v = p.RefPicL1[pBlk] != q.RefPicL1[qBlk] || mvApart(p.MvL1[pBlk], q.MvL1[qBlk])
	}
	if !v {
		return false
	}
	if p.RefPicL0[pBlk] != q.RefPicL1[qBlk] || p.RefPicL1[pBlk] != q.RefPicL0[qBlk] {
		return true
	}
	return mvApart(p.MvL0[pBlk], q.MvL1[qBlk]) || mvApart(p.MvL1[pBlk], q.MvL0[qBlk])
}

func edgeStrength(p, q *MB, pBlk, qBlk int, mbEdge bool) uint8 {
	if p.Intra || q.Intra {
		if mbEdge {
			return 4
		}
		return 3
	}
	if lumaNonZero(p, pBlk) || lumaNonZero(q, qBlk) {
		return 2
	}
	if motionDiffers(p, q, pBlk, qBlk) {
		return 1
	}
	return 0
}

// mbKernel routes a macroblock through one call of the kernels (a variable so
// that a test can compare the two paths).
var mbKernel = deblock.KernelsMB()

type edgeContext struct {
	at  func(mbx, mby int) *MB
	pic *frame.Picture
}

func Apply(pic *frame.Picture, widthMBs, heightMBs int, at func(mbx, mby int) *MB) {
	if pic == nil || at == nil {
		return
	}
	ctx := edgeContext{at: at, pic: pic}
	parallel.Wavefront(heightMBs, widthMBs, 2, ctx.filterMB)
}

func (c *edgeContext) neighbourFor(m *MB, mbx, mby, dx, dy int) *MB {
	n := c.at(mbx+dx, mby+dy)
	if n == nil || !n.Decoded {
		return nil
	}
	if m.DisableDeblock == 2 && n.SliceID != m.SliceID {
		return nil
	}
	return n
}

func (c *edgeContext) filterMB(mbx, mby int) {
	cur := c.at(mbx, mby)
	if cur == nil || !cur.Decoded || cur.DisableDeblock == 1 {
		return
	}
	left := c.neighbourFor(cur, mbx, mby, -1, 0)
	top := c.neighbourFor(cur, mbx, mby, 0, -1)

	step := 1
	if cur.Transform8x8 {
		step = 2
	}
	// last is one past the last edge to visit. A macroblock without a coded
	// coefficient whose sixteen blocks share one motion has boundary strength 0
	// on every internal edge (8.7.2.1: no intra, no coefficients, same
	// reference pictures and vectors), so only its left and top edges can
	// filter. P_Skip and 16x16 partitions with no residual are most of a
	// P picture, and each of them used to compute twenty-four strengths to
	// find that out.
	last := 4
	if cur.internalEdgesUnfiltered() {
		last = 1
	}
	// With the kernels the macroblock is filtered in one call; strengths the
	// call does not take (a 4 beside other values) leave it to the per-edge path.
	if mbKernel && c.filterMBKernel(cur, left, top, mbx, mby, step, last) {
		return
	}
	for e := 0; e < last; e += step {
		c.verticalEdge(cur, left, mbx, mby, e)
	}
	for e := 0; e < last; e += step {
		c.horizontalEdge(cur, top, mbx, mby, e)
	}
}

// internalEdgesUnfiltered reports whether every edge inside the macroblock has
// boundary strength 0: it is not intra, none of its blocks has a coefficient,
// and all sixteen carry the same prediction (the same reference pictures and
// vectors in both lists, which is what motionDiffers compares).
func (m *MB) internalEdgesUnfiltered() bool {
	if m.Intra || m.IPCM {
		return false
	}
	for _, nz := range m.NzY {
		if nz != 0 {
			return false
		}
	}
	for b := 1; b < 16; b++ {
		if m.RefPicL0[b] != m.RefPicL0[0] || m.MvL0[b] != m.MvL0[0] {
			return false
		}
		if m.Bipredictive && (m.RefPicL1[b] != m.RefPicL1[0] || m.MvL1[b] != m.MvL1[0]) {
			return false
		}
	}
	return true
}

func (c *edgeContext) verticalEdge(cur, left *MB, mbx, mby, e int) {
	p := cur
	mbEdge := e == 0
	if mbEdge {
		if left == nil {
			return
		}
		p = left
	}
	x := e * 4
	var bs [4]uint8
	any := false
	for g := 0; g < 4; g++ {
		y := g * 4
		qBlk := BlkIdxAt(x, y)
		var pBlk int
		if mbEdge {
			pBlk = BlkIdxAt(12, y)
		} else {
			pBlk = BlkIdxAt(x-4, y)
		}
		bs[g] = edgeStrength(p, cur, pBlk, qBlk, mbEdge)
		if bs[g] != 0 {
			any = true
		}
	}
	if !any {
		return
	}
	qpAv := deblock.AverageQP(p.filterQPY(), cur.filterQPY())
	ia := deblock.IndexA(qpAv, cur.AlphaOffset)
	ib := deblock.IndexB(qpAv, cur.BetaOffset)
	deblock.FilterLumaEdgeVertical(c.pic.Y, c.pic.StrideY,
		c.pic.LumaOffset(mbx*16+x, mby*16), bs, ia, ib)

	if e != 0 && e != 2 {
		return
	}
	cx := mbx*8 + e*2
	offsets := cur.ChromaQPOffset
	var cia, cib [2]int
	for plane := 0; plane < 2; plane++ {
		qpc := deblock.AverageQP(
			h264.ChromaQP(p.filterQPY(), offsets[plane]),
			h264.ChromaQP(cur.filterQPY(), offsets[plane]))
		cia[plane], cib[plane] = deblock.IndexA(qpc, cur.AlphaOffset), deblock.IndexB(qpc, cur.BetaOffset)
	}
	deblock.FilterChromaPairVertical(c.pic.Cb, c.pic.Cr, c.pic.StrideC,
		c.pic.ChromaOffset(cx, mby*8), bs, cia[0], cib[0], cia[1], cib[1])
}

func (c *edgeContext) horizontalEdge(cur, top *MB, mbx, mby, e int) {
	p := cur
	mbEdge := e == 0
	if mbEdge {
		if top == nil {
			return
		}
		p = top
	}
	y := e * 4
	var bs [4]uint8
	any := false
	for g := 0; g < 4; g++ {
		x := g * 4
		qBlk := BlkIdxAt(x, y)
		var pBlk int
		if mbEdge {
			pBlk = BlkIdxAt(x, 12)
		} else {
			pBlk = BlkIdxAt(x, y-4)
		}
		bs[g] = edgeStrength(p, cur, pBlk, qBlk, mbEdge)
		if bs[g] != 0 {
			any = true
		}
	}
	if !any {
		return
	}
	qpAv := deblock.AverageQP(p.filterQPY(), cur.filterQPY())
	deblock.FilterLumaEdgeHorizontal(c.pic.Y, c.pic.StrideY,
		c.pic.LumaOffset(mbx*16, mby*16+y), bs,
		deblock.IndexA(qpAv, cur.AlphaOffset), deblock.IndexB(qpAv, cur.BetaOffset))

	if e != 0 && e != 2 {
		return
	}
	cy := mby*8 + e*2
	offsets := cur.ChromaQPOffset
	var ia, ib [2]int
	for plane := 0; plane < 2; plane++ {
		qpc := deblock.AverageQP(
			h264.ChromaQP(p.filterQPY(), offsets[plane]),
			h264.ChromaQP(cur.filterQPY(), offsets[plane]))
		ia[plane], ib[plane] = deblock.IndexA(qpc, cur.AlphaOffset), deblock.IndexB(qpc, cur.BetaOffset)
	}
	deblock.FilterChromaPairHorizontal(c.pic.Cb, c.pic.Cr, c.pic.StrideC,
		c.pic.ChromaOffset(mbx*8, cy), bs, ia[0], ib[0], ia[1], ib[1])
}

// strengths is the four boundary strengths of edge e between p and q (8.7.2.1):
// vertical or horizontal, mbEdge for the edge shared with the neighbour p.
func strengths(p, q *MB, e int, vertical, mbEdge bool) (bs [4]uint8) {
	for g := 0; g < 4; g++ {
		var qBlk, pBlk int
		switch {
		case vertical && mbEdge:
			qBlk, pBlk = BlkIdxAt(0, g*4), BlkIdxAt(12, g*4)
		case vertical:
			qBlk, pBlk = BlkIdxAt(e*4, g*4), BlkIdxAt(e*4-4, g*4)
		case mbEdge:
			qBlk, pBlk = BlkIdxAt(g*4, 0), BlkIdxAt(g*4, 12)
		default:
			qBlk, pBlk = BlkIdxAt(g*4, e*4), BlkIdxAt(g*4, e*4-4)
		}
		bs[g] = edgeStrength(p, q, pBlk, qBlk, mbEdge)
	}
	return bs
}

// thresholds are the luma and chroma indexes of an edge between p and q
// (8-306 to 8-311, 8-454, 8-455).
func thresholds(p, q *MB) (luma lf.Thresholds, chroma [2]lf.Thresholds) {
	qpAv := deblock.AverageQP(p.filterQPY(), q.filterQPY())
	luma = lf.Thresholds{IndexA: uint8(deblock.IndexA(qpAv, q.AlphaOffset)), IndexB: uint8(deblock.IndexB(qpAv, q.BetaOffset))}
	for plane := 0; plane < 2; plane++ {
		qpc := deblock.AverageQP(
			h264.ChromaQP(p.filterQPY(), q.ChromaQPOffset[plane]),
			h264.ChromaQP(q.filterQPY(), q.ChromaQPOffset[plane]))
		chroma[plane] = lf.Thresholds{IndexA: uint8(deblock.IndexA(qpc, q.AlphaOffset)), IndexB: uint8(deblock.IndexB(qpc, q.BetaOffset))}
	}
	return luma, chroma
}

// filterMBKernel filters the macroblock with one call of the kernels and
// reports whether it did.
func (c *edgeContext) filterMBKernel(cur, left, top *MB, mbx, mby, step, last int) bool {
	var e lf.MBEdges
	for k := 0; k < last; k += step {
		if k == 0 {
			if left != nil {
				e.BS[0][0] = strengths(left, cur, 0, true, true)
			}
			if top != nil {
				e.BS[1][0] = strengths(top, cur, 0, false, true)
			}
			continue
		}
		e.BS[0][k] = strengths(cur, cur, k, true, false)
		e.BS[1][k] = strengths(cur, cur, k, false, false)
	}
	if !deblock.MBStrengthsOK(&e) {
		return false
	}
	inL, inC := thresholds(cur, cur)
	leftL, leftC, topL, topC := inL, inC, inL, inC
	if left != nil {
		leftL, leftC = thresholds(left, cur)
	}
	if top != nil {
		topL, topC = thresholds(top, cur)
	}
	e.Luma = [2][2]lf.Thresholds{{leftL, inL}, {topL, inL}}
	for p := 0; p < 2; p++ {
		e.Chroma[p] = [2][2]lf.Thresholds{{leftC[p], inC[p]}, {topC[p], inC[p]}}
	}
	pic := c.pic
	deblock.FilterMB(pic.Y, pic.LumaOffset(mbx*16, mby*16), pic.StrideY,
		pic.Cb, pic.Cr, pic.ChromaOffset(mbx*8, mby*8), pic.StrideC, &e)
	return true
}
