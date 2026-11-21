package deblock

import (
	"sync/atomic"

	"github.com/go-videodsp/kernels/lf"
)

// classifiedFallbacks counts the calls that took the plain filters because the
// build has no kernels, so a test can tell the two routes apart (their output
// is identical by construction).
var classifiedFallbacks atomic.Int64

// Planes is one picture's three planes as the kernels want them: their
// geometry proved once (lf.NewPlane, lf.NewPlanePair), so that an edge pays
// two comparisons for its footprint instead of the arithmetic of the plain
// entry points. It is built for one picture and is read-only afterwards, so the
// macroblock rows of a wavefront share it. A plane the kernels refuse (a stride
// below 16, say) stays on the checked route, and so does every plane of a build
// without kernels.
type Planes struct {
	y, cb, cr        []byte
	strideY, strideC int
	luma             lf.Plane
	chroma           lf.PlanePair
	lumaOK, chromaOK bool
}

// NewPlanes proves the planes of one picture.
func NewPlanes(y, cb, cr []byte, strideY, strideC int) *Planes {
	p := &Planes{y: y, cb: cb, cr: cr, strideY: strideY, strideC: strideC}
	if !kernelsLF {
		return p
	}
	var err error
	p.luma, err = lf.NewPlane(y, strideY)
	p.lumaOK = err == nil
	p.chroma, err = lf.NewPlanePair(cb, cr, strideC)
	p.chromaOK = err == nil
	return p
}

// Proved reports which planes took the proved route (luma, chroma); the others
// use the kernels' checked entry points.
func (p *Planes) Proved() (luma, chroma bool) { return p.lumaOK, p.chromaOK }

// The *Classified entry points are the edge filters for a caller that already
// knows the class of its four strengths: strong is true when all four are 4,
// false when none is (the only two cases a frame macroblock produces: 4 on an
// intra macroblock edge, at most 3 elsewhere). They skip the recount that
// uniform does on every call; without the kernels they are the plain filters.
// off is q0 of the first line, as in the plain entry points.

// LumaVertical is FilterLumaEdgeVertical for strengths of a known class.
func (p *Planes) LumaVertical(off int, bS [4]uint8, strong bool, indexA, indexB int) {
	if p.lumaOK {
		if strong {
			p.luma.LumaIntraV(off, indexA, indexB)
		} else {
			p.luma.LumaV(off, indexA, indexB, bS)
		}
		return
	}
	FilterLumaEdgeVerticalClassified(p.y, p.strideY, off, bS, strong, indexA, indexB)
}

// LumaHorizontal is FilterLumaEdgeHorizontal for strengths of a known class.
func (p *Planes) LumaHorizontal(off int, bS [4]uint8, strong bool, indexA, indexB int) {
	if p.lumaOK {
		if strong {
			p.luma.LumaIntraH(off, indexA, indexB)
		} else {
			p.luma.LumaH(off, indexA, indexB, bS)
		}
		return
	}
	FilterLumaEdgeHorizontalClassified(p.y, p.strideY, off, bS, strong, indexA, indexB)
}

// ChromaVertical is FilterChromaPairVertical for strengths of a known class.
func (p *Planes) ChromaVertical(off int, bS [4]uint8, strong bool, indexACb, indexBCb, indexACr, indexBCr int) {
	if p.chromaOK {
		if strong {
			p.chroma.ChromaPairIntraV(off, indexACb, indexBCb, indexACr, indexBCr)
		} else {
			p.chroma.ChromaPairV(off, indexACb, indexBCb, indexACr, indexBCr, bS)
		}
		return
	}
	FilterChromaPairVerticalClassified(p.cb, p.cr, p.strideC, off, bS, strong, indexACb, indexBCb, indexACr, indexBCr)
}

// ChromaHorizontal is FilterChromaPairHorizontal for strengths of a known class.
func (p *Planes) ChromaHorizontal(off int, bS [4]uint8, strong bool, indexACb, indexBCb, indexACr, indexBCr int) {
	if p.chromaOK {
		if strong {
			p.chroma.ChromaPairIntraH(off, indexACb, indexBCb, indexACr, indexBCr)
		} else {
			p.chroma.ChromaPairH(off, indexACb, indexBCb, indexACr, indexBCr, bS)
		}
		return
	}
	FilterChromaPairHorizontalClassified(p.cb, p.cr, p.strideC, off, bS, strong, indexACb, indexBCb, indexACr, indexBCr)
}

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
	classifiedFallbacks.Add(1)
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
	classifiedFallbacks.Add(1)
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
	classifiedFallbacks.Add(1)
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
	classifiedFallbacks.Add(1)
	FilterChromaPairHorizontal(cb, cr, stride, offset, bS, indexACb, indexBCb, indexACr, indexBCr)
}
