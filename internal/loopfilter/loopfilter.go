package loopfilter

import (
	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/deblock"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/parallel"
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
	planes := [2][]byte{c.pic.Cb, c.pic.Cr}
	for plane := 0; plane < 2; plane++ {
		qpc := deblock.AverageQP(
			h264.ChromaQP(p.filterQPY(), offsets[plane]),
			h264.ChromaQP(cur.filterQPY(), offsets[plane]))
		deblock.FilterChromaEdgeVertical(planes[plane], c.pic.StrideC,
			c.pic.ChromaOffset(cx, mby*8), bs,
			deblock.IndexA(qpc, cur.AlphaOffset), deblock.IndexB(qpc, cur.BetaOffset))
	}
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
	planes := [2][]byte{c.pic.Cb, c.pic.Cr}
	for plane := 0; plane < 2; plane++ {
		qpc := deblock.AverageQP(
			h264.ChromaQP(p.filterQPY(), offsets[plane]),
			h264.ChromaQP(cur.filterQPY(), offsets[plane]))
		deblock.FilterChromaEdgeHorizontal(planes[plane], c.pic.StrideC,
			c.pic.ChromaOffset(mbx*8, cy), bs,
			deblock.IndexA(qpc, cur.AlphaOffset), deblock.IndexB(qpc, cur.BetaOffset))
	}
}
