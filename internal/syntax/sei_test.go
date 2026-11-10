package syntax

import (
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

func seiHRDSPS(nalHRD, vclHRD bool, cpbCnt int, picStruct bool, timeOffsetLength uint8) *SPS {
	s := baseSPS()
	s.VUIPresent = true
	build := func() HRD {
		h := HRD{
			CPBCntMinus1:                       uint32(cpbCnt - 1),
			BitRateScale:                       2,
			CPBSizeScale:                       3,
			InitialCPBRemovalDelayLengthMinus1: 23,
			CPBRemovalDelayLengthMinus1:        15,
			DPBOutputDelayLengthMinus1:         9,
			TimeOffsetLength:                   timeOffsetLength,
			BitRateValueMinus1:                 make([]uint32, cpbCnt),
			CPBSizeValueMinus1:                 make([]uint32, cpbCnt),
			CBRFlag:                            make([]bool, cpbCnt),
		}
		for i := 0; i < cpbCnt; i++ {
			h.BitRateValueMinus1[i] = uint32(1000 + i*37)
			h.CPBSizeValueMinus1[i] = uint32(2000 + i*53)
			h.CBRFlag[i] = i%2 == 0
		}
		return h
	}
	if nalHRD {
		s.VUI.NalHRDPresent = true
		s.VUI.NalHRD = build()
	}
	if vclHRD {
		s.VUI.VclHRDPresent = true
		s.VUI.VclHRD = build()
	}
	s.VUI.PicStructPresent = picStruct
	return s
}

func mustParseSEI(t *testing.T, rbsp []byte, activeSPS *SPS, lookup func(uint32) *SPS) []SEIMessage {
	t.Helper()
	msgs, err := ParseSEI(rbsp, activeSPS, lookup)
	if err != nil {
		t.Fatalf("ParseSEI: %v", err)
	}
	return msgs
}

func mustWriteSEI(t *testing.T, msgs []SEIMessage, activeSPS *SPS, lookup func(uint32) *SPS) []byte {
	t.Helper()
	b, err := WriteSEI(msgs, activeSPS, lookup)
	if err != nil {
		t.Fatalf("WriteSEI: %v", err)
	}
	return b
}

func TestSEIRecoveryPointRoundTrip(t *testing.T) {
	msgs := []SEIMessage{
		{
			PayloadType: SEIPayloadTypeRecoveryPoint,
			RecoveryPoint: &SEIRecoveryPoint{
				RecoveryFrameCnt:      17,
				ExactMatch:            true,
				BrokenLink:            false,
				ChangingSliceGroupIDC: 2,
			},
		},
	}
	b := mustWriteSEI(t, msgs, nil, nil)
	got := mustParseSEI(t, b, nil, nil)
	if !reflect.DeepEqual(msgs, got) {
		t.Fatalf("round trip mismatch\n want %+v\n  got %+v", msgs, got)
	}
	b2 := mustWriteSEI(t, got, nil, nil)
	bytesEqual(t, "recovery point sei", b2, b)
}

func TestSEIUserDataUnregisteredRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{name: "no_extra_bytes", data: nil},
		{name: "with_payload", data: []byte("go264 encoder identity stamp")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msgs := []SEIMessage{
				{
					PayloadType: SEIPayloadTypeUserDataUnregistered,
					UserDataUnregistered: &SEIUserDataUnregistered{
						UUID: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
						Data: c.data,
					},
				},
			}
			b := mustWriteSEI(t, msgs, nil, nil)
			got := mustParseSEI(t, b, nil, nil)
			if len(got) != 1 || got[0].UserDataUnregistered == nil {
				t.Fatalf("expected a single user data unregistered message, got %+v", got)
			}
			if got[0].UserDataUnregistered.UUID != msgs[0].UserDataUnregistered.UUID {
				t.Fatalf("uuid mismatch: got %x want %x", got[0].UserDataUnregistered.UUID, msgs[0].UserDataUnregistered.UUID)
			}
			bytesEqual(t, "user data payload", got[0].UserDataUnregistered.Data, c.data)
		})
	}
}

func TestSEIBufferingPeriodRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		nalHRD bool
		vclHRD bool
		cpbCnt int
	}{
		{name: "nal_only_single_schedule", nalHRD: true, cpbCnt: 1},
		{name: "vcl_only_multi_schedule", vclHRD: true, cpbCnt: 4},
		{name: "both_nal_and_vcl", nalHRD: true, vclHRD: true, cpbCnt: 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sps := seiHRDSPS(c.nalHRD, c.vclHRD, c.cpbCnt, false, 0)
			sps.ID = 4
			lookup := lookupSPSFunc(sps)

			bp := &SEIBufferingPeriod{SeqParameterSetID: sps.ID}
			if c.nalHRD {
				bp.NalHRD = make([]SEIInitialCPBDelay, c.cpbCnt)
				for i := range bp.NalHRD {
					bp.NalHRD[i] = SEIInitialCPBDelay{
						InitialCPBRemovalDelay:       uint32(90000 + i*11),
						InitialCPBRemovalDelayOffset: uint32(1000 + i*3),
					}
				}
			}
			if c.vclHRD {
				bp.VclHRD = make([]SEIInitialCPBDelay, c.cpbCnt)
				for i := range bp.VclHRD {
					bp.VclHRD[i] = SEIInitialCPBDelay{
						InitialCPBRemovalDelay:       uint32(80000 + i*7),
						InitialCPBRemovalDelayOffset: uint32(500 + i*2),
					}
				}
			}
			msgs := []SEIMessage{{PayloadType: SEIPayloadTypeBufferingPeriod, BufferingPeriod: bp}}

			b := mustWriteSEI(t, msgs, nil, lookup)
			got := mustParseSEI(t, b, nil, lookup)
			if !reflect.DeepEqual(msgs, got) {
				t.Fatalf("round trip mismatch\n want %+v\n  got %+v", msgs, got)
			}
			b2 := mustWriteSEI(t, got, nil, lookup)
			bytesEqual(t, "buffering period sei", b2, b)
		})
	}
}

func TestSEIPicTimingRoundTrip(t *testing.T) {
	cases := []struct {
		name             string
		nalHRD           bool
		vclHRD           bool
		picStruct        bool
		picStructValue   uint8
		timeOffsetLength uint8
		clocks           func() []SEIClockTimestamp
	}{
		{name: "delays_only_no_pic_struct", nalHRD: true},
		{
			name: "pic_struct_full_timestamp", nalHRD: true, picStruct: true, picStructValue: 0, timeOffsetLength: 10,
			clocks: func() []SEIClockTimestamp {
				return []SEIClockTimestamp{{
					Present: true, CTType: 1, NuitFieldBased: true, CountingType: 4,
					FullTimestamp: true, Discontinuity: false, CntDropped: true,
					NFrames: 24, Seconds: 30, Minutes: 15, Hours: 5, TimeOffset: -37,
				}}
			},
		},
		{
			name: "pic_struct_partial_timestamp_flags", vclHRD: true, picStruct: true, picStructValue: 3, timeOffsetLength: 24,
			clocks: func() []SEIClockTimestamp {
				return []SEIClockTimestamp{
					{
						Present: true, CTType: 0, NuitFieldBased: false, CountingType: 0,
						FullTimestamp: false, SecondsFlag: true, Seconds: 42,
						MinutesFlag: true, Minutes: 7, HoursFlag: true, Hours: 3, TimeOffset: 1000,
					},
					{Present: false},
				}
			},
		},
		{
			name: "pic_struct_no_clock_timestamp_present", nalHRD: true, picStruct: true, picStructValue: 7, timeOffsetLength: 0,
			clocks: func() []SEIClockTimestamp { return []SEIClockTimestamp{{}, {}} },
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sps := seiHRDSPS(c.nalHRD, c.vclHRD, 1, c.picStruct, c.timeOffsetLength)
			sps.ID = 7

			pt := &SEIPicTiming{CPBRemovalDelay: 12345, DPBOutputDelay: 2}
			pt.PicStruct = c.picStructValue
			if c.picStruct {
				if c.clocks != nil {
					pt.ClockTimestamps = c.clocks()
				} else {
					pt.ClockTimestamps = make([]SEIClockTimestamp, seiNumClockTS[c.picStructValue])
				}
			}
			msgs := []SEIMessage{{PayloadType: SEIPayloadTypePicTiming, PicTiming: pt}}

			b := mustWriteSEI(t, msgs, sps, nil)
			got := mustParseSEI(t, b, sps, nil)
			if !reflect.DeepEqual(msgs, got) {
				t.Fatalf("round trip mismatch\n want %+v\n  got %+v", msgs, got)
			}
			b2 := mustWriteSEI(t, got, sps, nil)
			bytesEqual(t, "pic timing sei", b2, b)
		})
	}
}

