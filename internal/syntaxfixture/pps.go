package syntaxfixture

import (
	"fmt"

	"github.com/go-avkit/h264/internal/bits"
)

type PPS struct {
	ID                                uint32
	SPSID                             uint32
	CABAC                             bool
	BottomFieldPicOrderInFramePresent bool
	NumSliceGroupsMinus1              uint32

	NumRefIdxL0DefaultActiveMinus1 uint32
	NumRefIdxL1DefaultActiveMinus1 uint32
	WeightedPred                   bool
	WeightedBipredIDC              uint32
	PicInitQPMinus26               int32
	PicInitQSMinus26               int32
	ChromaQPIndexOffset            int32
	DeblockingFilterControlPresent bool
	ConstrainedIntraPred           bool
	RedundantPicCntPresent         bool

	HasExtension              bool
	Transform8x8Mode          bool
	PicScalingMatrixPresent   bool
	ScalingList4x4            [6][16]uint8
	ScalingList8x8            [6][64]uint8
	ScalingList4x4Present     [6]bool
	ScalingList8x8Present     [6]bool
	UseDefaultScaling4x4      [6]bool
	UseDefaultScaling8x8      [6]bool
	SecondChromaQPIndexOffset int32
}

func (p *PPS) SliceQPY(sliceQPDelta int32) int { return 26 + int(p.PicInitQPMinus26+sliceQPDelta) }

var chromaQPTable = [52]int{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
	10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
	29, 30, 31, 32, 32, 33, 34, 34, 35, 35,
	36, 36, 37, 37, 37, 38, 38, 38, 39, 39,
	39, 39,
}

func ChromaQP(qpY, offset int) int {
	qpi := qpY + offset
	if qpi < 0 {
		qpi = 0
	}
	if qpi > 51 {
		qpi = 51
	}
	return chromaQPTable[qpi]
}

func WritePPS(p *PPS, lookupSPS func(id uint32) *SPS) ([]byte, error) {
	w := bits.NewWriterSize(32)
	w.WriteUE(p.ID)
	w.WriteUE(p.SPSID)
	w.WriteFlag(p.CABAC)
	w.WriteFlag(p.BottomFieldPicOrderInFramePresent)
	w.WriteUE(p.NumSliceGroupsMinus1)
	if p.NumSliceGroupsMinus1 != 0 {
		return nil, fmt.Errorf("%w: slice groups", ErrUnsupported)
	}
	w.WriteUE(p.NumRefIdxL0DefaultActiveMinus1)
	w.WriteUE(p.NumRefIdxL1DefaultActiveMinus1)
	w.WriteFlag(p.WeightedPred)
	w.WriteBits(p.WeightedBipredIDC, 2)
	w.WriteSE(p.PicInitQPMinus26)
	w.WriteSE(p.PicInitQSMinus26)
	w.WriteSE(p.ChromaQPIndexOffset)
	w.WriteFlag(p.DeblockingFilterControlPresent)
	w.WriteFlag(p.ConstrainedIntraPred)
	w.WriteFlag(p.RedundantPicCntPresent)

	if p.HasExtension {
		w.WriteFlag(p.Transform8x8Mode)
		w.WriteFlag(p.PicScalingMatrixPresent)
		if p.PicScalingMatrixPresent {
			sps := lookupSPS(p.SPSID)
			if sps == nil {
				return nil, ErrMissingSPS
			}
			extra := 2
			if sps.ChromaFormatIDC == Chroma444 {
				extra = 6
			}
			n := 6
			if p.Transform8x8Mode {
				n += extra
			}
			for i := 0; i < n; i++ {
				if i < 6 {
					w.WriteFlag(p.ScalingList4x4Present[i])
					if p.ScalingList4x4Present[i] {
						writeScalingList(w, p.ScalingList4x4[i][:], p.UseDefaultScaling4x4[i])
					}
				} else {
					w.WriteFlag(p.ScalingList8x8Present[i-6])
					if p.ScalingList8x8Present[i-6] {
						writeScalingList(w, p.ScalingList8x8[i-6][:], p.UseDefaultScaling8x8[i-6])
					}
				}
			}
		}
		w.WriteSE(p.SecondChromaQPIndexOffset)
	}
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		return nil, err
	}
	return w.Bytes(), nil
}
