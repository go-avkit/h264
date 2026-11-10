package syntax

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

func TestSPSRoundTripMatrix(t *testing.T) {
	cases := []struct {
		name  string
		build func() *SPS
	}{
		{
			name: "baseline_66_pocType0",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  77,
					LevelIDC:                    30,
					ID:                          0,
					ChromaFormatIDC:             Chroma420,
					Log2MaxFrameNumMinus4:       2,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 2,
					MaxNumRefFrames:             1,
					PicWidthInMbsMinus1:         19,
					PicHeightInMapUnitsMinus1:   14,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "profile66_baseline",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                66,
					ConstraintSet:             0xC0,
					LevelIDC:                  31,
					ID:                        5,
					ChromaFormatIDC:           Chroma420,
					Log2MaxFrameNumMinus4:     4,
					PicOrderCntType:           2,
					MaxNumRefFrames:           2,
					PicWidthInMbsMinus1:       79,
					PicHeightInMapUnitsMinus1: 44,
					FrameMbsOnly:              true,
					Direct8x8Inference:        true,
				}
			},
		},
		{
			name: "profile77_pocType1_withOffsets",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                77,
					LevelIDC:                  40,
					ID:                        3,
					ChromaFormatIDC:           Chroma420,
					Log2MaxFrameNumMinus4:     3,
					PicOrderCntType:           1,
					DeltaPicOrderAlwaysZero:   false,
					OffsetForNonRefPic:        -3,
					OffsetForTopToBottomField: 2,
					OffsetForRefFrame:         []int32{1, -2, 3, -4},
					MaxNumRefFrames:           4,
					PicWidthInMbsMinus1:       119,
					PicHeightInMapUnitsMinus1: 67,
					FrameMbsOnly:              true,
					Direct8x8Inference:        true,
				}
			},
		},
		{
			name: "profile77_pocType1_deltaAlwaysZero_noOffsets",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                77,
					LevelIDC:                  40,
					ID:                        4,
					ChromaFormatIDC:           Chroma420,
					Log2MaxFrameNumMinus4:     3,
					PicOrderCntType:           1,
					DeltaPicOrderAlwaysZero:   true,
					OffsetForNonRefPic:        0,
					OffsetForTopToBottomField: 0,
					OffsetForRefFrame:         []int32{},
					MaxNumRefFrames:           4,
					PicWidthInMbsMinus1:       119,
					PicHeightInMapUnitsMinus1: 67,
					FrameMbsOnly:              true,
					Direct8x8Inference:        true,
				}
			},
		},
		{
			name: "high_chroma_monochrome",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  100,
					LevelIDC:                    41,
					ID:                          1,
					ChromaFormatIDC:             ChromaMonochrome,
					BitDepthLumaMinus8:          0,
					BitDepthChromaMinus8:        0,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             3,
					PicWidthInMbsMinus1:         79,
					PicHeightInMapUnitsMinus1:   44,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "high_chroma420_bitdepth10_scalingMixed",
			build: func() *SPS {
				s := &SPS{
					ProfileIDC:                  100,
					LevelIDC:                    41,
					ID:                          2,
					ChromaFormatIDC:             Chroma420,
					BitDepthLumaMinus8:          2,
					BitDepthChromaMinus8:        2,
					SeqScalingMatrixPresent:     true,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             4,
					PicWidthInMbsMinus1:         79,
					PicHeightInMapUnitsMinus1:   44,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
				s.ScalingList4x4Present[0] = true
				s.ScalingList4x4[0] = flatList4x4(1)
				s.ScalingList4x4Present[2] = true
				s.UseDefaultScaling4x4[2] = true
				s.ScalingList4x4[2] = defaultList4x4()
				s.ScalingList8x8Present[0] = true
				s.ScalingList8x8[0] = flatList8x8(5)
				return s
			},
		},
		{
			name: "high_chroma422",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  100,
					LevelIDC:                    50,
					ID:                          6,
					ChromaFormatIDC:             Chroma422,
					BitDepthLumaMinus8:          0,
					BitDepthChromaMinus8:        0,
					Log2MaxFrameNumMinus4:       1,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 1,
					MaxNumRefFrames:             2,
					PicWidthInMbsMinus1:         119,
					PicHeightInMapUnitsMinus1:   67,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "high444_separateColourPlane_fullScaling",
			build: func() *SPS {
				s := &SPS{
					ProfileIDC:                  244,
					LevelIDC:                    51,
					ID:                          7,
					ChromaFormatIDC:             Chroma444,
					SeparateColourPlane:         true,
					BitDepthLumaMinus8:          4,
					BitDepthChromaMinus8:        4,
					QpprimeYZeroTransformBypass: true,
					SeqScalingMatrixPresent:     true,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             1,
					PicWidthInMbsMinus1:         39,
					PicHeightInMapUnitsMinus1:   29,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
				for i := 0; i < 6; i++ {
					s.ScalingList4x4Present[i] = true
					s.ScalingList4x4[i] = flatList4x4(uint8(i))
				}
				for i := 0; i < 6; i++ {
					s.ScalingList8x8Present[i] = true
					if i%2 == 0 {
						s.UseDefaultScaling8x8[i] = true
						s.ScalingList8x8[i] = defaultList8x8()
					} else {
						s.ScalingList8x8[i] = flatList8x8(uint8(i))
					}
				}
				return s
			},
		},
		{
			name: "high444_notSeparate_noScaling",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  100,
					LevelIDC:                    51,
					ID:                          8,
					ChromaFormatIDC:             Chroma444,
					SeparateColourPlane:         false,
					BitDepthLumaMinus8:          0,
					BitDepthChromaMinus8:        0,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             1,
					PicWidthInMbsMinus1:         39,
					PicHeightInMapUnitsMinus1:   29,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "mbaff_adaptive_true",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  77,
					LevelIDC:                    40,
					ID:                          9,
					ChromaFormatIDC:             Chroma420,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             2,
					PicWidthInMbsMinus1:         21,
					PicHeightInMapUnitsMinus1:   8,
					FrameMbsOnly:                false,
					MBAdaptiveFrameField:        true,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "fieldOnly_mbaff_false",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  77,
					LevelIDC:                    40,
					ID:                          10,
					ChromaFormatIDC:             Chroma420,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             2,
					PicWidthInMbsMinus1:         21,
					PicHeightInMapUnitsMinus1:   8,
					FrameMbsOnly:                false,
					MBAdaptiveFrameField:        false,
					Direct8x8Inference:          true,
				}
			},
		},
		{
			name: "cropping_present_nonzero",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  100,
					LevelIDC:                    40,
					ID:                          11,
					ChromaFormatIDC:             Chroma420,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             1,
					PicWidthInMbsMinus1:         119,
					PicHeightInMapUnitsMinus1:   67,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
					FrameCropping:               true,
					FrameCropLeftOffset:         1,
					FrameCropRightOffset:        2,
					FrameCropTopOffset:          0,
					FrameCropBottomOffset:       4,
				}
			},
		},
		{
			name: "cropping_absent",
			build: func() *SPS {
				return &SPS{
					ProfileIDC:                  77,
					LevelIDC:                    40,
					ID:                          12,
					ChromaFormatIDC:             Chroma420,
					Log2MaxFrameNumMinus4:       0,
					PicOrderCntType:             0,
					Log2MaxPicOrderCntLsbMinus4: 0,
					MaxNumRefFrames:             1,
					PicWidthInMbsMinus1:         119,
					PicHeightInMapUnitsMinus1:   67,
					FrameMbsOnly:                true,
					Direct8x8Inference:          true,
					FrameCropping:               false,
				}
			},
		},
		{
			name: "vui_absent",
			build: func() *SPS {
				s := baseSPS()
				s.ID = 13
				s.VUIPresent = false
				return s
			},
		},
		{
			name: "vui_present_minimal",
			build: func() *SPS {
				s := baseSPS()
				s.ID = 14
				s.VUIPresent = true
				return s
			},
		},
		{
			name: "vui_present_everything",
			build: func() *SPS {
				s := baseSPS()
				s.ID = 15
				s.VUIPresent = true
				v := &s.VUI
				v.AspectRatioInfoPresent = true
				v.AspectRatioIDC = ExtendedSAR
				v.SarWidth = 4
				v.SarHeight = 3
				v.OverscanInfoPresent = true
				v.OverscanAppropriate = true
				v.VideoSignalTypePresent = true
				v.VideoFormat = 5
				v.VideoFullRange = true
				v.ColourDescPresent = true
				v.ColourPrimaries = 1
				v.TransferCharacteristics = 2
				v.MatrixCoefficients = 3
				v.ChromaLocInfoPresent = true
				v.ChromaSampleLocTypeTop = 1
				v.ChromaSampleLocTypeBottom = 2
				v.TimingInfoPresent = true
				v.NumUnitsInTick = 1
				v.TimeScale = 60
				v.FixedFrameRate = true
				v.NalHRDPresent = true
				v.NalHRD = HRD{
					CPBCntMinus1:                       2,
					BitRateScale:                       3,
					CPBSizeScale:                       4,
					BitRateValueMinus1:                 []uint32{100, 200, 300},
					CPBSizeValueMinus1:                 []uint32{10, 20, 30},
					CBRFlag:                            []bool{true, false, true},
					InitialCPBRemovalDelayLengthMinus1: 23,
					CPBRemovalDelayLengthMinus1:        23,
					DPBOutputDelayLengthMinus1:         23,
					TimeOffsetLength:                   24,
				}
				v.VclHRDPresent = true
				v.VclHRD = HRD{
					CPBCntMinus1:                       1,
					BitRateScale:                       1,
					CPBSizeScale:                       1,
					BitRateValueMinus1:                 []uint32{500, 600},
					CPBSizeValueMinus1:                 []uint32{50, 60},
					CBRFlag:                            []bool{false, true},
					InitialCPBRemovalDelayLengthMinus1: 10,
					CPBRemovalDelayLengthMinus1:        11,
					DPBOutputDelayLengthMinus1:         12,
					TimeOffsetLength:                   13,
				}
				v.LowDelayHRD = true
				v.PicStructPresent = true
				v.BitstreamRestriction = true
				v.MotionVectorsOverPicBoundary = true
				v.MaxBytesPerPicDenom = 2
				v.MaxBitsPerMBDenom = 1
				v.Log2MaxMvLengthHorizontal = 16
				v.Log2MaxMvLengthVertical = 16
				v.MaxNumReorderFrames = 2
				v.MaxDecFrameBuffering = 4
				return s
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			orig := c.build()
			b1 := mustWriteSPS(t, orig)
			parsed := mustParseSPS(t, b1)
			b2 := mustWriteSPS(t, parsed)
			bytesEqual(t, c.name, b1, b2)
			if !reflect.DeepEqual(orig, parsed) {
				t.Fatalf("%s: struct mismatch\n orig: %+v\n parsed: %+v", c.name, orig, parsed)
			}
		})
	}
}

