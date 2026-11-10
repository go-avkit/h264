package decoder

import (
	h264 "github.com/go-avkit/h264"
	syntax "github.com/go-avkit/h264/internal/syntaxfixture"
)

// The stream-building tests describe their parameter sets with the fixture types
// of internal/syntax, which can write them out; the decoder works on h264's.
// These make the second from the first.

func rootSPS(s *syntax.SPS) *h264.SPS {
	r := &h264.SPS{
		Profile:                   s.ProfileIDC,
		ConstraintFlags:           s.ConstraintSet,
		Level:                     s.LevelIDC,
		ID:                        s.ID,
		ChromaFormat:              uint8(s.ChromaFormatIDC),
		SeparatePlanes:            s.SeparateColourPlane,
		BitDepthLuma:              uint8(s.BitDepthLumaMinus8) + 8,
		BitDepthChroma:            uint8(s.BitDepthChromaMinus8) + 8,
		TransformBypass:           s.QpprimeYZeroTransformBypass,
		ScalingPresent:            s.SeqScalingMatrixPresent,
		FrameMBSOnly:              s.FrameMbsOnly,
		MBAdaptive:                s.MBAdaptiveFrameField,
		Direct8x8:                 s.Direct8x8Inference,
		MBWidth:                   uint32(s.PicWidthInMbs()),
		MBHeight:                  uint32(s.FrameHeightInMbs()),
		Width:                     uint32(s.CroppedWidth()),
		Height:                    uint32(s.CroppedHeight()),
		MaxNumRefFrames:           s.MaxNumRefFrames,
		GapsAllowed:               s.GapsInFrameNumValueAllowed,
		Log2MaxFrameNum:           uint8(s.Log2MaxFrameNumMinus4) + 4,
		POCType:                   s.PicOrderCntType,
		Log2MaxPOCLSB:             uint8(s.Log2MaxPicOrderCntLsbMinus4) + 4,
		POCAlwaysZero:             s.DeltaPicOrderAlwaysZero,
		OffsetForNonRefPic:        s.OffsetForNonRefPic,
		OffsetForTopToBottomField: s.OffsetForTopToBottomField,
		OffsetForRefFrame:         s.OffsetForRefFrame,
		VUIPresent:                s.VUIPresent,
		VUI: h264.VUI{
			BitstreamRestriction: s.VUI.BitstreamRestriction,
			MaxNumReorderFrames:  s.VUI.MaxNumReorderFrames,
			MaxDecFrameBuffering: s.VUI.MaxDecFrameBuffering,
		},
	}
	r.Scaling = h264.ScalingLists{
		List4x4: s.ScalingList4x4, List8x8: s.ScalingList8x8,
		Present4x4: s.ScalingList4x4Present, Present8x8: s.ScalingList8x8Present,
		UseDefault4x4: s.UseDefaultScaling4x4, UseDefault8x8: s.UseDefaultScaling8x8,
	}
	return r
}

func rootPPS(p *syntax.PPS) *h264.PPS {
	return &h264.PPS{
		ID:                   p.ID,
		SPSID:                p.SPSID,
		CABAC:                p.CABAC,
		BottomFieldPicOrder:  p.BottomFieldPicOrderInFramePresent,
		NumRefIdxL0:          p.NumRefIdxL0DefaultActiveMinus1 + 1,
		NumRefIdxL1:          p.NumRefIdxL1DefaultActiveMinus1 + 1,
		WeightedPred:         p.WeightedPred,
		WeightedBipredIDC:    uint8(p.WeightedBipredIDC),
		InitQP:               26 + p.PicInitQPMinus26,
		InitQS:               26 + p.PicInitQSMinus26,
		ChromaQPIndexOffset:  p.ChromaQPIndexOffset,
		DeblockingControl:    p.DeblockingFilterControlPresent,
		ConstrainedIntraPred: p.ConstrainedIntraPred,
		RedundantPicCnt:      p.RedundantPicCntPresent,
		Transform8x8:         p.Transform8x8Mode,
		SecondChromaQPOffset: p.SecondChromaQPIndexOffset,
		ScalingPresent:       p.PicScalingMatrixPresent,
		Scaling: h264.ScalingLists{
			List4x4: p.ScalingList4x4, List8x8: p.ScalingList8x8,
			Present4x4: p.ScalingList4x4Present, Present8x8: p.ScalingList8x8Present,
			UseDefault4x4: p.UseDefaultScaling4x4, UseDefault8x8: p.UseDefaultScaling8x8,
		},
	}
}
