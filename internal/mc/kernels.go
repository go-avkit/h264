package mc

import kmc "github.com/go-videodsp/kernels/mc"

// kernelSize reports whether github.com/go-videodsp/kernels predicts a w x h
// luma block: the seven sizes the standard's partitions give. A merged run of
// equal motion can make others (4x16, 12 wide...), which take the portable path.
func kernelSize(w, h int) bool {
	switch w {
	case 16:
		return h == 16 || h == 8
	case 8:
		return h == 16 || h == 8 || h == 4
	case 4:
		return h == 8 || h == 4
	}
	return false
}

// useLumaKernel is the decision between the kernels and the portable path: the
// kernels exist on this build, take this block size, and the destination does
// not share memory with the source (they panic on that).
func useLumaKernel(dst, src []byte, w, h int) bool {
	return kernelsLuma && kernelSize(w, h) && !shares(dst, src)
}

// PredictLuma writes the quarter-sample luma prediction of the w x h block at
// src[srcOff] displaced by (mvx, mvy) quarter samples to dst.
//
// Where github.com/go-videodsp/kernels has assembly (arm64) it does the work;
// elsewhere the portable code of this package, which on amd64 sits behind the
// avo kernels of internal/simd. The caller proves the footprint lies in src.
func PredictLuma(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if useLumaKernel(dst, src, w, h) {
		kmc.PutLuma(dst, dstOff, dstStride, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), srcStride, w, h, mvx&3, mvy&3)
		return
	}
	predictLumaPortable(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, mvx, mvy)
}

// PredictLumaAvg is PredictLuma averaged into dst, (dst + p + 1) >> 1 (8-273):
// the second prediction of a bi-predicted block. Fused into the producer where
// the kernels are used; otherwise a prediction block and the average.
func PredictLumaAvg(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if useLumaKernel(dst, src, w, h) {
		kmc.AvgLuma(dst, dstOff, dstStride, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), srcStride, w, h, mvx&3, mvy&3)
		return
	}
	var buf [256]byte
	predictLumaPortable(buf[:w*h], w, 0, src, srcStride, srcOff, w, h, mvx, mvy)
	Average(dst, dstStride, dstOff, dst, dstStride, dstOff, buf[:w*h], w, 0, w, h)
}

// PredictChromaPair is PredictChroma for the Cb and Cr planes of one block,
// which share the position, strides, size and vector: one call of the kernels
// where they are used, two of PredictChroma otherwise. The caller proves the
// (w+1) x (h+1) footprint lies in both sources.
func PredictChromaPair(dstCb, dstCr []byte, dstStride, dstOff int, srcCb, srcCr []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if kernelsChroma && !shares(dstCb, srcCb) && !shares(dstCb, srcCr) && !shares(dstCr, srcCb) && !shares(dstCr, srcCr) && !shares(dstCb, dstCr) {
		kmc.PutChromaPair(dstCb, dstCr, dstOff, dstStride, srcCb, srcCr, srcOff+(mvy>>3)*srcStride+(mvx>>3), srcStride, w, h, mvx&7, mvy&7)
		return
	}
	PredictChroma(dstCb, dstStride, dstOff, srcCb, srcStride, srcOff, w, h, mvx, mvy)
	PredictChroma(dstCr, dstStride, dstOff, srcCr, srcStride, srcOff, w, h, mvx, mvy)
}

// PredictChromaPairAvg is PredictChromaPair averaged into the destinations,
// (dst + p + 1) >> 1 (8-273): the second prediction of a bi-predicted block.
// One fused call of the kernels where they are used; otherwise a prediction
// block per plane and the average.
func PredictChromaPairAvg(dstCb, dstCr []byte, dstStride, dstOff int, srcCb, srcCr []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if kernelsChroma && !shares(dstCb, srcCb) && !shares(dstCb, srcCr) && !shares(dstCr, srcCb) && !shares(dstCr, srcCr) && !shares(dstCb, dstCr) {
		kmc.AvgChromaPair(dstCb, dstCr, dstOff, dstStride, srcCb, srcCr, srcOff+(mvy>>3)*srcStride+(mvx>>3), srcStride, w, h, mvx&7, mvy&7)
		return
	}
	var cb, cr [64]byte
	PredictChroma(cb[:w*h], w, 0, srcCb, srcStride, srcOff, w, h, mvx, mvy)
	PredictChroma(cr[:w*h], w, 0, srcCr, srcStride, srcOff, w, h, mvx, mvy)
	Average(dstCb, dstStride, dstOff, dstCb, dstStride, dstOff, cb[:w*h], w, 0, w, h)
	Average(dstCr, dstStride, dstOff, dstCr, dstStride, dstOff, cr[:w*h], w, 0, w, h)
}

// FusedAverage reports whether PredictLumaAvg is a single fused pass.
var FusedAverage = kernelsLuma
