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

// PredictLuma writes the quarter-sample luma prediction of the w x h block at
// src[srcOff] displaced by (mvx, mvy) quarter samples to dst.
//
// Where github.com/go-videodsp/kernels has assembly (arm64) it does the work;
// elsewhere the portable code of this package, which on amd64 sits behind the
// avo kernels of internal/simd. The caller proves the footprint lies in src.
func PredictLuma(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if kernelsLuma && kernelSize(w, h) && !shares(dst, src) {
		kmc.PutLuma(dst, dstOff, dstStride, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), srcStride, w, h, mvx&3, mvy&3)
		return
	}
	predictLumaPortable(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, mvx, mvy)
}

// PredictLumaAvg is PredictLuma averaged into dst, (dst + p + 1) >> 1 (8-273):
// the second prediction of a bi-predicted block. Fused into the producer where
// the kernels are used; otherwise a prediction block and the average.
func PredictLumaAvg(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff int, w, h, mvx, mvy int) {
	if kernelsLuma && kernelSize(w, h) && !shares(dst, src) {
		kmc.AvgLuma(dst, dstOff, dstStride, src, srcOff+(mvy>>2)*srcStride+(mvx>>2), srcStride, w, h, mvx&3, mvy&3)
		return
	}
	var buf [256]byte
	predictLumaPortable(buf[:w*h], w, 0, src, srcStride, srcOff, w, h, mvx, mvy)
	Average(dst, dstStride, dstOff, dst, dstStride, dstOff, buf[:w*h], w, 0, w, h)
}

// FusedAverage reports whether PredictLumaAvg is a single fused pass.
const FusedAverage = kernelsLuma
