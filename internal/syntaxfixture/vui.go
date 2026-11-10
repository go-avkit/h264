package syntaxfixture

import "github.com/go-avkit/h264/internal/bits"

const ExtendedSAR = 255

func writeHRD(w *bits.Writer, h *HRD) {
	w.WriteUE(h.CPBCntMinus1)
	w.WriteBits(uint32(h.BitRateScale), 4)
	w.WriteBits(uint32(h.CPBSizeScale), 4)
	for i := 0; i <= int(h.CPBCntMinus1); i++ {
		w.WriteUE(h.BitRateValueMinus1[i])
		w.WriteUE(h.CPBSizeValueMinus1[i])
		w.WriteFlag(h.CBRFlag[i])
	}
	w.WriteBits(uint32(h.InitialCPBRemovalDelayLengthMinus1), 5)
	w.WriteBits(uint32(h.CPBRemovalDelayLengthMinus1), 5)
	w.WriteBits(uint32(h.DPBOutputDelayLengthMinus1), 5)
	w.WriteBits(uint32(h.TimeOffsetLength), 5)
}

func writeVUI(w *bits.Writer, v *VUI) {
	w.WriteFlag(v.AspectRatioInfoPresent)
	if v.AspectRatioInfoPresent {
		w.WriteBits(uint32(v.AspectRatioIDC), 8)
		if v.AspectRatioIDC == ExtendedSAR {
			w.WriteBits(uint32(v.SarWidth), 16)
			w.WriteBits(uint32(v.SarHeight), 16)
		}
	}
	w.WriteFlag(v.OverscanInfoPresent)
	if v.OverscanInfoPresent {
		w.WriteFlag(v.OverscanAppropriate)
	}
	w.WriteFlag(v.VideoSignalTypePresent)
	if v.VideoSignalTypePresent {
		w.WriteBits(uint32(v.VideoFormat), 3)
		w.WriteFlag(v.VideoFullRange)
		w.WriteFlag(v.ColourDescPresent)
		if v.ColourDescPresent {
			w.WriteBits(uint32(v.ColourPrimaries), 8)
			w.WriteBits(uint32(v.TransferCharacteristics), 8)
			w.WriteBits(uint32(v.MatrixCoefficients), 8)
		}
	}
	w.WriteFlag(v.ChromaLocInfoPresent)
	if v.ChromaLocInfoPresent {
		w.WriteUE(v.ChromaSampleLocTypeTop)
		w.WriteUE(v.ChromaSampleLocTypeBottom)
	}
	w.WriteFlag(v.TimingInfoPresent)
	if v.TimingInfoPresent {
		w.WriteBits(v.NumUnitsInTick, 32)
		w.WriteBits(v.TimeScale, 32)
		w.WriteFlag(v.FixedFrameRate)
	}
	w.WriteFlag(v.NalHRDPresent)
	if v.NalHRDPresent {
		writeHRD(w, &v.NalHRD)
	}
	w.WriteFlag(v.VclHRDPresent)
	if v.VclHRDPresent {
		writeHRD(w, &v.VclHRD)
	}
	if v.NalHRDPresent || v.VclHRDPresent {
		w.WriteFlag(v.LowDelayHRD)
	}
	w.WriteFlag(v.PicStructPresent)
	w.WriteFlag(v.BitstreamRestriction)
	if v.BitstreamRestriction {
		w.WriteFlag(v.MotionVectorsOverPicBoundary)
		w.WriteUE(v.MaxBytesPerPicDenom)
		w.WriteUE(v.MaxBitsPerMBDenom)
		w.WriteUE(v.Log2MaxMvLengthHorizontal)
		w.WriteUE(v.Log2MaxMvLengthVertical)
		w.WriteUE(v.MaxNumReorderFrames)
		w.WriteUE(v.MaxDecFrameBuffering)
	}
}

func (s *SPS) RestrictBitstream(reorder int) {
	s.VUIPresent = true
	v := &s.VUI
	v.BitstreamRestriction = true
	v.MotionVectorsOverPicBoundary = true
	v.MaxBytesPerPicDenom = 2
	v.MaxBitsPerMBDenom = 1
	v.Log2MaxMvLengthHorizontal = 15
	v.Log2MaxMvLengthVertical = 15
	v.MaxNumReorderFrames = uint32(reorder)
	v.MaxDecFrameBuffering = uint32(max(int(s.MaxNumRefFrames), reorder, 1))
}
