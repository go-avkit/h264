package syntax

import (
	"bytes"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/nal"
)

type fakeParams struct {
	sps map[uint32]*SPS
	pps map[uint32]*PPS
}

func newFakeParams() *fakeParams {
	return &fakeParams{sps: map[uint32]*SPS{}, pps: map[uint32]*PPS{}}
}

func (f *fakeParams) addSPS(s *SPS) *fakeParams {
	f.sps[s.ID] = s
	return f
}

func (f *fakeParams) addPPS(p *PPS) *fakeParams {
	f.pps[p.ID] = p
	return f
}

func (f *fakeParams) SPS(id uint32) *SPS {
	return f.sps[id]
}

func (f *fakeParams) PPS(id uint32) *PPS {
	return f.pps[id]
}

func lookupSPSFunc(sets ...*SPS) func(uint32) *SPS {
	m := map[uint32]*SPS{}
	for _, s := range sets {
		m[s.ID] = s
	}
	return func(id uint32) *SPS {
		if s, ok := m[id]; ok {
			return s
		}
		return nil
	}
}

func mustWriteSPS(t *testing.T, s *SPS) []byte {
	t.Helper()
	b, err := WriteSPS(s)
	if err != nil {
		t.Fatalf("WriteSPS: %v", err)
	}
	return b
}

func mustParseSPS(t *testing.T, b []byte) *SPS {
	t.Helper()
	s, err := ParseSPS(b)
	if err != nil {
		t.Fatalf("ParseSPS: %v", err)
	}
	return s
}

func mustWritePPS(t *testing.T, p *PPS, lookup func(uint32) *SPS) []byte {
	t.Helper()
	b, err := WritePPS(p, lookup)
	if err != nil {
		t.Fatalf("WritePPS: %v", err)
	}
	return b
}

func mustParsePPS(t *testing.T, b []byte, lookup func(uint32) *SPS) *PPS {
	t.Helper()
	p, err := ParsePPS(b, lookup)
	if err != nil {
		t.Fatalf("ParsePPS: %v", err)
	}
	return p
}

func sliceHeaderBytes(t *testing.T, h *SliceHeader, sps *SPS, pps *PPS) []byte {
	t.Helper()
	w := bits.NewWriter()
	if err := WriteSliceHeader(w, h, sps, pps); err != nil {
		t.Fatalf("WriteSliceHeader: %v", err)
	}
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatalf("writer error: %v", err)
	}
	return w.Bytes()
}

func nalHeaderFor(h *SliceHeader) nal.Header {
	typ := nal.TypeSliceNonIDR
	if h.IDR {
		typ = nal.TypeSliceIDR
	}
	return nal.Header{RefIDC: h.NalRefIDC, Type: typ}
}

func roundtripSliceHeader(t *testing.T, h *SliceHeader, sps *SPS, pps *PPS) (*SliceHeader, error) {
	t.Helper()
	b := sliceHeaderBytes(t, h, sps, pps)
	r := bits.NewReader(b)
	sets := newFakeParams().addSPS(sps).addPPS(pps)
	parsed, _, _, err := ParseSliceHeader(r, nalHeaderFor(h), sets)
	return parsed, err
}

func baseSPS() *SPS {
	return &SPS{
		ProfileIDC:                  77,
		ConstraintSet:               0,
		LevelIDC:                    30,
		ID:                          0,
		ChromaFormatIDC:             Chroma420,
		Log2MaxFrameNumMinus4:       0,
		PicOrderCntType:             0,
		Log2MaxPicOrderCntLsbMinus4: 0,
		MaxNumRefFrames:             1,
		PicWidthInMbsMinus1:         9,
		PicHeightInMapUnitsMinus1:   7,
		FrameMbsOnly:                true,
		Direct8x8Inference:          true,
	}
}

func basePPS(spsID uint32) *PPS {
	return &PPS{
		ID:                             0,
		SPSID:                          spsID,
		NumRefIdxL0DefaultActiveMinus1: 0,
		NumRefIdxL1DefaultActiveMinus1: 0,
		PicInitQPMinus26:               0,
		PicInitQSMinus26:               0,
		ChromaQPIndexOffset:            0,
	}
}

func flatList4x4(seed uint8) [16]uint8 {
	var a [16]uint8
	for i := range a {
		a[i] = seed + uint8(i) + 1
	}
	return a
}

func flatList8x8(seed uint8) [64]uint8 {
	var a [64]uint8
	for i := range a {
		a[i] = seed + uint8(i%250) + 1
	}
	return a
}

func defaultList4x4() [16]uint8 {
	var a [16]uint8
	for i := range a {
		a[i] = 8
	}
	return a
}

func defaultList8x8() [64]uint8 {
	var a [64]uint8
	for i := range a {
		a[i] = 8
	}
	return a
}

func bytesEqual(t *testing.T, name string, a, b []byte) {
	t.Helper()
	if !bytes.Equal(a, b) {
		t.Fatalf("%s: byte mismatch\n got: % x\nwant: % x", name, a, b)
	}
}
