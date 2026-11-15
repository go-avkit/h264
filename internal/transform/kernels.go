package transform

import "github.com/go-videodsp/kernels/itx"

// DequantInverse8x8Add scales the coefficients of b (8.5.9), runs the inverse
// 8x8 transform (8.5.13) and adds the result to the 8x8 block of plane at
// offset (8.5.14, with Clip1). With go-videodsp/kernels it is one call that
// leaves b untouched; otherwise it is Dequant8x8, Inverse8x8 and AddResidual8x8
// and b holds the residual afterwards, so callers must not read b.
func DequantInverse8x8Add(plane []byte, stride, offset int, b *Block8x8, qp int, ls *LevelScale8x8) {
	if kernelsITX && qp >= 0 {
		itx.Dequant8x8IDCTAdd(plane, offset, stride, (*[64]int32)(b), (*[64]int32)(&ls[qp%6]), qp/6)
		return
	}
	Dequant8x8(b, qp, ls)
	Inverse8x8(b)
	AddResidual8x8(plane, stride, offset, b)
}

// KernelsITX reports whether the transforms run through go-videodsp/kernels.
func KernelsITX() bool { return kernelsITX }

// DequantInverse4x4AddBlocks is itx.Dequant4x4IDCTAddBlocks: the blocks of a
// 16x16 (n = 16) or 8x8 (n = 4) region selected by mask, scaled with scale
// (the row for qP % 6), transformed and added to plane in one call. Only
// valid where KernelsITX is true.
func DequantInverse4x4AddBlocks(plane []byte, stride, offset, n int, blocks *[16]Block, mask uint16, scale *[16]int32, qpDiv int, skipDC bool) {
	itx.Dequant4x4IDCTAddBlocks(plane, offset, stride, n, blocks, mask, scale, qpDiv, skipDC)
}

// LumaDCDequant is the luma DC transform and scaling of an Intra16x16
// macroblock (8.5.10), ls0 being the first entry of the scale for qP % 6:
// the kernel where there is one, the Hadamard transform and the scaling
// otherwise.
func LumaDCDequant(b *Block, qp int, ls0 int32) {
	if kernelsITX && qp >= 0 {
		itx.LumaDCDequant(b, ls0, qp/6)
		return
	}
	Hadamard4x4(b)
	shift := qp / 6
	if shift >= 6 {
		s := uint(shift - 6)
		for i := range b {
			b[i] = b[i] * ls0 << s
		}
		return
	}
	s := uint(6 - shift)
	round := int32(1) << (s - 1)
	for i := range b {
		b[i] = (b[i]*ls0 + round) >> s
	}
}