func TestSPSDerivedGetters(t *testing.T) {
	tests := []struct {
		name           string
		sps            SPS
		width          int
		height         int
		croppedWidth   int
		croppedHeight  int
		picWidthMbs    int
		frameHeightMbs int
		chromaArrayTy  uint32
		maxFrameNum    uint32
		maxPocLsb      uint32
	}{
		{
			name: "1920x1080_chroma420",
			sps: SPS{
				ChromaFormatIDC:             Chroma420,
				PicWidthInMbsMinus1:         119,
				PicHeightInMapUnitsMinus1:   67,
				FrameMbsOnly:                true,
				FrameCropping:               true,
				FrameCropBottomOffset:       4,
				Log2MaxFrameNumMinus4:       0,
				Log2MaxPicOrderCntLsbMinus4: 0,
			},
			width: 1920, height: 1088,
			croppedWidth: 1920, croppedHeight: 1080,
			picWidthMbs: 120, frameHeightMbs: 68,
			chromaArrayTy: 1, maxFrameNum: 16, maxPocLsb: 16,
		},
		{
			name: "monochrome_cropping",
			sps: SPS{
				ChromaFormatIDC:             ChromaMonochrome,
				PicWidthInMbsMinus1:         9,
				PicHeightInMapUnitsMinus1:   9,
				FrameMbsOnly:                true,
				FrameCropping:               true,
				FrameCropLeftOffset:         1,
				FrameCropRightOffset:        1,
				FrameCropTopOffset:          1,
				FrameCropBottomOffset:       1,
				Log2MaxFrameNumMinus4:       4,
				Log2MaxPicOrderCntLsbMinus4: 6,
			},
			width: 160, height: 160,
			croppedWidth: 158, croppedHeight: 158,
			picWidthMbs: 10, frameHeightMbs: 10,
			chromaArrayTy: 0, maxFrameNum: 256, maxPocLsb: 1024,
		},
		{
			name: "chroma422_cropping",
			sps: SPS{
				ChromaFormatIDC:             Chroma422,
				PicWidthInMbsMinus1:         9,
				PicHeightInMapUnitsMinus1:   9,
				FrameMbsOnly:                true,
				FrameCropping:               true,
				FrameCropLeftOffset:         1,
				FrameCropRightOffset:        1,
				FrameCropTopOffset:          1,
				FrameCropBottomOffset:       1,
				Log2MaxFrameNumMinus4:       0,
				Log2MaxPicOrderCntLsbMinus4: 0,
			},
			width: 160, height: 160,
			croppedWidth: 156, croppedHeight: 158,
			picWidthMbs: 10, frameHeightMbs: 10,
			chromaArrayTy: 2, maxFrameNum: 16, maxPocLsb: 16,
		},
		{
			name: "chroma444_cropping",
			sps: SPS{
				ChromaFormatIDC:             Chroma444,
				PicWidthInMbsMinus1:         9,
				PicHeightInMapUnitsMinus1:   9,
				FrameMbsOnly:                true,
				FrameCropping:               true,
				FrameCropLeftOffset:         1,
				FrameCropRightOffset:        1,
				FrameCropTopOffset:          1,
				FrameCropBottomOffset:       1,
				Log2MaxFrameNumMinus4:       0,
				Log2MaxPicOrderCntLsbMinus4: 0,
			},
			width: 160, height: 160,
			croppedWidth: 158, croppedHeight: 158,
			picWidthMbs: 10, frameHeightMbs: 10,
			chromaArrayTy: 3, maxFrameNum: 16, maxPocLsb: 16,
		},
		{
			name: "interlaced_doubles_vertical_crop_unit",
			sps: SPS{
				ChromaFormatIDC:             Chroma420,
				PicWidthInMbsMinus1:         9,
				PicHeightInMapUnitsMinus1:   9,
				FrameMbsOnly:                false,
				FrameCropping:               true,
				FrameCropTopOffset:          1,
				FrameCropBottomOffset:       1,
				Log2MaxFrameNumMinus4:       0,
				Log2MaxPicOrderCntLsbMinus4: 0,
			},
			width: 160, height: 320,
			croppedWidth: 160, croppedHeight: 312,
			picWidthMbs: 10, frameHeightMbs: 20,
			chromaArrayTy: 1, maxFrameNum: 16, maxPocLsb: 16,
		},
		{
			name: "separateColourPlane_forces_chromaArrayType0",
			sps: SPS{
				ChromaFormatIDC:             Chroma444,
				SeparateColourPlane:         true,
				PicWidthInMbsMinus1:         9,
				PicHeightInMapUnitsMinus1:   9,
				FrameMbsOnly:                true,
				Log2MaxFrameNumMinus4:       0,
				Log2MaxPicOrderCntLsbMinus4: 0,
			},
			width: 160, height: 160,
			croppedWidth: 160, croppedHeight: 160,
			picWidthMbs: 10, frameHeightMbs: 10,
			chromaArrayTy: 0, maxFrameNum: 16, maxPocLsb: 16,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.sps
			if got := s.Width(); got != tc.width {
				t.Errorf("Width() = %d, want %d", got, tc.width)
			}
			if got := s.Height(); got != tc.height {
				t.Errorf("Height() = %d, want %d", got, tc.height)
			}
			if got := s.CroppedWidth(); got != tc.croppedWidth {
				t.Errorf("CroppedWidth() = %d, want %d", got, tc.croppedWidth)
			}
			if got := s.CroppedHeight(); got != tc.croppedHeight {
				t.Errorf("CroppedHeight() = %d, want %d", got, tc.croppedHeight)
			}
			if got := s.PicWidthInMbs(); got != tc.picWidthMbs {
				t.Errorf("PicWidthInMbs() = %d, want %d", got, tc.picWidthMbs)
			}
			if got := s.FrameHeightInMbs(); got != tc.frameHeightMbs {
				t.Errorf("FrameHeightInMbs() = %d, want %d", got, tc.frameHeightMbs)
			}
			if got := s.ChromaArrayType(); got != tc.chromaArrayTy {
				t.Errorf("ChromaArrayType() = %d, want %d", got, tc.chromaArrayTy)
			}
			if got := s.MaxFrameNum(); got != tc.maxFrameNum {
				t.Errorf("MaxFrameNum() = %d, want %d", got, tc.maxFrameNum)
			}
			if got := s.MaxPicOrderCntLsb(); got != tc.maxPocLsb {
				t.Errorf("MaxPicOrderCntLsb() = %d, want %d", got, tc.maxPocLsb)
			}
		})
	}
}

