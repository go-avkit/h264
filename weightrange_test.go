// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// weighted builds a P slice carrying one weighted reference.
func weighted(t *testing.T, denom uint32, e RefWeight) (SliceReferences, error) {
	t.Helper()
	sps, pps := refSets()
	u := refSlice{typ: uint32(SliceP), refIDC: 2, frameNum: 1, pocLSB: 2,
		lumaDenom: denom, l0Weights: []RefWeight{e}}.build(sps, pps)
	_, ref, err := ParseSliceReferences(u, sps, pps)
	return ref, err
}

// TestAWeightDenominatorIsAShiftCount.
//
// ⛔ The denominator leaves this package as a SHIFT COUNT: Weighting computes
// 1 << LogDenom and shifts a sample by it. A count the stream chose freely is a
// shift by whatever it likes, and in Go that is not a panic but a silently
// wrong number. 7.4.3.2 puts it in 0..7.
func TestAWeightDenominatorIsAShiftCount(t *testing.T) {
	for _, denom := range []uint32{8, 31, 1 << 20} {
		if _, err := weighted(t, denom, RefWeight{}); !errors.Is(err, ErrSliceHeader) {
			t.Errorf("denominator %d: err = %v, want ErrSliceHeader", denom, err)
		}
	}
	// Seven is the largest the format allows and must be read.
	ref, err := weighted(t, 7, RefWeight{})
	if err != nil || ref.Weights == nil || ref.Weights.LumaLog2Denom != 7 {
		t.Errorf("a denominator of seven gave %v, %v", ref.Weights, err)
	}
}

// TestAWeightTheArithmeticCannotCarry.
//
// ⛔ A weight multiplies a sample BEFORE anything clamps it, so an unbounded
// one overflows the arithmetic rather than making a bright picture. 7.4.3.2
// puts every weight and every offset in -128..127, and ffmpeg checks each by
// casting it to int8_t and comparing.
func TestAWeightTheArithmeticCannotCarry(t *testing.T) {
	for _, c := range []struct {
		name string
		e    RefWeight
	}{
		{"a luma weight past a byte", RefWeight{LumaStated: true, LumaWeight: 128}},
		{"a luma weight below a byte", RefWeight{LumaStated: true, LumaWeight: -129}},
		{"a luma offset past a byte", RefWeight{LumaStated: true, LumaOffset: 128}},
		{"a huge luma weight", RefWeight{LumaStated: true, LumaWeight: 1 << 30}},
		{"a chroma weight past a byte", RefWeight{ChromaStated: true, ChromaWeight: [2]int32{128, 0}}},
		{"a chroma offset past a byte", RefWeight{ChromaStated: true, ChromaOffset: [2]int32{0, -129}}},
		// ⛔ The SECOND chroma component, so that a reader checking only the
		// first is caught.
		{"the second chroma weight", RefWeight{ChromaStated: true, ChromaWeight: [2]int32{0, 1 << 20}}},
	} {
		if _, err := weighted(t, 5, c.e); !errors.Is(err, ErrSliceHeader) {
			t.Errorf("%s: err = %v, want ErrSliceHeader", c.name, err)
		}
	}

	// Both boundaries must be READ: -128 and 127 are what the format allows,
	// and refusing them would refuse conformant streams.
	for _, v := range []int32{-128, 127} {
		e := RefWeight{LumaStated: true, LumaWeight: v, LumaOffset: v,
			ChromaStated: true, ChromaWeight: [2]int32{v, v}, ChromaOffset: [2]int32{v, v}}
		ref, err := weighted(t, 5, e)
		if err != nil {
			t.Errorf("a weight of %d was refused: %v", v, err)
			continue
		}
		got := ref.Weights.L0[0]
		if got.LumaWeight != v || got.LumaOffset != v || got.ChromaWeight[1] != v {
			t.Errorf("a weight of %d came back as %+v", v, got)
		}
	}
}

// TestAWeightTableThatEndsInsideItself.
//
// ⛔ A range check that runs on a value the reader never read would refuse a
// TRUNCATED stream for the wrong reason, and name a number that was never in
// it. The check has to stand down once the reader has failed.
func TestAWeightTableThatEndsInsideItself(t *testing.T) {
	sps, pps := refSets()
	whole := refSlice{typ: uint32(SliceB), refIDC: 2, frameNum: 1, pocLSB: 2,
		override: true, l0Active: 2, l1Active: 2, lumaDenom: 6,
		l0Weights: []RefWeight{
			{LumaStated: true, LumaWeight: 70, LumaOffset: -12,
				ChromaStated: true, ChromaWeight: [2]int32{60, -60}, ChromaOffset: [2]int32{7, -7}},
			{LumaStated: true, LumaWeight: -70, LumaOffset: 12},
		},
		l1Weights: []RefWeight{
			{ChromaStated: true, ChromaWeight: [2]int32{33, 33}},
			{LumaStated: true, LumaWeight: 1},
		},
	}.build(sps, pps)
	if _, ref, err := ParseSliceReferences(whole, sps, pps); err != nil || ref.Weights == nil {
		t.Fatalf("the whole slice stopped parsing: %v", err)
	}
	for n := 1; n < len(whole.Payload)-1; n++ {
		short := Unit{Type: whole.Type, RefIDC: whole.RefIDC, Payload: whole.Payload[:n]}
		_, _, err := ParseSliceReferences(short, sps, pps)
		if err == nil {
			t.Errorf("%d of %d bytes: accepted", n, len(whole.Payload))
		}
	}
}

// TestAFailedReadReturnsZero.
//
// ⛔ weightInRange runs on whatever the reader handed back, including on the
// read that FAILED. It is safe to range-check that value only because a failed
// read returns zero, which is in range -- so a truncated stream is reported as
// truncation rather than as a weight it never carried.
//
// This is the measurement that let the stand-down guard go. Pinning it here
// makes it part of the contract rather than something that happened to hold.
func TestAFailedReadReturnsZero(t *testing.T) {
	for _, data := range [][]byte{
		{0x00},       // the prefix runs past the data
		{0x00, 0x00}, //
		{0x01},       // a prefix with no suffix behind it
		{0x00, 0x01},
		{0x02},
		{},
	} {
		r := newSticky(data)
		if v := r.se(); v != 0 {
			t.Errorf("se() on %v returned %d with err %v, want 0", data, v, r.err)
		}
		if r.err == nil {
			t.Errorf("se() on %v reported no error", data)
		}
		r = newSticky(data)
		if v := r.ue(); v != 0 {
			t.Errorf("ue() on %v returned %d with err %v, want 0", data, v, r.err)
		}
	}
}
