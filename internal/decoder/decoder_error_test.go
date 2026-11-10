package decoder

import (
	"errors"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/nal"
	"github.com/go-avkit/h264/internal/syntax"
	"github.com/go-avkit/h264/internal/testutil"
)

func minimalSPS() *syntax.SPS {
	return &syntax.SPS{
		ProfileIDC:                77,
		LevelIDC:                  30,
		ID:                        0,
		ChromaFormatIDC:           syntax.Chroma420,
		Log2MaxFrameNumMinus4:     0,
		PicOrderCntType:           2,
		MaxNumRefFrames:           1,
		PicWidthInMbsMinus1:       1,
		PicHeightInMapUnitsMinus1: 1,
		FrameMbsOnly:              true,
		Direct8x8Inference:        true,
	}
}

func minimalPPS(spsID uint32, cabac bool) *syntax.PPS {
	return &syntax.PPS{ID: 0, SPSID: spsID, CABAC: cabac}
}

func lookupOne(sps *syntax.SPS) func(uint32) *syntax.SPS {
	return func(id uint32) *syntax.SPS {
		if id == sps.ID {
			return sps
		}
		return nil
	}
}

func mustWriteSPSBytes(t *testing.T, s *syntax.SPS) []byte {
	t.Helper()
	b, err := syntax.WriteSPS(s)
	if err != nil {
		t.Fatalf("WriteSPS: %v", err)
	}
	return b
}

func mustWritePPSBytes(t *testing.T, p *syntax.PPS, lookup func(uint32) *syntax.SPS) []byte {
	t.Helper()
	b, err := syntax.WritePPS(p, lookup)
	if err != nil {
		t.Fatalf("WritePPS: %v", err)
	}
	return b
}

func mustSliceBytes(t *testing.T, h *syntax.SliceHeader, sps *syntax.SPS, pps *syntax.PPS) []byte {
	t.Helper()
	w := bits.NewWriter()
	if err := syntax.WriteSliceHeader(w, h, sps, pps); err != nil {
		t.Fatalf("WriteSliceHeader: %v", err)
	}
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("writer: %v", err)
	}
	return w.Bytes()
}

func annexBUnit(typ nal.Type, refIDC uint8, rbsp []byte) []byte {
	return nal.AppendAnnexB(nil, nal.Unit{Header: nal.Header{RefIDC: refIDC, Type: typ}, RBSP: rbsp}, true)
}

func decodeWithFlush(d *Decoder, data []byte) ([]*frame.Picture, error) {
	pics, err := d.Decode(data)
	if err != nil {
		return pics, err
	}
	rest, err := d.Flush()
	return append(pics, rest...), err
}

func TestCheckSupportedRejections(t *testing.T) {
	base := func() *syntax.SPS {
		return &syntax.SPS{ChromaFormatIDC: syntax.Chroma420, FrameMbsOnly: true, LevelIDC: 30}
	}

	cases := []struct {
		name string
		mod  func(*syntax.SPS)
	}{
		{"chroma format not 4:2:0", func(s *syntax.SPS) { s.ChromaFormatIDC = syntax.Chroma422 }},
		{"luma bit depth above 8", func(s *syntax.SPS) { s.BitDepthLumaMinus8 = 2 }},
		{"chroma bit depth above 8", func(s *syntax.SPS) { s.BitDepthChromaMinus8 = 2 }},
		{"field or MBAFF coding", func(s *syntax.SPS) { s.FrameMbsOnly = false }},
		{"lossless transform bypass", func(s *syntax.SPS) { s.QpprimeYZeroTransformBypass = true }},
	}

	d := New()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sps := base()
			c.mod(sps)
			err := d.checkSupported(sps)
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("checkSupported() = %v, want ErrUnsupported", err)
			}
		})
	}

	t.Run("fully supported SPS passes", func(t *testing.T) {
		if err := d.checkSupported(base()); err != nil {
			t.Fatalf("checkSupported() = %v, want nil", err)
		}
	})

	t.Run("sequence scaling matrices are accepted", func(t *testing.T) {
		sps := base()
		sps.ProfileIDC = syntax.ProfileHigh
		sps.SeqScalingMatrixPresent = true
		if err := d.checkSupported(sps); err != nil {
			t.Fatalf("checkSupported() = %v, want nil", err)
		}
		tables := buildScalingTables(sps, &syntax.PPS{Transform8x8Mode: true})
		if tables.flat {
			t.Fatal("a sequence scaling matrix must reach dequantisation as non-flat weights")
		}
	})
}