func TestSEIUnknownPayloadRoundTrip(t *testing.T) {
	msgs := []SEIMessage{
		{PayloadType: 137, Opaque: []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}},
	}
	b := mustWriteSEI(t, msgs, nil, nil)
	got := mustParseSEI(t, b, nil, nil)
	if !reflect.DeepEqual(msgs, got) {
		t.Fatalf("round trip mismatch\n want %+v\n  got %+v", msgs, got)
	}
}

func TestSEIMultipleMessagesInOneNAL(t *testing.T) {
	msgs := []SEIMessage{
		{
			PayloadType: SEIPayloadTypeUserDataUnregistered,
			UserDataUnregistered: &SEIUserDataUnregistered{
				UUID: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				Data: []byte("hello"),
			},
		},
		{
			PayloadType: SEIPayloadTypeRecoveryPoint,
			RecoveryPoint: &SEIRecoveryPoint{
				RecoveryFrameCnt: 5, ExactMatch: true,
			},
		},
		{PayloadType: 200, Opaque: []byte{1, 2, 3}},
	}
	b := mustWriteSEI(t, msgs, nil, nil)
	got := mustParseSEI(t, b, nil, nil)
	if !reflect.DeepEqual(msgs, got) {
		t.Fatalf("round trip mismatch\n want %+v\n  got %+v", msgs, got)
	}
}

func TestSEIPayloadTypeExtensionEncoding(t *testing.T) {
	msgs := []SEIMessage{
		{PayloadType: 260, Opaque: make([]byte, 300)},
	}
	for i := range msgs[0].Opaque {
		msgs[0].Opaque[i] = byte(i)
	}
	b := mustWriteSEI(t, msgs, nil, nil)
	got := mustParseSEI(t, b, nil, nil)
	if !reflect.DeepEqual(msgs, got) {
		t.Fatalf("round trip mismatch for extended type/size encoding")
	}
	r := bits.NewReader(b)
	v, err := readSEIExtended(r)
	if err != nil || v != 260 {
		t.Fatalf("expected extended payload type 260, got %d err %v", v, err)
	}
}

func TestSEIMissingSPSFallsBackToOpaqueError(t *testing.T) {
	sps := seiHRDSPS(true, false, 1, false, 0)
	sps.ID = 9
	lookup := lookupSPSFunc(sps)
	bp := &SEIBufferingPeriod{
		SeqParameterSetID: sps.ID,
		NalHRD:            []SEIInitialCPBDelay{{InitialCPBRemovalDelay: 1, InitialCPBRemovalDelayOffset: 2}},
	}
	msgs := []SEIMessage{{PayloadType: SEIPayloadTypeBufferingPeriod, BufferingPeriod: bp}}
	b := mustWriteSEI(t, msgs, nil, lookup)

	got := mustParseSEI(t, b, nil, func(uint32) *SPS { return nil })
	if len(got) != 1 || got[0].BufferingPeriod != nil || got[0].Opaque == nil {
		t.Fatalf("expected a missing SPS to fall back to an opaque message, got %+v", got)
	}
	if got[0].PayloadType != SEIPayloadTypeBufferingPeriod {
		t.Fatalf("payload type changed across the opaque fallback: got %d", got[0].PayloadType)
	}
	b2 := mustWriteSEI(t, got, nil, nil)
	bytesEqual(t, "opaque fallback round trip", b2, b)
}
