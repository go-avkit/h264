// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// flag writes one bit from a boolean, which is what most of this syntax is.
func (w *spsWriter) flag(v bool) {
	if v {
		w.bit(1)
		return
	}
	w.bit(0)
}

// refSlice describes a whole slice header, through the reference syntax, so a
// test states the slice it means rather than a byte string.
type refSlice struct {
	typ      uint32
	refIDC   uint8
	idr      bool
	frameNum uint32
	pocLSB   uint32

	direct    bool
	override  bool
	l0Active  uint32 // written as a minus-one when override is set
	l1Active  uint32
	modifyL0  []RefListOp
	modifyL1  []RefListOp
	lumaDenom uint32
	l0Weights []RefWeight
	l1Weights []RefWeight
	noOutput  bool
	longTerm  bool
	adaptive  bool
	marking   []MarkOp
	cabacInit uint32
	qpDelta   int32
	qsDelta   int32
	deblockID uint32
	alpha     int32
	beta      int32
}

func (s refSlice) build(sps SPS, pps PPS) Unit {
	w := &spsWriter{}
	w.ue(0) // first_mb_in_slice
	w.ue(s.typ)
	w.ue(0) // pic_parameter_set_id
	w.bits(s.frameNum, int(sps.Log2MaxFrameNum))
	if s.idr {
		w.ue(7)
	}
	if sps.POCType == 0 {
		w.bits(s.pocLSB, int(sps.Log2MaxPOCLSB))
	}
	t := SliceType(s.typ % 5)
	if t == SliceB {
		w.flag(s.direct)
	}
	if t == SliceP || t == SliceSP || t == SliceB {
		w.flag(s.override)
		if s.override {
			w.ue(s.l0Active - 1)
			if t == SliceB {
				w.ue(s.l1Active - 1)
			}
		}
	}
	if t != SliceI && t != SliceSI {
		writeRefListOps(w, s.modifyL0)
	}
	if t == SliceB {
		writeRefListOps(w, s.modifyL1)
	}
	if (pps.WeightedPred && (t == SliceP || t == SliceSP)) ||
		(pps.WeightedBipredIDC == 1 && t == SliceB) {
		w.ue(s.lumaDenom)
		if chromaArrayType(sps) != 0 {
			w.ue(0) // chroma_log2_weight_denom
		}
		writeWeights(w, s.l0Weights, chromaArrayType(sps) != 0)
		if t == SliceB {
			writeWeights(w, s.l1Weights, chromaArrayType(sps) != 0)
		}
	}
	if s.refIDC != 0 {
		if s.idr {
			w.flag(s.noOutput)
			w.flag(s.longTerm)
		} else {
			w.flag(s.adaptive)
			if s.adaptive {
				for _, m := range s.marking {
					w.ue(m.Operation)
					switch m.Operation {
					case 1, 2, 4, 6:
						w.ue(m.Value)
					case 3:
						w.ue(m.Value)
						w.ue(m.Extra)
					}
				}
				w.ue(0) // the list ends
			}
		}
	}
	if pps.CABAC && t != SliceI && t != SliceSI {
		w.ue(s.cabacInit)
	}
	w.se(s.qpDelta)
	if t == SliceSP || t == SliceSI {
		if t == SliceSP {
			w.flag(false) // sp_for_switch_flag
		}
		w.se(s.qsDelta)
	}
	if pps.DeblockingControl {
		w.ue(s.deblockID)
		if s.deblockID != 1 {
			w.se(s.alpha)
			w.se(s.beta)
		}
	}
	w.bits(0xAB, 8) // coded data stands in
	kind := UnitNonIDR
	if s.idr {
		kind = UnitIDR
	}
	return Unit{Type: kind, RefIDC: s.refIDC, Payload: w.data}
}

func writeRefListOps(w *spsWriter, ops []RefListOp) {
	if len(ops) == 0 {
		w.flag(false)
		return
	}
	w.flag(true)
	for _, op := range ops {
		w.ue(op.Kind)
		w.ue(op.Value)
	}
	w.ue(3) // three ends the list
}