func TestDecodeCABACPPSIsAccepted(t *testing.T) {
	sps := minimalSPS()
	spsBytes := mustWriteSPSBytes(t, sps)
	pps := minimalPPS(sps.ID, true)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)

	d := New()
	if _, err := decodeWithFlush(d, data); err != nil {
		t.Fatalf("Decode() = %v, want a CABAC parameter set to be accepted", err)
	}
	if got := d.PPS(pps.ID); got == nil || !got.CABAC {
		t.Fatal("the CABAC parameter set was not stored")
	}
}

func TestDecodeSliceReferencingAbsentPPS(t *testing.T) {
	sps := minimalSPS()
	spsBytes := mustWriteSPSBytes(t, sps)
	pps := minimalPPS(sps.ID, false)

	hdr := &syntax.SliceHeader{
		SliceType: syntax.SliceI,
		PPSID:     pps.ID,
		IDR:       true,
		NalRefIDC: 1,
	}
	sliceRBSP := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 1, sliceRBSP)...)

	d := New()
	_, err := decodeWithFlush(d, data)
	if !errors.Is(err, syntax.ErrMissingPPS) {
		t.Fatalf("Decode() = %v, want syntax.ErrMissingPPS", err)
	}
}

func TestDecodeFirstSliceIsPWithNoReference(t *testing.T) {
	sps := minimalSPS()
	pps := minimalPPS(sps.ID, false)
	spsBytes := mustWriteSPSBytes(t, sps)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	hdr := &syntax.SliceHeader{SliceType: syntax.SliceP, PPSID: pps.ID, NalRefIDC: 1}
	sliceRBSP := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceNonIDR, 1, sliceRBSP)...)

	d := New()
	_, err := decodeWithFlush(d, data)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Decode() = %v, want ErrCorrupt (P slice with an empty reference list)", err)
	}
}

func TestDecodeMalformedNALHeaderPropagatesError(t *testing.T) {
	data := []byte{0, 0, 1, 0x80, 0x00, 0, 0, 1}

	d := New()
	_, err := d.Decode(data)
	if !errors.Is(err, nal.ErrForbiddenBit) {
		t.Fatalf("Decode() = %v, want nal.ErrForbiddenBit", err)
	}
}

func TestDecodeUnsupportedSPSPropagatesError(t *testing.T) {
	sps := minimalSPS()
	sps.FrameMbsOnly = false // field/MBAFF coding: rejected by checkSupported
	spsBytes := mustWriteSPSBytes(t, sps)

	d := New()
	_, err := decodeWithFlush(d, annexBUnit(nal.TypeSPS, 3, spsBytes))
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Decode() = %v, want ErrUnsupported", err)
	}
}

func TestDecodeMalformedPPSPropagatesParseError(t *testing.T) {
	var data []byte
	data = append(data, annexBUnit(nal.TypePPS, 3, nil)...)
	data = append(data, annexBUnit(nal.TypeAccessUnitDelim, 3, []byte{0xF0})...)

	d := New()
	_, err := d.Decode(data)
	if err == nil {
		t.Fatalf("Decode() = nil, want a parse error from the malformed PPS")
	}
}

func TestDecodeSlicePicScalingMatrixIsAccepted(t *testing.T) {
	sps := minimalSPS()
	pps := minimalPPS(sps.ID, false)
	pps.HasExtension = true
	pps.PicScalingMatrixPresent = true
	spsBytes := mustWriteSPSBytes(t, sps)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	hdr := &syntax.SliceHeader{SliceType: syntax.SliceI, PPSID: pps.ID, IDR: true, NalRefIDC: 1}
	sliceRBSP := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 1, sliceRBSP)...)

	d := New()
	_, err := decodeWithFlush(d, data)
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("Decode() = %v, want picture scaling matrices to be understood", err)
	}
	tables := buildScalingTables(sps, d.PPS(pps.ID))
	if tables.flat {
		t.Fatal("a picture scaling matrix must reach dequantisation as non-flat weights")
	}
}

