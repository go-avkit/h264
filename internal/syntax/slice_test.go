package syntax

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/nal"
)

func TestSliceTypeHelpers(t *testing.T) {
	wantBase := map[SliceType]SliceType{}
	wantString := map[SliceType]string{}
	for i := SliceType(0); i <= 9; i++ {
		base := i % 5
		wantBase[i] = base
		switch base {
		case SliceP:
			wantString[i] = "P"
		case SliceB:
			wantString[i] = "B"
		case SliceI:
			wantString[i] = "I"
		case SliceSP:
			wantString[i] = "SP"
		case SliceSI:
			wantString[i] = "SI"
		}
	}

	for st := SliceType(0); st <= 9; st++ {
		t.Run(st.String(), func(t *testing.T) {
			if got := st.Base(); got != wantBase[st] {
				t.Errorf("Base() = %d, want %d", got, wantBase[st])
			}
			if got := st.IsI(); got != (wantBase[st] == SliceI) {
				t.Errorf("IsI() = %v", got)
			}
			if got := st.IsP(); got != (wantBase[st] == SliceP) {
				t.Errorf("IsP() = %v", got)
			}
			if got := st.IsB(); got != (wantBase[st] == SliceB) {
				t.Errorf("IsB() = %v", got)
			}
			if got := st.IsSI(); got != (wantBase[st] == SliceSI) {
				t.Errorf("IsSI() = %v", got)
			}
			if got := st.IsSP(); got != (wantBase[st] == SliceSP) {
				t.Errorf("IsSP() = %v", got)
			}
			if got := st.AllSlicesSameType(); got != (st >= 5) {
				t.Errorf("AllSlicesSameType() = %v, want %v", got, st >= 5)
			}
			if got := st.String(); got != wantString[st] {
				t.Errorf("String() = %q, want %q", got, wantString[st])
			}
			for base := SliceType(0); base <= 4; base++ {
				alias := base + 5
				if alias.IsI() != base.IsI() || alias.IsP() != base.IsP() || alias.IsB() != base.IsB() ||
					alias.IsSI() != base.IsSI() || alias.IsSP() != base.IsSP() {
					t.Errorf("alias %d does not match predicates of base %d", alias, base)
				}
			}
		})
	}
}