func writeWeights(w *spsWriter, list []RefWeight, chroma bool) {
	for _, e := range list {
		w.flag(e.LumaStated)
		if e.LumaStated {
			w.se(e.LumaWeight)
			w.se(e.LumaOffset)
		}
		if chroma {
			w.flag(e.ChromaStated)
			if e.ChromaStated {
				for j := 0; j < 2; j++ {
					w.se(e.ChromaWeight[j])
					w.se(e.ChromaOffset[j])
				}
			}
		}
	}
}

// refSets are parameter sets that ask for every optional part of the syntax, so a
// fixture exercises the conditionals rather than skipping them.
func refSets() (SPS, PPS) {
	sps := SPS{ChromaFormat: 1, FrameMBSOnly: true, Log2MaxFrameNum: 4, Log2MaxPOCLSB: 4,
		Width: 32, Height: 32, MBWidth: 2, MBHeight: 2, MaxNumRefFrames: 4}
	pps := PPS{CABAC: true, NumRefIdxL0: 1, NumRefIdxL1: 1, InitQP: 26,
		WeightedPred: true, WeightedBipredIDC: 1, DeblockingControl: true}
	return sps, pps
}

// TestTheQuantisationParameterIsThisReadersOwnWitness.
//
// ⛔ Every field of this syntax has a width that depends on the fields before it,
// so a parse that drifts by one bit anywhere lands at the quantisation delta with
// a number outside the only range the format allows. That is how 4816 slices from
// 81 real files, written by several encoders, confirmed this reader with no
// reference decoder to compare against: QP came out in [5, 47], and not one of the
// 4816 fell outside [0, 51].
func TestTheQuantisationParameterIsThisReadersOwnWitness(t *testing.T) {
	sps, pps := refSets()
	for _, delta := range []int32{-26, -1, 0, 1, 25} {
		s := refSlice{typ: 0, refIDC: 2, frameNum: 1, pocLSB: 2, override: true,
			l0Active: 1, lumaDenom: 5, l0Weights: []RefWeight{{}}, qpDelta: delta}
		_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		if err != nil {
			t.Errorf("delta %d: %v", delta, err)
			continue
		}
		if want := pps.InitQP + delta; ref.QP != want {
			t.Errorf("delta %d: QP = %d, want %d", delta, ref.QP, want)
		}
		if ref.QP < 0 || ref.QP > 51 {
			t.Errorf("delta %d: QP %d is outside what the format allows", delta, ref.QP)
		}
	}
}

// TestAnIslicestatesNoReferenceList.
//
// ⛔ An I or SI slice sends no ref_pic_list_modification at all, so a reader that
// asked for one would take the bits of whatever follows -- here the quantisation
// delta, which would then be read from the middle of the coded data.
func TestAnISliceStatesNoReferenceList(t *testing.T) {
	sps, pps := refSets()
	s := refSlice{typ: 2, refIDC: 3, idr: true, frameNum: 0, pocLSB: 0, qpDelta: -3}
	h, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != SliceI {
		t.Fatalf("type %v", h.Type)
	}
	if ref.QP != pps.InitQP-3 {
		t.Errorf("QP = %d, want %d -- a list was read where none was sent", ref.QP, pps.InitQP-3)
	}
	if len(ref.ModifyL0) != 0 || len(ref.ModifyL1) != 0 {
		t.Errorf("an I slice gave %d and %d list instructions", len(ref.ModifyL0), len(ref.ModifyL1))
	}
	// And an IDR states what to do with the pictures before it, not a marking list.
	if !ref.NoOutputOfPriorPics && !ref.LongTermReference && ref.AdaptiveMarking {
		t.Error("an IDR was read as stating an adaptive marking list")
	}
}

