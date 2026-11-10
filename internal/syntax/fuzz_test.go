package syntax

import (
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/nal"
	"github.com/go-avkit/h264/internal/testutil"
)

func FuzzParseSPS(f *testing.F) {
	for _, sc := range buildSliceScenarios() {
		if b, err := WriteSPS(sc.sps); err == nil {
			f.Add(b)
		}
	}
	for _, c := range []*SPS{baseSPS()} {
		if b, err := WriteSPS(c); err == nil {
			f.Add(b)
		}
	}
	for _, rbsp := range testutil.RBSPOfType(testutil.NALTypeSPS) {
		f.Add(rbsp)
	}
	f.Add([]byte{})
	f.Add([]byte{0x64, 0x00, 0x1f})

	f.Fuzz(func(t *testing.T, data []byte) {
		s, err := ParseSPS(data)
		if err != nil {
			return
		}
		b, err := WriteSPS(s)
		if err != nil {
			t.Fatalf("WriteSPS failed after successful ParseSPS: %v", err)
		}
		s2, err := ParseSPS(b)
		if err != nil {
			t.Fatalf("second ParseSPS failed: %v", err)
		}
		if !reflect.DeepEqual(s, s2) {
			t.Fatalf("round trip mismatch\n first: %+v\nsecond: %+v", s, s2)
		}
	})
}

func FuzzParsePPS(f *testing.F) {
	sps := baseSPS()
	sps.ChromaFormatIDC = Chroma444
	lookup := lookupSPSFunc(sps)

	for _, name := range []func() *PPS{
		func() *PPS { return basePPS(sps.ID) },
		func() *PPS {
			p := basePPS(sps.ID)
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
			return p
		},
	} {
		if b, err := WritePPS(name(), lookup); err == nil {
			f.Add(b)
		}
	}
	for _, rbsp := range testutil.RBSPOfType(testutil.NALTypePPS) {
		f.Add(rbsp)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := ParsePPS(data, lookup)
		if err != nil {
			return
		}
		b, err := WritePPS(p, lookup)
		if err != nil {
			t.Fatalf("WritePPS failed after successful ParsePPS: %v", err)
		}
		p2, err := ParsePPS(b, lookup)
		if err != nil {
			t.Fatalf("second ParsePPS failed: %v", err)
		}
		if !reflect.DeepEqual(p, p2) {
			t.Fatalf("round trip mismatch\n first: %+v\nsecond: %+v", p, p2)
		}
	})
}

func FuzzParseSliceHeader(f *testing.F) {
	for _, sc := range buildSliceScenarios() {
		b := func() []byte {
			w := bits.NewWriter()
			if err := WriteSliceHeader(w, sc.h, sc.sps, sc.pps); err != nil {
				return nil
			}
			w.WriteRBSPTrailingBits()
			if w.Err() != nil {
				return nil
			}
			return w.Bytes()
		}()
		if b != nil {
			f.Add(b)
		}
	}

	for _, rbsp := range testutil.RBSPOfType(testutil.NALTypeSliceNonIDR, testutil.NALTypeSliceIDR) {
		f.Add(rbsp)
	}

	sps := baseSPS()
	pps := basePPS(sps.ID)
	sets := newFakeParams().addSPS(sps).addPPS(pps)
	unit := nal.Header{RefIDC: 1, Type: nal.TypeSliceNonIDR}

	f.Fuzz(func(t *testing.T, data []byte) {
		r := bits.NewReader(data)
		_, _, _, _ = ParseSliceHeader(r, unit, sets)
	})
}

func FuzzParseSEI(f *testing.F) {
	sps := seiHRDSPS(true, true, 2, true, 12)
	sps.ID = 3
	lookup := lookupSPSFunc(sps)

	seeds := [][]SEIMessage{
		{{PayloadType: SEIPayloadTypeRecoveryPoint, RecoveryPoint: &SEIRecoveryPoint{RecoveryFrameCnt: 9, ExactMatch: true}}},
		{{PayloadType: SEIPayloadTypeUserDataUnregistered, UserDataUnregistered: &SEIUserDataUnregistered{
			UUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Data: []byte("go264"),
		}}},
		{{PayloadType: SEIPayloadTypeBufferingPeriod, BufferingPeriod: &SEIBufferingPeriod{
			SeqParameterSetID: sps.ID,
			NalHRD:            []SEIInitialCPBDelay{{InitialCPBRemovalDelay: 1, InitialCPBRemovalDelayOffset: 2}, {InitialCPBRemovalDelay: 3, InitialCPBRemovalDelayOffset: 4}},
			VclHRD:            []SEIInitialCPBDelay{{InitialCPBRemovalDelay: 5, InitialCPBRemovalDelayOffset: 6}, {InitialCPBRemovalDelay: 7, InitialCPBRemovalDelayOffset: 8}},
		}}},
		{{PayloadType: SEIPayloadTypePicTiming, PicTiming: &SEIPicTiming{
			CPBRemovalDelay: 100, DPBOutputDelay: 4, PicStruct: 0,
			ClockTimestamps: []SEIClockTimestamp{{Present: true, CTType: 1, CountingType: 3, FullTimestamp: true, NFrames: 5, Seconds: 1, Minutes: 2, Hours: 3, TimeOffset: -5}},
		}}},
		{{PayloadType: 99, Opaque: []byte{1, 2, 3, 4}}},
	}
	for _, msgs := range seeds {
		var activeSPS *SPS
		for _, m := range msgs {
			if m.PicTiming != nil {
				activeSPS = sps
			}
		}
		if b, err := WriteSEI(msgs, activeSPS, lookup); err == nil {
			f.Add(b)
		}
	}
	for _, rbsp := range testutil.RBSPOfType(testutil.NALTypeSEI) {
		f.Add(rbsp)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		msgs, err := ParseSEI(data, sps, lookup)
		if err != nil {
			return
		}
		b, err := WriteSEI(msgs, sps, lookup)
		if err != nil {
			t.Fatalf("WriteSEI failed after successful ParseSEI: %v", err)
		}
		msgs2, err := ParseSEI(b, sps, lookup)
		if err != nil {
			t.Fatalf("second ParseSEI failed: %v", err)
		}
		if !reflect.DeepEqual(msgs, msgs2) {
			t.Fatalf("round trip mismatch\n first: %+v\nsecond: %+v", msgs, msgs2)
		}
	})
}
