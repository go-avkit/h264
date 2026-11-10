package syntax

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

func TestChromaQPTable(t *testing.T) {
	want := map[int]int{}
	for qp := 0; qp < 30; qp++ {
		want[qp] = qp
	}
	tail := []int{29, 30, 31, 32, 32, 33, 34, 34, 35, 35, 36, 36, 37, 37, 37, 38, 38, 38, 39, 39, 39, 39}
	for i, v := range tail {
		want[30+i] = v
	}
	if len(want) != 52 {
		t.Fatalf("test setup error: want table has %d entries", len(want))
	}
	for qp := 0; qp <= 51; qp++ {
		got := ChromaQP(qp, 0)
		if got != want[qp] {
			t.Errorf("ChromaQP(%d, 0) = %d, want %d", qp, got, want[qp])
		}
	}
}

func TestChromaQPOffsetClamping(t *testing.T) {
	if got := ChromaQP(0, -12); got != chromaQPTable[0] {
		t.Errorf("ChromaQP(0,-12) = %d, want %d (clamp to index 0)", got, chromaQPTable[0])
	}
	if got := ChromaQP(51, 12); got != chromaQPTable[51] {
		t.Errorf("ChromaQP(51,12) = %d, want %d (clamp to index 51)", got, chromaQPTable[51])
	}
	if got := ChromaQP(0, -1000); got != chromaQPTable[0] {
		t.Errorf("ChromaQP(0,-1000) = %d, want %d", got, chromaQPTable[0])
	}
	if got := ChromaQP(51, 1000); got != chromaQPTable[51] {
		t.Errorf("ChromaQP(51,1000) = %d, want %d", got, chromaQPTable[51])
	}
}

func TestPPSRoundTripMatrix(t *testing.T) {
	sps420 := baseSPS()
	sps420.ID = 0
	sps420.ChromaFormatIDC = Chroma420
	sps420.ProfileIDC = 100

	sps444 := baseSPS()
	sps444.ID = 1
	sps444.ChromaFormatIDC = Chroma444
	sps444.ProfileIDC = 244

	lookup := lookupSPSFunc(sps420, sps444)

	cases := []struct {
		name  string
		build func() *PPS
	}{
		{
			name: "cavlc_no_extension",
			build: func() *PPS {
				p := basePPS(0)
				p.CABAC = false
				return p
			},
		},
		{
			name: "cabac_no_extension",
			build: func() *PPS {
				p := basePPS(0)
				p.CABAC = true
				return p
			},
		},
		{
			name: "extension_no_transform8x8_no_scaling",
			build: func() *PPS {
				p := basePPS(0)
				p.HasExtension = true
				p.SecondChromaQPIndexOffset = 3
				return p
			},
		},
		{
			name: "extension_transform8x8_no_scaling",
			build: func() *PPS {
				p := basePPS(0)
				p.HasExtension = true
				p.Transform8x8Mode = true
				p.SecondChromaQPIndexOffset = -3
				return p
			},
		},
		{
			name: "extension_scaling_chroma420_partial_lists",
			build: func() *PPS {
				p := basePPS(0)
				p.HasExtension = true
				p.Transform8x8Mode = true
				p.PicScalingMatrixPresent = true
				p.ScalingList4x4Present[0] = true
				p.ScalingList4x4[0] = flatList4x4(2)
				p.ScalingList4x4Present[3] = true
				p.UseDefaultScaling4x4[3] = true
				p.ScalingList4x4[3] = defaultList4x4()
				p.ScalingList8x8Present[0] = true
				p.ScalingList8x8[0] = flatList8x8(1)
				p.SecondChromaQPIndexOffset = 5
				return p
			},
		},
		{
			name: "extension_scaling_chroma420_no_transform8x8",
			build: func() *PPS {
				p := basePPS(0)
				p.HasExtension = true
				p.Transform8x8Mode = false
				p.PicScalingMatrixPresent = true
				p.ScalingList4x4Present[1] = true
				p.ScalingList4x4[1] = flatList4x4(3)
				p.SecondChromaQPIndexOffset = 1
				return p
			},
		},
		{
			name: "extension_scaling_chroma444_all_lists",
			build: func() *PPS {
				p := basePPS(1)
				p.HasExtension = true
				p.Transform8x8Mode = true
				p.PicScalingMatrixPresent = true
				for i := 0; i < 6; i++ {
					p.ScalingList4x4Present[i] = true
					p.ScalingList4x4[i] = flatList4x4(uint8(i))
				}
				for i := 0; i < 6; i++ {
					p.ScalingList8x8Present[i] = true
					p.ScalingList8x8[i] = flatList8x8(uint8(i))
				}
				p.SecondChromaQPIndexOffset = -7
				return p
			},
		},
		{
			name: "weighted_pred_and_bipred",
			build: func() *PPS {
				p := basePPS(0)
				p.WeightedPred = true
				p.WeightedBipredIDC = 2
				p.NumRefIdxL0DefaultActiveMinus1 = 3
				p.NumRefIdxL1DefaultActiveMinus1 = 2
				return p
			},
		},
		{
			name: "qp_extremes_min",
			build: func() *PPS {
				p := basePPS(0)
				p.PicInitQPMinus26 = -26
				p.PicInitQSMinus26 = -26
				p.ChromaQPIndexOffset = -12
				p.SecondChromaQPIndexOffset = -12
				return p
			},
		},
		{
			name: "qp_extremes_max",
			build: func() *PPS {
				p := basePPS(0)
				p.PicInitQPMinus26 = 25
				p.PicInitQSMinus26 = 25
				p.ChromaQPIndexOffset = 12
				p.SecondChromaQPIndexOffset = 12
				return p
			},
		},
		{
			name: "deblocking_and_redundant_and_bottom_field",
			build: func() *PPS {
				p := basePPS(0)
				p.BottomFieldPicOrderInFramePresent = true
				p.DeblockingFilterControlPresent = true
				p.ConstrainedIntraPred = true
				p.RedundantPicCntPresent = true
				return p
			},
		},
		{
			name: "extension_second_chroma_qp_extremes",
			build: func() *PPS {
				p := basePPS(0)
				p.HasExtension = true
				p.SecondChromaQPIndexOffset = -12
				return p
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			orig := c.build()
			b1 := mustWritePPS(t, orig, lookup)
			parsed := mustParsePPS(t, b1, lookup)
			b2 := mustWritePPS(t, parsed, lookup)
			bytesEqual(t, c.name, b1, b2)
			if !reflect.DeepEqual(orig, parsed) {
				t.Fatalf("%s: struct mismatch\n orig: %+v\n parsed: %+v", c.name, orig, parsed)
			}
		})
	}
}