// TestImplicitWeightingSendsNoTable.
//
// ⛔ weighted_bipred_idc of two means the weights come from the picture order
// counts and NO table is sent. A reader that read one would consume the marking,
// the context index and the quantisation delta -- and this is the common case: of
// 2374 B slices measured, not one carried a table while every one of 2330 P slices
// did.
func TestImplicitWeightingSendsNoTable(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedBipredIDC = 2
	s := refSlice{typ: 1, refIDC: 0, frameNum: 3, pocLSB: 6, direct: true, qpDelta: 2}
	h, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if h.Type != SliceB {
		t.Fatalf("type %v", h.Type)
	}
	if ref.Weights != nil {
		t.Error("a table was read where implicit weighting sends none")
	}
	if ref.QP != pps.InitQP+2 {
		t.Errorf("QP = %d, want %d", ref.QP, pps.InitQP+2)
	}
	if !ref.DirectSpatial {
		t.Error("direct_spatial_mv_pred_flag was not read")
	}
	// Explicit weighting does send one.
	pps.WeightedBipredIDC = 1
	s.lumaDenom = 6
	s.l0Weights = []RefWeight{{LumaStated: true, LumaWeight: 3, LumaOffset: -4}}
	s.l1Weights = []RefWeight{{ChromaStated: true, ChromaWeight: [2]int32{2, 3}, ChromaOffset: [2]int32{-1, 1}}}
	_, ref, err = ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Weights == nil {
		t.Fatal("no table where one was sent")
	}
	if ref.Weights.LumaLog2Denom != 6 {
		t.Errorf("luma denominator %d, want 6", ref.Weights.LumaLog2Denom)
	}
	if len(ref.Weights.L0) != 1 || ref.Weights.L0[0].LumaWeight != 3 || ref.Weights.L0[0].LumaOffset != -4 {
		t.Errorf("L0 = %+v", ref.Weights.L0)
	}
	if len(ref.Weights.L1) != 1 || ref.Weights.L1[0].ChromaWeight != [2]int32{2, 3} {
		t.Errorf("L1 = %+v", ref.Weights.L1)
	}
}

// TestAnOverriddenReferenceCountIsTheOneUsed: the picture set states a default and
// a slice may override it. A list built to the wrong length is read past its end
// rather than refused, so the number has to come from the slice when the slice
// states one.
func TestAnOverriddenReferenceCountIsTheOneUsed(t *testing.T) {
	sps, pps := refSets()
	pps.NumRefIdxL0, pps.NumRefIdxL1 = 2, 3
	// Without an override, the set's numbers stand.
	s := refSlice{typ: 1, refIDC: 2, frameNum: 1, pocLSB: 2, qpDelta: 0,
		adaptive: false}
	pps.WeightedBipredIDC = 2 // no table, to keep the fixture about the counts
	_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if ref.NumRefIdxL0Active != 2 || ref.NumRefIdxL1Active != 3 {
		t.Errorf("without an override: %d/%d, want 2/3", ref.NumRefIdxL0Active, ref.NumRefIdxL1Active)
	}
	// With one, the slice's numbers stand.
	s.override, s.l0Active, s.l1Active = true, 4, 1
	_, ref, err = ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if ref.NumRefIdxL0Active != 4 || ref.NumRefIdxL1Active != 1 {
		t.Errorf("with an override: %d/%d, want 4/1", ref.NumRefIdxL0Active, ref.NumRefIdxL1Active)
	}
}

func TestReferenceListInstructionsAreReadInOrder(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	ops := []RefListOp{{Kind: 0, Value: 3}, {Kind: 1, Value: 0}, {Kind: 2, Value: 7}}
	s := refSlice{typ: 0, refIDC: 2, frameNum: 1, pocLSB: 2, modifyL0: ops, qpDelta: 1}
	_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.ModifyL0) != 3 {
		t.Fatalf("%d instructions, want 3", len(ref.ModifyL0))
	}
	for i := range ops {
		if ref.ModifyL0[i] != ops[i] {
			t.Errorf("instruction %d = %+v, want %+v", i, ref.ModifyL0[i], ops[i])
		}
	}
	if ref.QP != pps.InitQP+1 {
		t.Errorf("QP = %d: the list's end was not found where it was written", ref.QP)
	}
}