func TestSliceHeaderMbaffFrame(t *testing.T) {
	tests := []struct {
		name     string
		mbaff    bool
		fieldPic bool
		want     bool
	}{
		{"mbaff_and_frame", true, false, true},
		{"mbaff_but_field", true, true, false},
		{"no_mbaff_frame", false, false, false},
		{"no_mbaff_field", false, true, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sps := &SPS{MBAdaptiveFrameField: tc.mbaff}
			h := &SliceHeader{FieldPic: tc.fieldPic}
			if got := h.MbaffFrame(sps); got != tc.want {
				t.Errorf("MbaffFrame() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSliceHeaderNumRefIdxActive(t *testing.T) {
	pps := &PPS{NumRefIdxL0DefaultActiveMinus1: 2, NumRefIdxL1DefaultActiveMinus1: 1}

	noOverride := &SliceHeader{NumRefIdxActiveOverride: false}
	if got := noOverride.NumRefIdxL0Active(pps); got != 3 {
		t.Errorf("NumRefIdxL0Active() = %d, want 3", got)
	}
	if got := noOverride.NumRefIdxL1Active(pps); got != 2 {
		t.Errorf("NumRefIdxL1Active() = %d, want 2", got)
	}

	override := &SliceHeader{
		NumRefIdxActiveOverride: true,
		NumRefIdxL0ActiveMinus1: 5,
		NumRefIdxL1ActiveMinus1: 4,
	}
	if got := override.NumRefIdxL0Active(pps); got != 6 {
		t.Errorf("NumRefIdxL0Active() = %d, want 6", got)
	}
	if got := override.NumRefIdxL1Active(pps); got != 5 {
		t.Errorf("NumRefIdxL1Active() = %d, want 5", got)
	}
}

type sliceScenario struct {
	name string
	sps  *SPS
	pps  *PPS
	h    *SliceHeader
}

func buildSliceScenarios() []sliceScenario {
	var out []sliceScenario

	add := func(name string, sps *SPS, pps *PPS, h *SliceHeader) {
		out = append(out, sliceScenario{name: name, sps: sps, pps: pps, h: h})
	}

	for _, st := range []SliceType{SliceI, SliceSI} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			FirstMBInSlice:      0,
			SliceType:           st,
			PPSID:               pps.ID,
			FrameNum:            1,
			IDR:                 true,
			NalRefIDC:           3,
			IDRPicID:            7,
			NoOutputOfPriorPics: true,
			LongTermReference:   false,
			SliceQPDelta:        2,
		}
		add("idr_"+st.String(), sps, pps, h)
	}

	for _, st := range []SliceType{SliceI + 5, SliceSI + 5} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			FirstMBInSlice:    0,
			SliceType:         st,
			PPSID:             pps.ID,
			FrameNum:          1,
			IDR:               true,
			NalRefIDC:         1,
			IDRPicID:          0,
			LongTermReference: true,
			SliceQPDelta:      0,
		}
		add("idr_alias_"+st.String(), sps, pps, h)
	}

	for _, st := range []SliceType{SliceP, SliceB, SliceSP} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			FirstMBInSlice: 2,
			SliceType:      st,
			PPSID:          pps.ID,
			FrameNum:       5,
			IDR:            false,
			NalRefIDC:      0,
			SliceQPDelta:   -3,
		}
		if st.IsSP() {
			h.SPForSwitch = true
			h.SliceQSDelta = 1
		}
		add("nonidr_"+st.String(), sps, pps, h)
	}

	for _, st := range []SliceType{SliceP + 5, SliceB + 5, SliceSP + 5} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			FirstMBInSlice:        1,
			SliceType:             st,
			PPSID:                 pps.ID,
			FrameNum:              2,
			IDR:                   false,
			NalRefIDC:             2,
			SliceQPDelta:          4,
			AdaptiveRefPicMarking: false,
		}
		if st.IsSP() {
			h.SliceQSDelta = -1
		}
		add("nonidr_alias_"+st.String(), sps, pps, h)
	}

	{
		sps := baseSPS()
		sps.PicOrderCntType = 0
		pps := basePPS(sps.ID)
		pps.BottomFieldPicOrderInFramePresent = false
		h := &SliceHeader{
			SliceType:      SliceP,
			PPSID:          pps.ID,
			FrameNum:       3,
			PicOrderCntLsb: 5,
			SliceQPDelta:   0,
		}
		add("poc0_no_bottom_field_flag", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.PicOrderCntType = 0
		pps := basePPS(sps.ID)
		pps.BottomFieldPicOrderInFramePresent = true
		h := &SliceHeader{
			SliceType:              SliceP,
			PPSID:                  pps.ID,
			FrameNum:               3,
			PicOrderCntLsb:         9,
			DeltaPicOrderCntBottom: -2,
			SliceQPDelta:           0,
		}
		add("poc0_with_bottom_field_flag", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.PicOrderCntType = 1
		sps.DeltaPicOrderAlwaysZero = false
		pps := basePPS(sps.ID)
		pps.BottomFieldPicOrderInFramePresent = true
		h := &SliceHeader{
			SliceType:        SliceP,
			PPSID:            pps.ID,
			FrameNum:         3,
			DeltaPicOrderCnt: [2]int32{7, -8},
			SliceQPDelta:     0,
		}
		add("poc1_delta_not_always_zero", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.PicOrderCntType = 1
		sps.DeltaPicOrderAlwaysZero = true
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:    SliceP,
			PPSID:        pps.ID,
			FrameNum:     3,
			SliceQPDelta: 0,
		}
		add("poc1_delta_always_zero", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.PicOrderCntType = 2
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:    SliceP,
			PPSID:        pps.ID,
			FrameNum:     3,
			SliceQPDelta: 0,
		}
		add("poc2", sps, pps, h)
	}

	for _, cfg := range []struct {
		name        string
		fieldPic    bool
		bottomField bool
	}{
		{"field_false", false, false},
		{"field_true_top", true, false},
		{"field_true_bottom", true, true},
	} {
		sps := baseSPS()
		sps.FrameMbsOnly = false
		sps.MBAdaptiveFrameField = false
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:    SliceP,
			PPSID:        pps.ID,
			FrameNum:     1,
			FieldPic:     cfg.fieldPic,
			BottomField:  cfg.bottomField,
			SliceQPDelta: 0,
		}
		add("mbsonly_false_"+cfg.name, sps, pps, h)
	}

	{
		sps := baseSPS()
		sps.ChromaFormatIDC = Chroma444
		sps.ProfileIDC = 244
		sps.SeparateColourPlane = true
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:     SliceP,
			PPSID:         pps.ID,
			ColourPlaneID: 2,
			FrameNum:      1,
			SliceQPDelta:  0,
		}
		add("separate_colour_plane", sps, pps, h)
	}

	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		pps.RedundantPicCntPresent = true
		h := &SliceHeader{
			SliceType:       SliceP,
			PPSID:           pps.ID,
			FrameNum:        1,
			RedundantPicCnt: 3,
			SliceQPDelta:    0,
		}
		add("redundant_pic_cnt", sps, pps, h)
	}

	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		pps.NumRefIdxL0DefaultActiveMinus1 = 2
		pps.NumRefIdxL1DefaultActiveMinus1 = 1
		h := &SliceHeader{
			SliceType:               SliceB,
			PPSID:                   pps.ID,
			FrameNum:                1,
			NumRefIdxActiveOverride: false,
			NumRefIdxL0ActiveMinus1: pps.NumRefIdxL0DefaultActiveMinus1,
			NumRefIdxL1ActiveMinus1: pps.NumRefIdxL1DefaultActiveMinus1,
			SliceQPDelta:            0,
		}
		add("num_ref_idx_no_override", sps, pps, h)
	}
	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		pps.NumRefIdxL0DefaultActiveMinus1 = 2
		pps.NumRefIdxL1DefaultActiveMinus1 = 1
		h := &SliceHeader{
			SliceType:               SliceB,
			PPSID:                   pps.ID,
			FrameNum:                1,
			NumRefIdxActiveOverride: true,
			NumRefIdxL0ActiveMinus1: 5,
			NumRefIdxL1ActiveMinus1: 4,
			SliceQPDelta:            0,
		}
		add("num_ref_idx_override", sps, pps, h)
	}

	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:             SliceB,
			PPSID:                 pps.ID,
			FrameNum:              1,
			ModificationL0Present: true,
			RefPicListModificationL0: []RefPicListModification{
				{IDC: 0, Value: 1},
				{IDC: 1, Value: 2},
				{IDC: 2, Value: 3},
			},
			ModificationL1Present: true,
			RefPicListModificationL1: []RefPicListModification{
				{IDC: 2, Value: 9},
				{IDC: 0, Value: 0},
			},
			SliceQPDelta: 0,
		}
		add("ref_pic_list_modification_present", sps, pps, h)
	}
	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:    SliceB,
			PPSID:        pps.ID,
			FrameNum:     1,
			SliceQPDelta: 0,
		}
		add("ref_pic_list_modification_absent", sps, pps, h)
	}

	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:             SliceP,
			PPSID:                 pps.ID,
			FrameNum:              1,
			NalRefIDC:             1,
			AdaptiveRefPicMarking: false,
			SliceQPDelta:          0,
		}
		add("dec_ref_pic_marking_non_idr_no_adaptive", sps, pps, h)
	}
	{
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:             SliceP,
			PPSID:                 pps.ID,
			FrameNum:              1,
			NalRefIDC:             1,
			AdaptiveRefPicMarking: true,
			MMCOs: []MMCO{
				{Op: 1, DifferenceOfPicNumsMinus1: 4},
				{Op: 2, LongTermPicNum: 3},
				{Op: 3, DifferenceOfPicNumsMinus1: 2, LongTermFrameIdx: 1},
				{Op: 4, MaxLongTermFrameIdxPlus1: 6},
				{Op: 5},
				{Op: 6, LongTermFrameIdx: 2},
			},
			SliceQPDelta: 0,
		}
		add("dec_ref_pic_marking_non_idr_adaptive_all_ops", sps, pps, h)
	}

	{
		sps := baseSPS()
		sps.ChromaFormatIDC = ChromaMonochrome
		sps.ProfileIDC = 100
		pps := basePPS(sps.ID)
		pps.WeightedPred = true
		h := &SliceHeader{
			SliceType:               SliceP,
			PPSID:                   pps.ID,
			FrameNum:                1,
			NumRefIdxActiveOverride: true,
			NumRefIdxL0ActiveMinus1: 1,
			PredWeight: PredWeightTable{
				LumaLog2WeightDenom: 5,
				L0: []WeightEntry{
					{LumaWeightFlag: true, LumaWeight: 10, LumaOffset: -2},
					{LumaWeightFlag: false, LumaWeight: 1 << 5},
				},
			},
			SliceQPDelta: 0,
		}
		add("pred_weight_p_monochrome", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.ChromaFormatIDC = Chroma420
		pps := basePPS(sps.ID)
		pps.WeightedPred = true
		h := &SliceHeader{
			SliceType:               SliceP,
			PPSID:                   pps.ID,
			FrameNum:                1,
			NumRefIdxActiveOverride: true,
			NumRefIdxL0ActiveMinus1: 1,
			PredWeight: PredWeightTable{
				LumaLog2WeightDenom:   3,
				ChromaLog2WeightDenom: 3,
				L0: []WeightEntry{
					{
						LumaWeightFlag:   true,
						LumaWeight:       9,
						LumaOffset:       1,
						ChromaWeightFlag: true,
						ChromaWeight:     [2]int32{7, 6},
						ChromaOffset:     [2]int32{-1, 2},
					},
					{
						LumaWeightFlag:   false,
						LumaWeight:       1 << 3,
						ChromaWeightFlag: false,
						ChromaWeight:     [2]int32{1 << 3, 1 << 3},
					},
				},
			},
			SliceQPDelta: 0,
		}
		add("pred_weight_p_chroma420", sps, pps, h)
	}
	{
		sps := baseSPS()
		sps.ChromaFormatIDC = Chroma420
		pps := basePPS(sps.ID)
		pps.WeightedBipredIDC = 1
		h := &SliceHeader{
			SliceType:               SliceB,
			PPSID:                   pps.ID,
			FrameNum:                1,
			NumRefIdxActiveOverride: true,
			NumRefIdxL0ActiveMinus1: 0,
			NumRefIdxL1ActiveMinus1: 0,
			PredWeight: PredWeightTable{
				LumaLog2WeightDenom:   2,
				ChromaLog2WeightDenom: 2,
				L0: []WeightEntry{
					{
						LumaWeightFlag:   false,
						LumaWeight:       1 << 2,
						ChromaWeightFlag: false,
						ChromaWeight:     [2]int32{1 << 2, 1 << 2},
					},
				},
				L1: []WeightEntry{
					{
						LumaWeightFlag:   true,
						LumaWeight:       3,
						LumaOffset:       0,
						ChromaWeightFlag: true,
						ChromaWeight:     [2]int32{2, 5},
						ChromaOffset:     [2]int32{0, -3},
					},
				},
			},
			SliceQPDelta: 0,
		}
		add("pred_weight_b_chroma420", sps, pps, h)
	}

	for _, idc := range []uint32{0, 1, 2} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		pps.CABAC = true
		h := &SliceHeader{
			SliceType:    SliceP,
			PPSID:        pps.ID,
			FrameNum:     1,
			CABACInitIDC: idc,
			SliceQPDelta: 1,
		}
		add("cabac_init_idc", sps, pps, h)
	}

	for _, cfg := range []struct {
		name  string
		idc   uint32
		alpha int32
		beta  int32
	}{
		{"idc0_extremes_low_high", 0, -6, 6},
		{"idc1_no_offsets", 1, 0, 0},
		{"idc2_extremes_high_low", 2, 6, -6},
	} {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		pps.DeblockingFilterControlPresent = true
		h := &SliceHeader{
			SliceType:                  SliceP,
			PPSID:                      pps.ID,
			FrameNum:                   1,
			DisableDeblockingFilterIDC: cfg.idc,
			SliceAlphaC0OffsetDiv2:     cfg.alpha,
			SliceBetaOffsetDiv2:        cfg.beta,
			SliceQPDelta:               0,
		}
		add("deblocking_"+cfg.name, sps, pps, h)
	}

	return out
}

