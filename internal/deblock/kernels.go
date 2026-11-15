package deblock

import "github.com/go-videodsp/kernels/lf"

// uniform classifies the four boundary strengths of an edge for the kernels,
// which filter either bS 0..3 (per group of lines) or bS 4 on every line, and
// nothing mixed. normal: all below 4; strong: all 4.
func uniform(bS [4]uint8) (normal, strong bool) {
	n4 := 0
	for _, b := range bS {
		if b == 4 {
			n4++
		}
	}
	return n4 == 0, n4 == 4
}

// kernelLuma filters one luma edge with the kernels and reports whether it
// did; a mix of bS 4 with lower strengths, or no usable thresholds, is left to
// the caller's scalar code. off is q0 of the first line (clause 8.7.2).
func kernelLuma(plane []byte, stride, off int, bS [4]uint8, indexA, indexB int, vertical bool) bool {
	normal, strong := uniform(bS)
	switch {
	case normal && vertical:
		lf.DeblockLumaV(plane, off, stride, indexA, indexB, bS)
	case normal:
		lf.DeblockLumaH(plane, off, stride, indexA, indexB, bS)
	case strong && vertical:
		lf.DeblockLumaIntraV(plane, off, stride, indexA, indexB)
	case strong:
		lf.DeblockLumaIntraH(plane, off, stride, indexA, indexB)
	default:
		return false
	}
	return true
}

func kernelChroma(plane []byte, stride, off int, bS [4]uint8, indexA, indexB int, vertical bool) bool {
	normal, strong := uniform(bS)
	switch {
	case normal && vertical:
		lf.DeblockChromaV(plane, off, stride, indexA, indexB, bS)
	case normal:
		lf.DeblockChromaH(plane, off, stride, indexA, indexB, bS)
	case strong && vertical:
		lf.DeblockChromaIntraV(plane, off, stride, indexA, indexB)
	case strong:
		lf.DeblockChromaIntraH(plane, off, stride, indexA, indexB)
	default:
		return false
	}
	return true
}

func kernelChromaPair(cb, cr []byte, stride, off int, bS [4]uint8, iaCb, ibCb, iaCr, ibCr int, vertical bool) bool {
	normal, strong := uniform(bS)
	switch {
	case normal && vertical:
		lf.DeblockChromaPairV(cb, cr, off, stride, iaCb, ibCb, iaCr, ibCr, bS)
	case normal:
		lf.DeblockChromaPairH(cb, cr, off, stride, iaCb, ibCb, iaCr, ibCr, bS)
	case strong && vertical:
		lf.DeblockChromaPairIntraV(cb, cr, off, stride, iaCb, ibCb, iaCr, ibCr)
	case strong:
		lf.DeblockChromaPairIntraH(cb, cr, off, stride, iaCb, ibCb, iaCr, ibCr)
	default:
		return false
	}
	return true
}

// FilterChromaPairVertical filters the vertical chroma edge of Cb and Cr at the
// same position (the two planes share their geometry), each with its own
// thresholds: Cb and Cr can have different chroma QP indexes (8-309, 8-310).
func FilterChromaPairVertical(cb, cr []byte, stride, offset int, bS [4]uint8, indexACb, indexBCb, indexACr, indexBCr int) {
	if kernelsLF && kernelChromaPair(cb, cr, stride, offset, bS, indexACb, indexBCb, indexACr, indexBCr, true) {
		return
	}
	FilterChromaEdgeVertical(cb, stride, offset, bS, indexACb, indexBCb)
	FilterChromaEdgeVertical(cr, stride, offset, bS, indexACr, indexBCr)
}

// FilterChromaPairHorizontal is FilterChromaPairVertical for a horizontal edge.
func FilterChromaPairHorizontal(cb, cr []byte, stride, offset int, bS [4]uint8, indexACb, indexBCb, indexACr, indexBCr int) {
	if kernelsLF && kernelChromaPair(cb, cr, stride, offset, bS, indexACb, indexBCb, indexACr, indexBCr, false) {
		return
	}
	FilterChromaEdgeHorizontal(cb, stride, offset, bS, indexACb, indexBCb)
	FilterChromaEdgeHorizontal(cr, stride, offset, bS, indexACr, indexBCr)
}