// TestOperationFiveIsTheOneThatRestartsTheCount.
//
// ⛔ A memory management control operation of five empties the reference set and
// restarts the picture order count at this picture. A reader computing counts
// without honouring it is right until the first one and wrong for ever after, so
// the header says it happened rather than leaving the caller to walk the list. Of
// 4816 slices measured, not one carried it -- which is exactly why it has to be
// reported rather than discovered by accident.
func TestOperationFiveIsTheOneThatRestartsTheCount(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	base := refSlice{typ: 0, refIDC: 2, frameNum: 1, pocLSB: 2, qpDelta: 0, adaptive: true}

	without := base
	without.marking = []MarkOp{{Operation: 1, Value: 0}}
	_, ref, err := ParseSliceReferences(without.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if ref.ResetsPOC {
		t.Error("a list without operation five reported a restart")
	}
	if len(ref.Marking) != 1 || ref.Marking[0].Operation != 1 {
		t.Errorf("marking = %+v", ref.Marking)
	}

	with := base
	with.marking = []MarkOp{{Operation: 1, Value: 0}, {Operation: 5}, {Operation: 4, Value: 2}}
	_, ref, err = ParseSliceReferences(with.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if !ref.ResetsPOC {
		t.Error("operation five was not reported")
	}
	// ⛔ Operation five takes NO argument. A reader that read one would take the
	// operation after it as that argument, and then read the rest of the list from
	// the wrong place -- which the quantisation check below would catch.
	//
	// It is kept in the list, in order, rather than dropped once ResetsPOC is set:
	// a caller applying these in order needs to know WHERE the reset falls, since
	// the operations before it address a reference set the ones after it no longer
	// have.
	if len(ref.Marking) != 3 {
		t.Fatalf("%d operations kept, want all three in order", len(ref.Marking))
	}
	if ref.Marking[1].Operation != 5 || ref.Marking[1].Value != 0 {
		t.Errorf("the reset is at %+v, and should carry no value", ref.Marking[1])
	}
	if ref.Marking[0].Operation != 1 || ref.Marking[2].Operation != 4 || ref.Marking[2].Value != 2 {
		t.Errorf("the order was not kept: %+v", ref.Marking)
	}
	if ref.QP != pps.InitQP {
		t.Errorf("QP = %d, want %d -- the list was not consumed exactly", ref.QP, pps.InitQP)
	}
	// Operation three takes two arguments, and both are kept.
	two := base
	two.marking = []MarkOp{{Operation: 3, Value: 4, Extra: 2}}
	_, ref, err = ParseSliceReferences(two.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Marking) != 1 || ref.Marking[0].Value != 4 || ref.Marking[0].Extra != 2 {
		t.Errorf("marking = %+v", ref.Marking)
	}
}

func TestADeblockingIDCOfOneSendsNoOffsets(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	for _, idc := range []uint32{0, 1, 2} {
		s := refSlice{typ: 0, refIDC: 2, frameNum: 1, pocLSB: 2, qpDelta: 0,
			deblockID: idc, alpha: -3, beta: 2}
		_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		if err != nil {
			t.Errorf("idc %d: %v", idc, err)
			continue
		}
		if ref.DeblockingIDC != idc {
			t.Errorf("idc = %d, want %d", ref.DeblockingIDC, idc)
		}
		wantAlpha, wantBeta := int32(-3), int32(2)
		if idc == 1 {
			wantAlpha, wantBeta = 0, 0 // none are sent
		}
		if ref.AlphaC0OffsetDiv2 != wantAlpha || ref.BetaOffsetDiv2 != wantBeta {
			t.Errorf("idc %d: offsets %d,%d want %d,%d", idc,
				ref.AlphaC0OffsetDiv2, ref.BetaOffsetDiv2, wantAlpha, wantBeta)
		}
	}
}

// TestANonReferencePictureStatesNoMarking: dec_ref_pic_marking is sent only when
// nal_ref_idc is not zero, so reading it for a disposable picture would take the
// context index and the quantisation delta.
func TestANonReferencePictureStatesNoMarking(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedBipredIDC = 2
	s := refSlice{typ: 1, refIDC: 0, frameNum: 2, pocLSB: 4, qpDelta: -5}
	_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if ref.AdaptiveMarking || len(ref.Marking) != 0 {
		t.Errorf("a disposable picture gave a marking: %+v", ref)
	}
	if ref.QP != pps.InitQP-5 {
		t.Errorf("QP = %d, want %d", ref.QP, pps.InitQP-5)
	}
}

// TestAHeaderThatRunsOutIsRefusedRatherThanGuessed.
func TestAHeaderThatRunsOutIsRefusedRatherThanGuessed(t *testing.T) {
	sps, pps := refSets()
	whole := refSlice{typ: 1, refIDC: 2, frameNum: 1, pocLSB: 2, direct: true,
		override: true, l0Active: 2, l1Active: 2, lumaDenom: 5,
		l0Weights: []RefWeight{{LumaStated: true}, {}}, l1Weights: []RefWeight{{}, {}},
		adaptive: true, marking: []MarkOp{{Operation: 1, Value: 1}}, qpDelta: 3,
	}.build(sps, pps)
	if _, _, err := ParseSliceReferences(whole, sps, pps); err != nil {
		t.Fatalf("the fixture does not parse: %v", err)
	}
	for n := 0; n < len(whole.Payload)-1; n++ {
		cut := Unit{Type: whole.Type, RefIDC: whole.RefIDC, Payload: whole.Payload[:n]}
		if _, _, err := ParseSliceReferences(cut, sps, pps); err == nil {
			t.Errorf("%d of %d bytes parsed cleanly", n, len(whole.Payload))
		}
	}
}

// TestAnInstructionThisDoesNotReadIsRefused: kinds four and five belong to
// multiview, and reading past them would take the rest of the header as rubbish.
func TestAnInstructionThisDoesNotReadIsRefused(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	w := &spsWriter{}
	w.ue(0)
	w.ue(0) // P slice
	w.ue(0)
	w.bits(1, int(sps.Log2MaxFrameNum))
	w.bits(2, int(sps.Log2MaxPOCLSB))
	w.flag(false) // no override
	w.flag(true)  // a list follows
	w.ue(4)       // multiview
	w.ue(0)
	w.bits(0xAB, 8)
	u := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w.data}
	if _, _, err := ParseSliceReferences(u, sps, pps); !errors.Is(err, ErrSliceHeader) {
		t.Errorf("err = %v, want ErrSliceHeader", err)
	}
}

// TestALongListIsBoundedRatherThanEndless: both lists are terminated by a value in
// the stream rather than by a count, so a corrupt slice can ask for instructions
// for ever.
func TestALongListIsBoundedRatherThanEndless(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	w := &spsWriter{}
	w.ue(0)
	w.ue(0)
	w.ue(0)
	w.bits(1, int(sps.Log2MaxFrameNum))
	w.bits(2, int(sps.Log2MaxPOCLSB))
	w.flag(false)
	w.flag(true)
	for i := 0; i <= maxRefListOps+2; i++ {
		w.ue(0)
		w.ue(1)
	}
	u := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w.data}
	if _, _, err := ParseSliceReferences(u, sps, pps); err == nil {
		t.Error("an endless instruction list was accepted")
	}
	// The same for the marking list.
	w2 := &spsWriter{}
	w2.ue(0)
	w2.ue(0)
	w2.ue(0)
	w2.bits(1, int(sps.Log2MaxFrameNum))
	w2.bits(2, int(sps.Log2MaxPOCLSB))
	w2.flag(false)
	w2.flag(false) // no list modification
	w2.flag(true)  // adaptive marking
	for i := 0; i <= maxMarkOps+2; i++ {
		w2.ue(4)
		w2.ue(1)
	}
	u2 := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w2.data}
	if _, _, err := ParseSliceReferences(u2, sps, pps); err == nil {
		t.Error("an endless marking list was accepted")
	}
}