func TestPPSNoExtensionSecondChromaQPMirrorsFirst(t *testing.T) {
	sps := baseSPS()
	lookup := lookupSPSFunc(sps)
	p := basePPS(sps.ID)
	p.HasExtension = false
	p.ChromaQPIndexOffset = 7
	p.SecondChromaQPIndexOffset = -99

	b := mustWritePPS(t, p, lookup)
	parsed := mustParsePPS(t, b, lookup)
	if parsed.SecondChromaQPIndexOffset != parsed.ChromaQPIndexOffset {
		t.Fatalf("SecondChromaQPIndexOffset = %d, want equal to ChromaQPIndexOffset %d",
			parsed.SecondChromaQPIndexOffset, parsed.ChromaQPIndexOffset)
	}
	if parsed.SecondChromaQPIndexOffset != 7 {
		t.Fatalf("SecondChromaQPIndexOffset = %d, want 7", parsed.SecondChromaQPIndexOffset)
	}
}

func TestPPSUnsupportedSliceGroups(t *testing.T) {
	sps := baseSPS()
	lookup := lookupSPSFunc(sps)
	p := basePPS(sps.ID)
	p.NumSliceGroupsMinus1 = 1

	_, err := WritePPS(p, lookup)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("WritePPS: expected ErrUnsupported, got %v", err)
	}

	corrupted := buildPPSWithSliceGroups(1)
	_, err = ParsePPS(corrupted, lookup)
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("ParsePPS: expected ErrUnsupported, got %v", err)
	}
}

func buildPPSWithSliceGroups(numSliceGroupsMinus1 uint32) []byte {
	w := bits.NewWriter()
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteFlag(false)
	w.WriteFlag(false)
	w.WriteUE(numSliceGroupsMinus1)
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteFlag(false)
	w.WriteBits(0, 2)
	w.WriteSE(0)
	w.WriteSE(0)
	w.WriteSE(0)
	w.WriteFlag(false)
	w.WriteFlag(false)
	w.WriteFlag(false)
	w.WriteRBSPTrailingBits()
	return w.Bytes()
}

