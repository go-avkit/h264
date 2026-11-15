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