func TestSliceHeaderRoundTripMatrix(t *testing.T) {
	for _, sc := range buildSliceScenarios() {
		t.Run(sc.name, func(t *testing.T) {
			b1 := sliceHeaderBytes(t, sc.h, sc.sps, sc.pps)
			sets := newFakeParams().addSPS(sc.sps).addPPS(sc.pps)
			r := bits.NewReader(b1)
			parsed, gotSPS, gotPPS, err := ParseSliceHeader(r, nalHeaderFor(sc.h), sets)
			if err != nil {
				t.Fatalf("ParseSliceHeader: %v", err)
			}
			if gotSPS != sc.sps {
				t.Errorf("returned SPS pointer mismatch")
			}
			if gotPPS != sc.pps {
				t.Errorf("returned PPS pointer mismatch")
			}
			if !reflect.DeepEqual(sc.h, parsed) {
				t.Fatalf("struct mismatch\n orig: %+v\n parsed: %+v", sc.h, parsed)
			}
			b2 := sliceHeaderBytes(t, parsed, sc.sps, sc.pps)
			bytesEqual(t, sc.name, b1, b2)
		})
	}
}

func TestPredWeightTableDefaults(t *testing.T) {
	for d := uint32(0); d <= 7; d++ {
		lumaDenom := d
		chromaDenom := 7 - d
		t.Run(fmt.Sprintf("luma_denom_%d_chroma_denom_%d", lumaDenom, chromaDenom), func(t *testing.T) {
			sps := baseSPS()
			sps.ChromaFormatIDC = Chroma420
			pps := basePPS(sps.ID)
			pps.WeightedPred = true
			h := &SliceHeader{
				SliceType:               SliceP,
				PPSID:                   pps.ID,
				FrameNum:                1,
				NumRefIdxActiveOverride: true,
				NumRefIdxL0ActiveMinus1: 0,
				PredWeight: PredWeightTable{
					LumaLog2WeightDenom:   lumaDenom,
					ChromaLog2WeightDenom: chromaDenom,
					L0:                    []WeightEntry{{}},
				},
				SliceQPDelta: 0,
			}

			parsed, err := roundtripSliceHeader(t, h, sps, pps)
			if err != nil {
				t.Fatalf("roundtrip: %v", err)
			}
			if len(parsed.PredWeight.L0) != 1 {
				t.Fatalf("L0 length = %d, want 1", len(parsed.PredWeight.L0))
			}

			wantLuma := int32(1) << lumaDenom
			wantChroma := int32(1) << chromaDenom
			e := parsed.PredWeight.L0[0]
			if e.LumaWeightFlag {
				t.Fatalf("LumaWeightFlag = true, want false")
			}
			if e.LumaWeight != wantLuma {
				t.Fatalf("LumaWeight = %d, want %d (1<<%d)", e.LumaWeight, wantLuma, lumaDenom)
			}
			if e.ChromaWeightFlag {
				t.Fatalf("ChromaWeightFlag = true, want false")
			}
			if e.ChromaWeight != [2]int32{wantChroma, wantChroma} {
				t.Fatalf("ChromaWeight = %v, want [%d %d] (1<<%d)", e.ChromaWeight, wantChroma, wantChroma, chromaDenom)
			}
		})
	}
}