func TestPPSMissingSPSForScalingMatrix(t *testing.T) {
	p := basePPS(30)
	p.HasExtension = true
	p.PicScalingMatrixPresent = true
	nilLookup := func(uint32) *SPS { return nil }

	_, err := WritePPS(p, nilLookup)
	if !errors.Is(err, ErrMissingSPS) {
		t.Fatalf("WritePPS: expected ErrMissingSPS, got %v", err)
	}

	sps := baseSPS()
	sps.ID = 30
	lookup := lookupSPSFunc(sps)
	b := mustWritePPS(t, p, lookup)
	_, err = ParsePPS(b, nilLookup)
	if !errors.Is(err, ErrMissingSPS) {
		t.Fatalf("ParsePPS: expected ErrMissingSPS, got %v", err)
	}
}

func TestPPSValidationRejects(t *testing.T) {
	sps := baseSPS()
	lookup := lookupSPSFunc(sps)
	buildValid := func() *PPS { return basePPS(sps.ID) }

	tests := []struct {
		name   string
		mutate func(*PPS)
	}{
		{"pic_parameter_set_id_too_large", func(p *PPS) { p.ID = 256 }},
		{"seq_parameter_set_id_too_large", func(p *PPS) { p.SPSID = 32 }},
		{"num_ref_idx_l0_default_too_large", func(p *PPS) { p.NumRefIdxL0DefaultActiveMinus1 = 32 }},
		{"num_ref_idx_l1_default_too_large", func(p *PPS) { p.NumRefIdxL1DefaultActiveMinus1 = 32 }},
		{"pic_init_qp_too_low", func(p *PPS) { p.PicInitQPMinus26 = -27 }},
		{"pic_init_qp_too_high", func(p *PPS) { p.PicInitQPMinus26 = 26 }},
		{"chroma_qp_index_offset_too_low", func(p *PPS) { p.ChromaQPIndexOffset = -13 }},
		{"chroma_qp_index_offset_too_high", func(p *PPS) { p.ChromaQPIndexOffset = 13 }},
		{
			"second_chroma_qp_index_offset_too_low",
			func(p *PPS) {
				p.HasExtension = true
				p.SecondChromaQPIndexOffset = -13
			},
		},
		{
			"second_chroma_qp_index_offset_too_high",
			func(p *PPS) {
				p.HasExtension = true
				p.SecondChromaQPIndexOffset = 13
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := buildValid()
			tc.mutate(p)
			b, err := WritePPS(p, lookup)
			if err != nil {
				t.Fatalf("WritePPS unexpectedly failed to build fixture: %v", err)
			}
			_, err = ParsePPS(b, lookup)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("expected ErrInvalidValue, got %v", err)
			}
		})
	}
}

func TestPPSExtensionAbsenceIsIndistinguishableFromTruncation(t *testing.T) {
	sps := baseSPS()
	sps.ChromaFormatIDC = Chroma444
	lookup := lookupSPSFunc(sps)

	base := func() *PPS {
		p := basePPS(sps.ID)
		p.CABAC = true
		p.WeightedPred = true
		p.WeightedBipredIDC = 2
		p.PicInitQPMinus26 = 5
		p.PicInitQSMinus26 = -3
		p.ChromaQPIndexOffset = 7
		p.RedundantPicCntPresent = true
		return p
	}

	withExt := base()
	withExt.HasExtension = true
	withExt.Transform8x8Mode = true
	withExt.PicScalingMatrixPresent = true
	for i := 0; i < 6; i++ {
		withExt.ScalingList4x4Present[i] = true
		withExt.ScalingList4x4[i] = flatList4x4(uint8(i))
	}
	withExt.SecondChromaQPIndexOffset = 4

	withoutExt := base()
	withoutExt.HasExtension = false

	baseOnlyBytes, err := WritePPS(withoutExt, lookup)
	if err != nil {
		t.Fatalf("WritePPS(withoutExt): %v", err)
	}

	got, err := ParsePPS(baseOnlyBytes, lookup)
	if err != nil {
		t.Fatalf("ParsePPS(%x) unexpectedly failed: %v; a byte stream that ends exactly at the "+
			"base-PPS/extension boundary is a valid extension-less PPS per more_rbsp_data() semantics, "+
			"not a detectable truncation", baseOnlyBytes, err)
	}
	if got.HasExtension {
		t.Fatalf("HasExtension = true, want false for %x", baseOnlyBytes)
	}
	if got.SecondChromaQPIndexOffset != got.ChromaQPIndexOffset {
		t.Fatalf("SecondChromaQPIndexOffset = %d, want equal to ChromaQPIndexOffset %d",
			got.SecondChromaQPIndexOffset, got.ChromaQPIndexOffset)
	}

	if _, err := WritePPS(withExt, lookup); err != nil {
		t.Fatalf("WritePPS(withExt): %v", err)
	}
}