func TestDecodeWeightedPredIsAccepted(t *testing.T) {
	sps := minimalSPS()
	pps := minimalPPS(sps.ID, false)
	pps.WeightedPred = true
	spsBytes := mustWriteSPSBytes(t, sps)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	hdr := &syntax.SliceHeader{SliceType: syntax.SliceP, PPSID: pps.ID, NalRefIDC: 1}
	hdr.PredWeight.L0 = make([]syntax.WeightEntry, 1)
	sliceRBSP := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceNonIDR, 1, sliceRBSP)...)

	d := New()
	_, err := decodeWithFlush(d, data)
	if errors.Is(err, ErrUnsupported) {
		t.Fatalf("Decode() = %v, want weighted prediction to be understood", err)
	}
}

func TestDecodeContinuationSliceWithNoCurrentPictureErrors(t *testing.T) {
	sps := minimalSPS()
	pps := minimalPPS(sps.ID, false)
	spsBytes := mustWriteSPSBytes(t, sps)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	hdr := &syntax.SliceHeader{FirstMBInSlice: 5, SliceType: syntax.SliceI, PPSID: pps.ID, IDR: true, NalRefIDC: 1}
	sliceRBSP := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 1, sliceRBSP)...)

	d := New()
	_, err := decodeWithFlush(d, data)
	if !errors.Is(err, ErrNoParameters) {
		t.Fatalf("Decode() = %v, want ErrNoParameters", err)
	}
}

func TestRefPictureOutOfRangeAndEmpty(t *testing.T) {
	sd := &sliceDecoder{}
	if p := sd.refPictureIn(0, 0); p != nil {
		t.Fatalf("empty reference list: want nil, got %v", p)
	}

	p0 := frame.NewPicture(1, 1)
	p1 := frame.NewPicture(1, 1)
	sd.refList = []*frame.Picture{p0, p1}

	if got := sd.refPictureIn(0, 5); got != p0 {
		t.Fatalf("out-of-range index: want fallback to refList[0]")
	}
	if got := sd.refPictureIn(0, -1); got != p0 {
		t.Fatalf("negative index: want fallback to refList[0]")
	}
	if got := sd.refPictureIn(0, 1); got != p1 {
		t.Fatalf("in-range index: want refList[1]")
	}
}

func TestReadRefIdxRejectsOutOfRange(t *testing.T) {
	w := bits.NewWriter()
	w.WriteUE(5) // ref_idx_l0 = 5, but max_minus1 = 2
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("writer: %v", err)
	}
	sd := &sliceDecoder{r: bits.NewReader(w.Bytes())}

	_, err := sd.readRefIdx(2)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("readRefIdx() = %v, want ErrCorrupt", err)
	}
}

func TestReadMVDRejectsOutOfRange(t *testing.T) {
	cases := []struct {
		name string
		x, y int32
	}{
		{"x too large", 40000, 0},
		{"y too small", 0, -40000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := bits.NewWriter()
			w.WriteSE(c.x)
			w.WriteSE(c.y)
			w.WriteRBSPTrailingBits()
			if err := w.Err(); err != nil {
				t.Fatalf("writer: %v", err)
			}
			sd := &sliceDecoder{r: bits.NewReader(w.Bytes())}

			_, err := sd.readMVD()
			if !errors.Is(err, ErrCorrupt) {
				t.Fatalf("readMVD() = %v, want ErrCorrupt", err)
			}
		})
	}
}

func TestBumpEmitsInPictureOrder(t *testing.T) {
	d := New()
	d.maxReorder = 2
	for _, poc := range []int{4, 0, 2, 6, 8} {
		d.pending = append(d.pending, &frame.Picture{POC: poc})
	}
	var got []int
	for _, p := range d.bump(false) {
		got = append(got, p.POC)
	}
	want := []int{0, 2, 4}
	if len(got) != len(want) {
		t.Fatalf("bump released %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("bump released %v, want %v", got, want)
		}
	}
	for _, p := range d.bump(true) {
		got = append(got, p.POC)
	}
	if len(got) != 5 || got[3] != 6 || got[4] != 8 {
		t.Fatalf("draining released %v", got)
	}
	if len(d.pending) != 0 {
		t.Fatalf("%d pictures still held after draining", len(d.pending))
	}
}

