package mc

import kmc "github.com/go-videodsp/kernels/mc"

// The functions below are PredictLuma, PredictLumaAvg, PredictChromaPair and
// PredictChromaPairAvg for pictures whose planes were proved once (kmc.Plane,
// kmc.ChromaPair), so the kernels check a block with a few comparisons instead
// of the full argument validation of the plain functions. Each reports whether
// it did the work: false means nothing was written (the kernels are not used on
// this build, or the planes refused the block) and the caller takes the path of
// the plain functions. The caller proves the footprint lies in src, as for them.

// PutLumaPlane predicts the w x h block at src[srcOff] displaced by (mvx, mvy)
// quarter samples into dst at dstOff.
func PutLumaPlane(dst *kmc.Plane, dstOff int, src *kmc.Plane, srcOff, srcStride, w, h, mvx, mvy int) bool {
	return kernelsLuma &&
		dst.PutLuma(dstOff, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), w, h, mvx&3, mvy&3) == nil
}

// AvgLumaPlane is PutLumaPlane averaged into dst, (dst + p + 1) >> 1 (8-273).
func AvgLumaPlane(dst *kmc.Plane, dstOff int, src *kmc.Plane, srcOff, srcStride, w, h, mvx, mvy int) bool {
	return kernelsLuma &&
		dst.AvgLuma(dstOff, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), w, h, mvx&3, mvy&3) == nil
}

// PutChromaPlane is PutLumaPlane for the Cb and Cr planes, vector in eighths.
func PutChromaPlane(dst *kmc.ChromaPair, dstOff int, src *kmc.ChromaPair, srcOff, srcStride, w, h, mvx, mvy int) bool {
	return kernelsChroma &&
		dst.PutChroma(dstOff, src, srcOff+(mvy>>3)*srcStride+(mvx>>3), w, h, mvx&7, mvy&7) == nil
}

// AvgChromaPlane is PutChromaPlane averaged into dst.
func AvgChromaPlane(dst *kmc.ChromaPair, dstOff int, src *kmc.ChromaPair, srcOff, srcStride, w, h, mvx, mvy int) bool {
	return kernelsChroma &&
		dst.AvgChroma(dstOff, src, srcOff+(mvy>>3)*srcStride+(mvx>>3), w, h, mvx&7, mvy&7) == nil
}