func TestPPSHasExtensionRoundTrips(t *testing.T) {
	sps := baseSPS()
	lookup := lookupSPSFunc(sps)

	noExt := basePPS(sps.ID)
	noExt.HasExtension = false
	b := mustWritePPS(t, noExt, lookup)
	parsed := mustParsePPS(t, b, lookup)
	if parsed.HasExtension {
		t.Fatalf("HasExtension = true, want false")
	}

	withExt := basePPS(sps.ID)
	withExt.HasExtension = true
	b = mustWritePPS(t, withExt, lookup)
	parsed = mustParsePPS(t, b, lookup)
	if !parsed.HasExtension {
		t.Fatalf("HasExtension = false, want true")
	}
}

func TestPPSPicInitQSMinus26RangeRejected(t *testing.T) {
	sps := baseSPS()
	sps.ChromaFormatIDC = Chroma444
	sps.ProfileIDC = 100
	lookup := lookupSPSFunc(sps)

	b := []byte("\xce\xce\x00\x00\x00\x01\x00\x00\x00\x010")

	_, err := ParsePPS(b, lookup)
	if !errors.Is(err, ErrInvalidValue) && !errors.Is(err, bits.ErrRange) {
		t.Fatalf("ParsePPS(% x) = %v, want ErrInvalidValue or bits.ErrRange", b, err)
	}

	for _, tc := range []struct {
		value    int32
		accepted bool
	}{
		{-26, true},
		{25, true},
		{-27, false},
		{26, false},
	} {
		p := basePPS(sps.ID)
		p.PicInitQSMinus26 = tc.value
		wb, werr := WritePPS(p, lookup)
		if werr != nil {
			t.Fatalf("WritePPS(PicInitQSMinus26=%d) unexpectedly failed: %v", tc.value, werr)
		}
		parsed, perr := ParsePPS(wb, lookup)
		if tc.accepted {
			if perr != nil {
				t.Fatalf("ParsePPS(PicInitQSMinus26=%d): unexpected error %v", tc.value, perr)
			}
			if parsed.PicInitQSMinus26 != tc.value {
				t.Fatalf("PicInitQSMinus26 = %d, want %d", parsed.PicInitQSMinus26, tc.value)
			}
		} else {
			if !errors.Is(perr, ErrInvalidValue) {
				t.Fatalf("ParsePPS(PicInitQSMinus26=%d) = %v, want error wrapping ErrInvalidValue", tc.value, perr)
			}
		}
	}
}

func TestPPSTruncationNeverPanicsOrReturnsNilBoth(t *testing.T) {
	sps := baseSPS()
	sps.ChromaFormatIDC = Chroma444
	lookup := lookupSPSFunc(sps)

	p := basePPS(sps.ID)
	p.CABAC = true
	p.BottomFieldPicOrderInFramePresent = true
	p.NumRefIdxL0DefaultActiveMinus1 = 3
	p.NumRefIdxL1DefaultActiveMinus1 = 2
	p.WeightedPred = true
	p.WeightedBipredIDC = 2
	p.PicInitQPMinus26 = 5
	p.PicInitQSMinus26 = -3
	p.ChromaQPIndexOffset = 7
	p.DeblockingFilterControlPresent = true
	p.ConstrainedIntraPred = true
	p.RedundantPicCntPresent = true
	p.HasExtension = true
	p.Transform8x8Mode = true
	p.PicScalingMatrixPresent = true
	for i := 0; i < 6; i++ {
		p.ScalingList4x4Present[i] = true
		p.ScalingList4x4[i] = flatList4x4(uint8(i))
	}
	for i := 0; i < 6; i++ {
		p.ScalingList8x8Present[i] = true
		p.ScalingList8x8[i] = flatList8x8(uint8(i))
	}
	p.SecondChromaQPIndexOffset = 4

	full, err := WritePPS(p, lookup)
	if err != nil {
		t.Fatalf("WritePPS: %v", err)
	}

	for n := 0; n < len(full); n++ {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("ParsePPS panicked on truncated input len=%d: %v", n, r)
				}
			}()
			got, err := ParsePPS(full[:n], lookup)
			if err == nil && got == nil {
				t.Fatalf("ParsePPS returned both a nil error and a nil PPS for truncated input len=%d", n)
			}
		}()
	}
}
