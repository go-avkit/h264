package deblock

import "github.com/go-videodsp/kernels/lf"

// The *Classified entry points are the edge filters for a caller that already
// knows the class of its four strengths: strong is true when all four are 4,
// false when none is (the only two cases a frame macroblock produces: 4 on an
// intra macroblock edge, at most 3 elsewhere). They skip the recount that
// uniform does on every call; without the kernels they are the plain filters.

// FilterLumaEdgeVerticalClassified is FilterLumaEdgeVertical for strengths of a known class.
func FilterLumaEdgeVerticalClassified(plane []byte, stride, offset int, bS [4]uint8, strong bool, indexA, indexB int) {
	if kernelsLF {
		if strong {
			lf.DeblockLumaIntraV(plane, offset, stride, indexA, indexB)
		} else {
			lf.DeblockLumaV(plane, offset, stride, indexA, indexB, bS)
		}
		return
	}
	FilterLumaEdgeVertical(plane, stride, offset, bS, indexA, indexB)
}

// FilterLumaEdgeHorizontalClassified is FilterLumaEdgeHorizontal for strengths of a known class.
func FilterLumaEdgeHorizontalClassified(plane []byte, stride, offset int, bS [4]uint8, strong bool, indexA, indexB int) {
	if kernelsLF {
		if strong {
			lf.DeblockLumaIntraH(plane, offset, stride, indexA, indexB)
		} else {
			lf.DeblockLumaH(plane, offset, stride, indexA, indexB, bS)
		}
		return
	}
	FilterLumaEdgeHorizontal(plane, stride, offset, bS, indexA, indexB)
}

// FilterChromaPairVerticalClassified is FilterChromaPairVertical for strengths of a known class.
func FilterChromaPairVerticalClassified(cb, cr []byte, stride, offset int, bS [4]uint8, strong bool, indexACb, indexBCb, indexACr, indexBCr int) {
	if kernelsLF {
		if strong {
			lf.DeblockChromaPairIntraV(cb, cr, offset, stride, indexACb, indexBCb, indexACr, indexBCr)
		} else {
			lf.DeblockChromaPairV(cb, cr, offset, stride, indexACb, indexBCb, indexACr, indexBCr, bS)
		}
		return
	}
	FilterChromaPairVertical(cb, cr, stride, offset, bS, indexACb, indexBCb, indexACr, indexBCr)
}

// FilterChromaPairHorizontalClassified is FilterChromaPairHorizontal for strengths of a known class.
func FilterChromaPairHorizontalClassified(cb, cr []byte, stride, offset int, bS [4]uint8, strong bool, indexACb, indexBCb, indexACr, indexBCr int) {
	if kernelsLF {
		if strong {
			lf.DeblockChromaPairIntraH(cb, cr, offset, stride, indexACb, indexBCb, indexACr, indexBCr)
		} else {
			lf.DeblockChromaPairH(cb, cr, offset, stride, indexACb, indexBCb, indexACr, indexBCr, bS)
		}
		return
	}
	FilterChromaPairHorizontal(cb, cr, stride, offset, bS, indexACb, indexBCb, indexACr, indexBCr)
}
