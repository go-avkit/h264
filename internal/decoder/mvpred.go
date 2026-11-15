package decoder

import (
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/loopfilter"
)

type motion struct {
	mv  [2]int16
	ref int8
}

var zscanOf = [4][4]int{
	{0, 1, 4, 5},
	{2, 3, 6, 7},
	{8, 9, 12, 13},
	{10, 11, 14, 15},
}

const (
	shapeOther = iota
	shape16x8
	shape8x16
)

func shapeOf(kind int) int {
	switch kind {
	case mbTypeP16x8, mbTypeB16x8:
		return shape16x8
	case mbTypeP8x16, mbTypeB8x16:
		return shape8x16
	}
	return shapeOther
}

func (m *mbState) refIdx(list, blk int) int8 {
	if list == 0 {
		return m.refIdxL0[blk]
	}
	return m.refIdxL1[blk]
}

func (m *mbState) mv(list, blk int) [2]int16 {
	if list == 0 {
		return m.MvL0[blk]
	}
	return m.MvL1[blk]
}

func blockMotion(m *mbState, list, blk int) motion {
	if list == 0 {
		return motion{mv: m.MvL0[blk], ref: m.refIdxL0[blk]}
	}
	return motion{mv: m.MvL1[blk], ref: m.refIdxL1[blk]}
}

func (d *sliceDecoder) neighbourBlock(x, y, curZ int) (*mbState, int) {
	switch {
	case x < 0 && y < 0:
		return d.nb.topLeft, loopfilter.BlkIdxAt(12, 12)
	case x < 0:
		if y >= 16 {
			return nil, 0
		}
		return d.nb.left, loopfilter.BlkIdxAt(12, y&^3)
	case y < 0:
		if x < 16 {
			return d.nb.top, loopfilter.BlkIdxAt(x&^3, 12)
		}
		return d.nb.topRight, loopfilter.BlkIdxAt(0, 12)
	case x >= 16 || y >= 16:
		return nil, 0
	}
	z := zscanOf[y>>2][x>>2]
	if z >= curZ {
		return nil, 0
	}
	return d.cur, z
}

func (d *sliceDecoder) neighbourMotion(list, x, y, curZ int) (motion, bool) {
	m, blk := d.neighbourBlock(x, y, curZ)
	if m == nil {
		return motion{ref: -1}, false
	}
	return blockMotion(m, list, blk), true
}

func (d *sliceDecoder) neighbourMVD(list, x, y, curZ int) [2]uint8 {
	m, blk := d.neighbourBlock(x, y, curZ)
	if m == nil || m.Intra {
		return [2]uint8{}
	}
	if list == 0 {
		return m.mvdL0[blk]
	}
	return m.mvdL1[blk]
}

func median(a, b, c int16) int16 {
	if a > b {
		a, b = b, a
	}
	if b > c {
		b = c
	}
	if a > b {
		b = a
	}
	return b
}

// neighboursFor is 6.4.11.7 for the partition at (x, y) of width w: A is the
// block to its left, B above, C above and to the right, with D (above left)
// standing in for C when C is not available. Each neighbour is located by
// its raster row and column directly (6.4.12.1 for the cases inside the
// macroblock, 6.4.12 for the four neighbouring macroblocks), which is what
// neighbourBlock derives from luma locations one call at a time; the test
// pins the two together over every partition shape.
func (d *sliceDecoder) neighboursFor(list, x, y, w int) (a, b, c motion) {
	row, col := y>>2, x>>2
	curZ := zscanOf[row][col]
	none := motion{ref: -1}
	okA, okB, okC := true, true, true
	switch {
	case col > 0:
		a = blockMotion(d.cur, list, zscanOf[row][col-1])
	case d.nb.left != nil:
		a = blockMotion(d.nb.left, list, zscanOf[row][3])
	default:
		a, okA = none, false
	}
	switch {
	case row > 0:
		b = blockMotion(d.cur, list, zscanOf[row-1][col])
	case d.nb.top != nil:
		b = blockMotion(d.nb.top, list, zscanOf[3][col])
	default:
		b, okB = none, false
	}
	cx := (x + w) >> 2
	switch {
	case row > 0:
		if cx < 4 && zscanOf[row-1][cx] < curZ {
			c = blockMotion(d.cur, list, zscanOf[row-1][cx])
		} else {
			okC = false
		}
	case cx < 4 && d.nb.top != nil:
		c = blockMotion(d.nb.top, list, zscanOf[3][cx])
	case cx >= 4 && d.nb.topRight != nil:
		c = blockMotion(d.nb.topRight, list, zscanOf[3][0])
	default:
		okC = false
	}
	if !okC {
		okC = true
		switch {
		case row > 0 && col > 0:
			c = blockMotion(d.cur, list, zscanOf[row-1][col-1])
		case row > 0 && d.nb.left != nil:
			c = blockMotion(d.nb.left, list, zscanOf[row-1][3])
		case row == 0 && col > 0 && d.nb.top != nil:
			c = blockMotion(d.nb.top, list, zscanOf[3][col-1])
		case row == 0 && col == 0 && d.nb.topLeft != nil:
			c = blockMotion(d.nb.topLeft, list, zscanOf[3][3])
		default:
			c, okC = none, false
		}
	}
	if !okB && !okC && okA {
		b, c = a, a
	}
	return a, b, c
}