// TestTooManyActiveReferencesIsRefused: the count comes from the stream, and a
// corrupt one would have a reader allocate and read to whatever it says.
//
// ⛔ This used to expect ErrSliceHeader, from the bound inside readWeightList.
// The refusal now comes from the slice header itself, EARLIER and for every
// consumer of the count -- a slice stating no weight table never reached the
// old one.
func TestTooManyActiveReferencesIsRefused(t *testing.T) {
	sps, pps := refSets()
	w := &spsWriter{}
	w.ue(0)
	w.ue(0) // P
	w.ue(0)
	w.bits(1, int(sps.Log2MaxFrameNum))
	w.bits(2, int(sps.Log2MaxPOCLSB))
	w.flag(true)                // override
	w.ue(uint32(maxActiveRefs)) // one more than allowed, as a minus-one
	w.flag(false)               // no list modification
	w.ue(5)                     // luma denominator
	w.bits(0xAB, 8)
	u := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w.data}
	if _, _, err := ParseSliceReferences(u, sps, pps); !errors.Is(err, ErrRefIdxRange) {
		t.Errorf("err = %v, want ErrRefIdxRange", err)
	}
}

// TestSeparatePlanesMakeEveryPlaneMonochrome: ChromaArrayType is the chroma format
// EXCEPT when the colour planes are coded separately, and a weight table read with
// the wrong answer is the wrong length.
func TestSeparatePlanesMakeEveryPlaneMonochrome(t *testing.T) {
	for _, c := range []struct {
		sps  SPS
		want uint8
	}{
		{SPS{ChromaFormat: 1}, 1},
		{SPS{ChromaFormat: 3}, 3},
		{SPS{ChromaFormat: 3, SeparatePlanes: true}, 0},
		{SPS{ChromaFormat: 0}, 0},
	} {
		if got := chromaArrayType(c.sps); got != c.want {
			t.Errorf("chroma %d separate %v: %d, want %d",
				c.sps.ChromaFormat, c.sps.SeparatePlanes, got, c.want)
		}
	}
}