func pcmSamples(seed byte) []byte {
	out := make([]byte, 384)
	for i := range out {
		out[i] = byte(int(seed)*37 + i*11)
	}
	return out
}

func TestDecodeIPCMMacroblocks(t *testing.T) {
	sps := minimalSPS()
	pps := minimalPPS(sps.ID, false)
	spsBytes := mustWriteSPSBytes(t, sps)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))

	hdr := &syntax.SliceHeader{SliceType: syntax.SliceI, PPSID: pps.ID, IDR: true, NalRefIDC: 1}
	w := bits.NewWriter()
	if err := syntax.WriteSliceHeader(w, hdr, sps, pps); err != nil {
		t.Fatalf("WriteSliceHeader: %v", err)
	}
	samples := make([][]byte, 4)
	for mb := 0; mb < 4; mb++ {
		w.WriteUE(25)
		w.AlignZero()
		samples[mb] = pcmSamples(byte(mb))
		for _, v := range samples[mb] {
			w.WriteBits(uint32(v), 8)
		}
	}
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("writer: %v", err)
	}

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 1, w.Bytes())...)

	d := New()
	pics, err := decodeWithFlush(d, data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(pics) != 1 {
		t.Fatalf("decoded %d pictures, want 1", len(pics))
	}
	pic := pics[0]
	for mb := 0; mb < 4; mb++ {
		mbx, mby := mb%2, mb/2
		want := samples[mb]
		for y := 0; y < 16; y++ {
			for x := 0; x < 16; x++ {
				got := pic.Y[pic.LumaOffset(mbx*16+x, mby*16+y)]
				if got != want[y*16+x] {
					t.Fatalf("macroblock %d luma (%d,%d) = %d, want %d", mb, x, y, got, want[y*16+x])
				}
			}
		}
		for plane, buf := range [][]byte{pic.Cb, pic.Cr} {
			base := 256 + plane*64
			for y := 0; y < 8; y++ {
				for x := 0; x < 8; x++ {
					got := buf[pic.ChromaOffset(mbx*8+x, mby*8+y)]
					if got != want[base+y*8+x] {
						t.Fatalf("macroblock %d chroma %d (%d,%d) = %d, want %d",
							mb, plane, x, y, got, want[base+y*8+x])
					}
				}
			}
		}
	}
}

func TestRepeatedParameterSetsDoNotResetTheBuffer(t *testing.T) {
	for _, c := range append(append([]testutil.Clip{}, testutil.Corpus...), testutil.MainCorpus...) {
		if c.Frames < 3 {
			continue
		}
		stream := testutil.LoadStream(t, c)
		units := nal.SplitAnnexB(stream)

		var headers [][]byte
		var rest [][]byte
		for _, ebsp := range units {
			u, err := nal.Parse(ebsp)
			if err != nil {
				t.Fatalf("%s: %v", c.Name, err)
			}
			if u.Type == nal.TypeSPS || u.Type == nal.TypePPS {
				headers = append(headers, ebsp)
				continue
			}
			rest = append(rest, ebsp)
		}
		if len(headers) == 0 {
			continue
		}

		var repeated []byte
		for _, ebsp := range rest {
			u, err := nal.Parse(ebsp)
			if err != nil {
				t.Fatal(err)
			}
			if u.Type == nal.TypeSliceIDR || u.Type == nal.TypeSliceNonIDR {
				for _, h := range headers {
					repeated = append(repeated, annexBPrefix...)
					repeated = append(repeated, h...)
				}
			}
			repeated = append(repeated, annexBPrefix...)
			repeated = append(repeated, ebsp...)
		}

		want := decodeAll(t, stream)
		got := decodeAll(t, repeated)
		if len(got) != len(want) {
			t.Fatalf("%s: repeating the parameter sets gave %d pictures against %d",
				c.Name, len(got), len(want))
		}
		for i := range want {
			a := make([]byte, want[i].Size())
			want[i].CopyOut(a)
			b := make([]byte, got[i].Size())
			got[i].CopyOut(b)
			for j := range a {
				if a[j] != b[j] {
					t.Fatalf("%s: repeating the parameter sets changed picture %d at sample %d, %d against %d",
						c.Name, i, j, b[j], a[j])
				}
			}
		}
	}
}

var annexBPrefix = []byte{0, 0, 1}