func TestSPSValidationRejects(t *testing.T) {
	buildValid := func() *SPS {
		s := baseSPS()
		s.ProfileIDC = 100
		return s
	}

	tests := []struct {
		name   string
		mutate func(*SPS)
	}{
		{"seq_parameter_set_id_too_large", func(s *SPS) { s.ID = 32 }},
		{"chroma_format_idc_too_large", func(s *SPS) { s.ChromaFormatIDC = 4 }},
		{"bit_depth_luma_too_large", func(s *SPS) { s.BitDepthLumaMinus8 = 7 }},
		{"bit_depth_chroma_too_large", func(s *SPS) { s.BitDepthChromaMinus8 = 7 }},
		{"log2_max_frame_num_too_large", func(s *SPS) { s.Log2MaxFrameNumMinus4 = 13 }},
		{"log2_max_poc_lsb_too_large", func(s *SPS) { s.Log2MaxPicOrderCntLsbMinus4 = 13 }},
		{"max_num_ref_frames_too_large", func(s *SPS) { s.MaxNumRefFrames = 17 }},
		{"pic_width_too_large", func(s *SPS) { s.PicWidthInMbsMinus1 = 1024 }},
		{"pic_height_too_large", func(s *SPS) { s.PicHeightInMapUnitsMinus1 = 1024 }},
		{
			"cropping_removes_whole_picture",
			func(s *SPS) {
				s.FrameCropping = true
				s.FrameCropLeftOffset = uint32(s.PicWidthInMbs()) * 8
				s.FrameCropRightOffset = uint32(s.PicWidthInMbs()) * 8
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := buildValid()
			tc.mutate(s)
			b, err := WriteSPS(s)
			if err != nil {
				t.Fatalf("WriteSPS unexpectedly failed to build fixture: %v", err)
			}
			_, err = ParseSPS(b)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("expected ErrInvalidValue, got %v", err)
			}
		})
	}
}