// TestPictureSegmentationDoesNotDependOnTheRestOfTheHeader.
//
// ⛔ Reading where a picture begins must not fail because a slice's weight table
// is unreadable. ParseSliceHeader stops at redundant_pic_cnt for that reason, and
// this asserts it: a slice truncated right after that point still says which
// picture it belongs to.
func TestPictureSegmentationDoesNotDependOnTheRestOfTheHeader(t *testing.T) {
	sps, pps := refSets()
	w := &spsWriter{}
	w.ue(0) // first_mb_in_slice
	w.ue(0) // P
	w.ue(0)
	w.bits(5, int(sps.Log2MaxFrameNum))
	w.bits(6, int(sps.Log2MaxPOCLSB))
	// Nothing after this: the reference syntax is missing entirely.
	u := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w.data}

	h, err := ParseSliceHeader(u, sps, pps)
	if err != nil {
		t.Fatalf("segmentation lost a slice it could have placed: %v", err)
	}
	if h.FrameNum != 5 || h.POCLSB != 6 || h.Type != SliceP {
		t.Errorf("header = %+v", h)
	}
	// And the reference reader refuses the same slice, because it cannot read what
	// it is for.
	if _, _, err := ParseSliceReferences(u, sps, pps); err == nil {
		t.Error("the reference reader accepted a slice with no reference syntax")
	}
}

