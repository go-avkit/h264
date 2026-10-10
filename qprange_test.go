// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// ⛔ A fixture whose weight table is the wrong LENGTH misaligns everything
// after it, and the parse then fails naming a field the test never touched. A
// first version of this probe did exactly that and reported "a chroma weight of
// 1000" for a quantiser delta. The baseline below is checked FIRST so that a
// refusal further down is about the field the case names.
func qpBase() refSlice {
	return refSlice{typ: uint32(SliceP), refIDC: 2, frameNum: 1, pocLSB: 2,
		override: true, l0Active: 1, lumaDenom: 5, l0Weights: []RefWeight{{}}}
}

func TestTheBaselineSliceParsesCleanly(t *testing.T) {
	sps, pps := refSets()
	_, ref, err := ParseSliceReferences(qpBase().build(sps, pps), sps, pps)
	if err != nil {
		t.Fatalf("the baseline does not parse, so nothing built on it measures a field: %v", err)
	}
	if ref.QP != pps.InitQP || ref.CABACInit != 0 || ref.DeblockingIDC != 0 {
		t.Errorf("baseline QP=%d cabac=%d deblock=%d, want %d/0/0",
			ref.QP, ref.CABACInit, ref.DeblockingIDC, pps.InitQP)
	}
}

// TestASelectorTheConsumerIndexesWith.
//
// ⛔ cabac_init_idc picks one of THREE context tables and
// disable_deblocking_filter_idc one of three behaviours. Both were unbounded,
// and a consumer indexes with them: 1 048 576 was read and handed on.
func TestASelectorTheConsumerIndexesWith(t *testing.T) {
	sps, pps := refSets()
	for _, c := range []struct {
		name   string
		apply  func(*refSlice)
		refuse bool
	}{
		{"cabac_init_idc 2", func(s *refSlice) { s.cabacInit = 2 }, false},
		{"cabac_init_idc 3", func(s *refSlice) { s.cabacInit = 3 }, true},
		{"cabac_init_idc a million", func(s *refSlice) { s.cabacInit = 1 << 20 }, true},
		{"deblocking idc 2", func(s *refSlice) { s.deblockID = 2 }, false},
		{"deblocking idc 3", func(s *refSlice) { s.deblockID = 3 }, true},
		{"deblocking idc a million", func(s *refSlice) { s.deblockID = 1 << 20 }, true},
	} {
		s := qpBase()
		c.apply(&s)
		_, _, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		switch {
		case c.refuse && !errors.Is(err, ErrSliceHeader):
			t.Errorf("%s: err = %v, want ErrSliceHeader", c.name, err)
		case !c.refuse && err != nil:
			t.Errorf("%s was refused: %v", c.name, err)
		}
	}
}

// TestADeblockingOffsetIsDoubledIntoATableIndex: 7.4.3 puts each in -6..6.
func TestADeblockingOffsetIsDoubledIntoATableIndex(t *testing.T) {
	sps, pps := refSets()
	for _, c := range []struct {
		name        string
		alpha, beta int32
		refuse      bool
	}{
		{"both at the ceiling", 6, 6, false},
		{"both at the floor", -6, -6, false},
		{"alpha one past", 7, 0, true},
		{"beta one below", 0, -7, true},
		{"alpha far past", 100, 0, true},
		{"beta far below", 0, -100, true},
	} {
		s := qpBase()
		s.alpha, s.beta = c.alpha, c.beta
		_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		switch {
		case c.refuse && !errors.Is(err, ErrSliceHeader):
			t.Errorf("%s: err = %v, want ErrSliceHeader", c.name, err)
		case !c.refuse && err != nil:
			t.Errorf("%s was refused: %v", c.name, err)
		case !c.refuse && (ref.AlphaC0OffsetDiv2 != c.alpha || ref.BetaOffsetDiv2 != c.beta):
			t.Errorf("%s read back as %d/%d", c.name, ref.AlphaC0OffsetDiv2, ref.BetaOffsetDiv2)
		}
	}
}

// TestASliceQuantiserOutsideTheTables.
//
// ⛔ SliceQPY indexes the quantisation tables, and it is a SUM: the set's
// InitQP plus the slice's delta. Neither was bounded, so a delta of 1000 gave
// a quantiser of 1026 -- twenty times the largest the tables hold.
func TestASliceQuantiserOutsideTheTables(t *testing.T) {
	sps, pps := refSets() // InitQP 26 at eight bits, so the range is 0..51
	for _, c := range []struct {
		delta  int32
		refuse bool
	}{
		{25, false},  // QP 51, the ceiling
		{-26, false}, // QP 0, the floor at eight bits
		{26, true},   // QP 52
		{-27, true},  // QP -1
		{1000, true},
		{-1000, true},
	} {
		s := qpBase()
		s.qpDelta = c.delta
		_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		switch {
		case c.refuse && !errors.Is(err, ErrSliceHeader):
			t.Errorf("a delta of %d gave QP %d and err %v, want ErrSliceHeader", c.delta, ref.QP, err)
		case !c.refuse && err != nil:
			t.Errorf("a delta of %d was refused: %v", c.delta, err)
		}
	}

	// ⛔ The floor MOVES with the bit depth: QpBdOffsetY is six per bit past
	// eight, so a ten-bit sequence may state a quantiser down to -12. A bound
	// written as zero would refuse a conformant ten-bit stream.
	deep := sps
	deep.BitDepthLuma = 10
	s := qpBase()
	s.qpDelta = -38 // QP -12
	if _, ref, err := ParseSliceReferences(s.build(deep, pps), deep, pps); err != nil {
		t.Errorf("a ten-bit quantiser of %d was refused: %v", ref.QP, err)
	}
	s.qpDelta = -39 // QP -13, one past
	if _, _, err := ParseSliceReferences(s.build(deep, pps), deep, pps); !errors.Is(err, ErrSliceHeader) {
		t.Errorf("err = %v, want ErrSliceHeader", err)
	}
}