func (d *sliceDecoder) predictMV(list, x, y, w, h int, refIdx int8, partIdx, kind int) [2]int16 {
	a, b, c := d.neighboursFor(list, x, y, w)

	switch shape := shapeOf(kind); {
	case shape == shape16x8 && partIdx == 0 && b.ref == refIdx:
		return b.mv
	case shape == shape16x8 && partIdx == 1 && a.ref == refIdx:
		return a.mv
	case shape == shape8x16 && partIdx == 0 && a.ref == refIdx:
		return a.mv
	case shape == shape8x16 && partIdx == 1 && c.ref == refIdx:
		return c.mv
	}

	matches := 0
	var only motion
	if a.ref == refIdx {
		matches++
		only = a
	}
	if b.ref == refIdx {
		matches++
		only = b
	}
	if c.ref == refIdx {
		matches++
		only = c
	}
	if matches == 1 {
		return only.mv
	}
	return [2]int16{
		median(a.mv[0], b.mv[0], c.mv[0]),
		median(a.mv[1], b.mv[1], c.mv[1]),
	}
}

func (d *sliceDecoder) skipMV() [2]int16 {
	if d.nb.left == nil || d.nb.top == nil {
		return [2]int16{}
	}
	a, _ := d.neighbourMotion(0, -1, 0, 0)
	b, _ := d.neighbourMotion(0, 0, -1, 0)
	if a.ref == 0 && a.mv == [2]int16{} {
		return [2]int16{}
	}
	if b.ref == 0 && b.mv == [2]int16{} {
		return [2]int16{}
	}
	return d.predictMV(0, 0, 0, 16, 16, 0, 0, mbTypeP16x16)
}

func absClip70(v int16) uint8 {
	if v < 0 {
		v = -v
	}
	if v > 70 {
		return 70
	}
	return uint8(v)
}

func (d *sliceDecoder) storeMVD(list, x, y, w, h int, mvd [2]int16) {
	packed := [2]uint8{absClip70(mvd[0]), absClip70(mvd[1])}
	for by := y; by < y+h; by += 4 {
		for bx := x; bx < x+w; bx += 4 {
			z := zscanOf[by>>2][bx>>2]
			if list == 0 {
				d.cur.mvdL0[z] = packed
			} else {
				d.cur.mvdL1[z] = packed
			}
		}
	}
}

func (d *sliceDecoder) storeRefIdx(list, x, y, w, h int, ref int8) {
	for by := y; by < y+h; by += 4 {
		for bx := x; bx < x+w; bx += 4 {
			z := zscanOf[by>>2][bx>>2]
			if list == 0 {
				d.cur.refIdxL0[z] = ref
			} else {
				d.cur.refIdxL1[z] = ref
			}
		}
	}
}

func (d *sliceDecoder) storeMotion(list, x, y, w, h int, mv [2]int16, ref int8) {
	var pic *frame.Picture
	if ref >= 0 {
		pic = d.refPictureIn(list, ref)
	}
	for by := y; by < y+h; by += 4 {
		for bx := x; bx < x+w; bx += 4 {
			z := zscanOf[by>>2][bx>>2]
			if list == 0 {
				d.cur.MvL0[z] = mv
				d.cur.refIdxL0[z] = ref
				d.cur.RefPicL0[z] = pic
			} else {
				d.cur.MvL1[z] = mv
				d.cur.refIdxL1[z] = ref
				d.cur.RefPicL1[z] = pic
			}
		}
	}
}