func TestSPSPicOrderCntTypeTooLarge(t *testing.T) {
	w := bits.NewWriter()
	w.WriteBits(100, 8)
	w.WriteBits(0, 8)
	w.WriteBits(30, 8)
	w.WriteUE(0)
	w.WriteUE(uint32(Chroma420))
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteFlag(false)
	w.WriteFlag(false)
	w.WriteUE(0)
	w.WriteUE(3)
	if err := w.Err(); err != nil {
		t.Fatalf("writer error building fixture: %v", err)
	}
	_, err := ParseSPS(w.Bytes())
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestSPSNumRefFramesInPicOrderCntCycleTooLarge(t *testing.T) {
	s := baseSPS()
	s.PicOrderCntType = 1
	s.OffsetForRefFrame = make([]int32, 256)
	b, err := WriteSPS(s)
	if err != nil {
		t.Fatalf("WriteSPS: %v", err)
	}
	_, err = ParseSPS(b)
	if !errors.Is(err, ErrInvalidValue) {
		t.Fatalf("expected ErrInvalidValue, got %v", err)
	}
}

func TestSPSTruncationNeverPanicsAlwaysErrors(t *testing.T) {
	s := baseSPS()
	s.ProfileIDC = 100
	s.ChromaFormatIDC = Chroma444
	s.SeparateColourPlane = true
	s.SeqScalingMatrixPresent = true
	s.ScalingList4x4Present[0] = true
	s.ScalingList4x4[0] = flatList4x4(1)
	s.PicOrderCntType = 1
	s.OffsetForRefFrame = []int32{1, 2, 3}
	s.FrameCropping = true
	s.FrameCropBottomOffset = 1
	s.VUIPresent = true
	s.VUI.AspectRatioInfoPresent = true
	s.VUI.AspectRatioIDC = ExtendedSAR
	s.VUI.SarWidth = 1
	s.VUI.SarHeight = 1
	s.VUI.TimingInfoPresent = true
	s.VUI.NumUnitsInTick = 1
	s.VUI.TimeScale = 2
	s.VUI.NalHRDPresent = true
	s.VUI.NalHRD = HRD{
		CPBCntMinus1:       0,
		BitRateValueMinus1: []uint32{1},
		CPBSizeValueMinus1: []uint32{1},
		CBRFlag:            []bool{true},
	}
	s.VUI.BitstreamRestriction = true

	full, err := WriteSPS(s)
	if err != nil {
		t.Fatalf("WriteSPS: %v", err)
	}

	for n := 0; n < len(full); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ParseSPS panicked on truncated input len=%d: %v", n, r)
				}
			}()
			_, err := ParseSPS(full[:n])
			if err == nil {
				t.Fatalf("expected error for truncated input of length %d, got nil", n)
			}
		}()
	}
}