// TestASwitchingSliceStatesOneMoreQuantiser.
//
// ⛔ An SP slice sends sp_for_switch_flag and both SP and SI send slice_qs_delta,
// after the quantisation delta and before the deblocking fields. A reader that
// skipped them would read the deblocking index out of the second quantiser, and
// the two are adjacent so nothing would look obviously wrong.
func TestASwitchingSliceStatesOneMoreQuantiser(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	for _, c := range []struct {
		name string
		typ  uint32
		want SliceType
	}{{"SP", 3, SliceSP}, {"SI", 4, SliceSI}} {
		s := refSlice{typ: c.typ, refIDC: 2, frameNum: 1, pocLSB: 2, qpDelta: -2,
			deblockID: 2, alpha: 1, beta: -1}
		h, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if h.Type != c.want {
			t.Errorf("%s read as %v", c.name, h.Type)
		}
		if ref.QP != pps.InitQP-2 {
			t.Errorf("%s: QP = %d, want %d", c.name, ref.QP, pps.InitQP-2)
		}
		// The deblocking fields come AFTER the second quantiser, so finding them
		// where they were written is what says it was consumed.
		if ref.DeblockingIDC != 2 || ref.AlphaC0OffsetDiv2 != 1 || ref.BetaOffsetDiv2 != -1 {
			t.Errorf("%s: deblocking %d (%d,%d) -- the second quantiser was not consumed",
				c.name, ref.DeblockingIDC, ref.AlphaC0OffsetDiv2, ref.BetaOffsetDiv2)
		}
		// An SI slice states no reference list, like an I slice.
		if c.want == SliceSI && len(ref.ModifyL0) != 0 {
			t.Errorf("an SI slice gave %d list instructions", len(ref.ModifyL0))
		}
	}
}

// TestEveryMarkingOperationIsRead: each takes a different number of arguments, and
// reading the wrong number puts every operation after it in the wrong place.
func TestEveryMarkingOperationIsRead(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	ops := []MarkOp{
		{Operation: 1, Value: 3},
		{Operation: 2, Value: 5},
		{Operation: 3, Value: 1, Extra: 2},
		{Operation: 4, Value: 4},
		{Operation: 6, Value: 7},
	}
	s := refSlice{typ: 0, refIDC: 2, frameNum: 1, pocLSB: 2, qpDelta: 4,
		adaptive: true, marking: ops}
	_, ref, err := ParseSliceReferences(s.build(sps, pps), sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Marking) != len(ops) {
		t.Fatalf("%d operations, want %d", len(ref.Marking), len(ops))
	}
	for i := range ops {
		if ref.Marking[i] != ops[i] {
			t.Errorf("operation %d = %+v, want %+v", i, ref.Marking[i], ops[i])
		}
	}
	if ref.QP != pps.InitQP+4 {
		t.Errorf("QP = %d: the list was not consumed exactly", ref.QP)
	}
}

// TestAMarkingOperationThisDoesNotKnowIsRefused: the format defines one to six, and
// reading past an unknown one would take the rest of the header as rubbish.
func TestAMarkingOperationThisDoesNotKnowIsRefused(t *testing.T) {
	sps, pps := refSets()
	pps.WeightedPred = false
	w := &spsWriter{}
	w.ue(0)
	w.ue(0) // P
	w.ue(0)
	w.bits(1, int(sps.Log2MaxFrameNum))
	w.bits(2, int(sps.Log2MaxPOCLSB))
	w.flag(false) // no override
	w.flag(false) // no list modification
	w.flag(true)  // adaptive marking
	w.ue(7)       // no such operation
	w.bits(0xAB, 8)
	u := Unit{Type: UnitNonIDR, RefIDC: 2, Payload: w.data}
	if _, _, err := ParseSliceReferences(u, sps, pps); !errors.Is(err, ErrSliceHeader) {
		t.Errorf("err = %v, want ErrSliceHeader", err)
	}
}