func TestSliceHeaderValidationRejects(t *testing.T) {
	baseline := func() (*SPS, *PPS, *SliceHeader) {
		sps := baseSPS()
		pps := basePPS(sps.ID)
		h := &SliceHeader{
			SliceType:    SliceP,
			PPSID:        pps.ID,
			FrameNum:     1,
			SliceQPDelta: 0,
		}
		return sps, pps, h
	}

	writeAndParse := func(t *testing.T, sps *SPS, pps *PPS, h *SliceHeader, sets ParameterSets) error {
		t.Helper()
		w := bits.NewWriter()
		if err := WriteSliceHeader(w, h, sps, pps); err != nil {
			t.Fatalf("WriteSliceHeader unexpectedly failed: %v", err)
		}
		w.WriteRBSPTrailingBits()
		if err := w.Err(); err != nil {
			t.Fatalf("writer error building fixture: %v", err)
		}
		r := bits.NewReader(w.Bytes())
		_, _, _, err := ParseSliceHeader(r, nalHeaderFor(h), sets)
		return err
	}

	t.Run("slice_type_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		h.SliceType = SliceType(10)
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("missing_pps", func(t *testing.T) {
		sps, pps, h := baseline()
		sets := newFakeParams().addSPS(sps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrMissingPPS) {
			t.Fatalf("got %v, want ErrMissingPPS", err)
		}
	})

	t.Run("missing_sps", func(t *testing.T) {
		sps, pps, h := baseline()
		sets := newFakeParams().addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrMissingSPS) {
			t.Fatalf("got %v, want ErrMissingSPS", err)
		}
	})

	t.Run("non_intra_slice_in_idr", func(t *testing.T) {
		sps, pps, h := baseline()
		h.IDR = true
		h.SliceType = SliceP
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("idr_pic_id_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		h.IDR = true
		h.SliceType = SliceI
		h.IDRPicID = 65536
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("num_ref_idx_active_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		h.NumRefIdxActiveOverride = true
		h.NumRefIdxL0ActiveMinus1 = 32
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("cabac_init_idc_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.CABAC = true
		h.CABACInitIDC = 3
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("slice_qpy_too_low", func(t *testing.T) {
		sps, pps, h := baseline()
		h.SliceQPDelta = -27
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("slice_qpy_too_high", func(t *testing.T) {
		sps, pps, h := baseline()
		h.SliceQPDelta = 52
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("disable_deblocking_filter_idc_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.DeblockingFilterControlPresent = true
		h.DisableDeblockingFilterIDC = 3
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("deblocking_alpha_offset_out_of_range", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.DeblockingFilterControlPresent = true
		h.DisableDeblockingFilterIDC = 0
		h.SliceAlphaC0OffsetDiv2 = 7
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("deblocking_beta_offset_out_of_range", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.DeblockingFilterControlPresent = true
		h.DisableDeblockingFilterIDC = 0
		h.SliceBetaOffsetDiv2 = -7
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("modification_of_pic_nums_idc_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		h.SliceType = SliceB
		h.ModificationL0Present = true
		h.RefPicListModificationL0 = []RefPicListModification{{IDC: 4, Value: 0}}
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("mmco_op_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		h.NalRefIDC = 1
		h.AdaptiveRefPicMarking = true
		h.MMCOs = []MMCO{{Op: 7}}
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("luma_log2_weight_denom_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.WeightedPred = true
		h.PredWeight.LumaLog2WeightDenom = 8
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("chroma_log2_weight_denom_too_large", func(t *testing.T) {
		sps, pps, h := baseline()
		pps.WeightedPred = true
		h.PredWeight.ChromaLog2WeightDenom = 8
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		err := writeAndParse(t, sps, pps, h, sets)
		if !errors.Is(err, ErrInvalidValue) {
			t.Fatalf("got %v, want ErrInvalidValue", err)
		}
	})

	t.Run("unterminated_ref_list_modification_errors_not_hangs", func(t *testing.T) {
		sps, pps, _ := baseline()
		w := bits.NewWriter()
		w.WriteUE(0)
		w.WriteUE(uint32(SliceB))
		w.WriteUE(pps.ID)
		w.WriteBits(0, int(sps.Log2MaxFrameNumMinus4)+4)
		w.WriteBits(0, int(sps.Log2MaxPicOrderCntLsbMinus4)+4)
		w.WriteFlag(false)
		w.WriteFlag(false)
		w.WriteFlag(true)
		for i := 0; i < 40; i++ {
			w.WriteBit(1)
		}
		if err := w.Err(); err != nil {
			t.Fatalf("writer error building fixture: %v", err)
		}
		r := bits.NewReader(w.Bytes())
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		_, _, _, err := ParseSliceHeader(r, nal.Header{Type: nal.TypeSliceNonIDR}, sets)
		if err == nil {
			t.Fatalf("expected an error, got nil")
		}
	})

	t.Run("unterminated_mmco_loop_errors_not_hangs", func(t *testing.T) {
		sps, pps, _ := baseline()
		w := bits.NewWriter()
		w.WriteUE(0)
		w.WriteUE(uint32(SliceP))
		w.WriteUE(pps.ID)
		w.WriteBits(0, int(sps.Log2MaxFrameNumMinus4)+4)
		w.WriteBits(0, int(sps.Log2MaxPicOrderCntLsbMinus4)+4)
		w.WriteFlag(false)
		w.WriteFlag(false)
		w.WriteFlag(true)
		for i := 0; i < 20; i++ {
			w.WriteUE(5)
		}
		if err := w.Err(); err != nil {
			t.Fatalf("writer error building fixture: %v", err)
		}
		r := bits.NewReader(w.Bytes())
		sets := newFakeParams().addSPS(sps).addPPS(pps)
		_, _, _, err := ParseSliceHeader(r, nal.Header{RefIDC: 1, Type: nal.TypeSliceNonIDR}, sets)
		if err == nil {
			t.Fatalf("expected an error, got nil")
		}
	})
}
