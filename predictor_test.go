package h264

import (
	"errors"
	"fmt"
	"testing"
)

// predSPS is a sequence with four references, four-bit frame numbers and the
// order count type Predictor derives.
func predSPS() SPS {
	return SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 4, Log2MaxPOCLSB: 6, POCType: 0}
}

// coded is one coded picture as a stream presents it.
type coded struct {
	idr      bool
	frameNum uint32
	refIDC   uint8
	kind     SliceType
	pocLSB   uint32
	field    bool
	activeL0 uint32
	activeL1 uint32
	modifyL0 []RefListOp
	modifyL1 []RefListOp
	marking  []MarkOp
}

func offer(t *testing.T, p *Predictor, c coded) (Prediction, error) {
	t.Helper()
	u := Unit{Type: UnitNonIDR, RefIDC: c.refIDC}
	if c.idr {
		u.Type = UnitIDR
	}
	h := SliceHeader{
		FrameNum: c.frameNum, IDR: c.idr, Type: c.kind,
		POCLSB: c.pocLSB, FieldPic: c.field,
	}
	active0, active1 := c.activeL0, c.activeL1
	if active0 == 0 {
		active0 = 2
	}
	if active1 == 0 {
		active1 = 2
	}
	ref := SliceReferences{
		NumRefIdxL0Active: active0, NumRefIdxL1Active: active1,
		ModifyL0: c.modifyL0, ModifyL1: c.modifyL1,
		AdaptiveMarking: len(c.marking) > 0, Marking: c.marking,
	}
	return p.Picture(u, h, ref, predSPS(), PPS{})
}

func mustOffer(t *testing.T, p *Predictor, c coded) Prediction {
	t.Helper()
	got, err := offer(t, p, c)
	if err != nil {
		t.Fatalf("picture frame_num=%d: %v", c.frameNum, err)
	}
	return got
}

// names describes a reference list as frame numbers and handles.
func names(list []RefPicture) string {
	out := make([]string, 0, len(list))
	for _, r := range list {
		out = append(out, fmt.Sprintf("f%d/%d", r.FrameNum, r.ID))
	}
	return fmt.Sprint(out)
}

// TestAPictureDoesNotPredictFromItself.
//
// ⛔ This is the whole reason Predictor exists. A picture predicts from the set
// as it stood BEFORE the picture joined it. Take the picture in first and it is
// the newest reference in its own list -- usually its FIRST entry, since a P
// slice orders by descending picture number -- and the stream decodes to a
// plausible, wrong image rather than to an error. Nothing in the signatures of
// the five steps says which order they go in; three separate harnesses in this
// project had to rediscover it from doc comments.
func TestAPictureDoesNotPredictFromItself(t *testing.T) {
	p := NewPredictor()
	first := mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	if first.Handle != 0 {
		t.Fatalf("the IDR got handle %d, want 0", first.Handle)
	}
	if len(first.L0) != 0 || len(first.L1) != 0 {
		t.Fatalf("an I slice predicts from %s and %s, want neither",
			names(first.L0), names(first.L1))
	}

	second := mustOffer(t, p, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 2})
	if got := names(second.L0); got != "[f0/0]" {
		t.Fatalf("the second picture predicts from %s; it must be the IDR alone, "+
			"and above all not itself", got)
	}
	if second.Handle != 1 {
		t.Fatalf("handle %d, want 1", second.Handle)
	}
	// It joined AFTER, so the next picture sees both.
	third := mustOffer(t, p, coded{frameNum: 2, refIDC: 2, kind: SliceP, pocLSB: 4})
	if got := names(third.L0); got != "[f1/1 f0/0]" {
		t.Fatalf("the third picture predicts from %s, want [f1/1 f0/0] "+
			"(descending picture number)", got)
	}
}

// TestABSliceGetsBothListsAndTheyDiffer: list 1 is not list 0, and a caller
// receiving one for the other would predict backwards.
func TestABSliceGetsBothListsAndTheyDiffer(t *testing.T) {
	p := NewPredictor()
	mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	mustOffer(t, p, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 8})
	// A B picture sitting between them in display order.
	b := mustOffer(t, p, coded{frameNum: 2, refIDC: 2, kind: SliceB, pocLSB: 4})
	if len(b.L0) == 0 || len(b.L1) == 0 {
		t.Fatalf("a B slice got L0=%s L1=%s; it needs both", names(b.L0), names(b.L1))
	}
	if names(b.L0) == names(b.L1) {
		t.Fatalf("both lists are %s; for a picture with a reference on each side "+
			"they must differ", names(b.L0))
	}
}

// TestTheListModificationsAreApplied, on each list, against the set rather
// than against the list being rewritten.
func TestTheListModificationsAreApplied(t *testing.T) {
	p := NewPredictor()
	mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	for i := uint32(1); i <= 3; i++ {
		mustOffer(t, p, coded{frameNum: i, refIDC: 2, kind: SliceP, pocLSB: 2 * i})
	}
	// From picture 4, "subtract 1" names frame 3 and then "subtract 2" names
	// frame 1 -- the second of which the first instruction has already pushed
	// off the end of a two-entry list.
	got := mustOffer(t, p, coded{frameNum: 4, refIDC: 2, kind: SliceP, pocLSB: 8,
		activeL0: 2, modifyL0: []RefListOp{{Kind: 0, Value: 0}, {Kind: 0, Value: 1}}})
	if want := "[f3/3 f1/1]"; names(got.L0) != want {
		t.Fatalf("list 0 is %s, want %s", names(got.L0), want)
	}

	// And on list 1, through a B slice.
	b := mustOffer(t, p, coded{frameNum: 5, refIDC: 2, kind: SliceB, pocLSB: 10,
		activeL1: 1, modifyL1: []RefListOp{{Kind: 0, Value: 0}}})
	if want := "[f4/4]"; names(b.L1) != want {
		t.Fatalf("list 1 is %s, want %s", names(b.L1), want)
	}
}

// TestANonReferencePictureGetsListsButNoHandle: a B picture nothing refers to
// still has to be decoded, so it still needs its lists.
func TestANonReferencePictureGetsListsButNoHandle(t *testing.T) {
	p := NewPredictor()
	mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	mustOffer(t, p, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 8})
	got := mustOffer(t, p, coded{frameNum: 2, refIDC: 0, kind: SliceB, pocLSB: 4})
	if got.Handle != -1 {
		t.Fatalf("handle %d, want -1 for a picture that is not a reference", got.Handle)
	}
	// ⛔ -1 and not 0. Zero is a real handle -- the first picture of the stream
	// has it -- so a composition that reported it for a picture it did not keep
	// would send a caller to somebody else's buffer.
	if len(got.L0) == 0 {
		t.Fatal("it was given no list to predict from")
	}
	if n := len(p.Held()); n != 2 {
		t.Fatalf("the set holds %d pictures, want the 2 from before", n)
	}
}

// TestEachStepsRefusalReachesTheCaller.
//
// ⛔ Four different steps can refuse, and a composition that swallowed any of
// them would hand back a Prediction built on a step that did not happen.
func TestEachStepsRefusalReachesTheCaller(t *testing.T) {
	// The order count: a sequence shaped in a way this does not derive.
	odd := NewPredictor()
	u := Unit{Type: UnitIDR, RefIDC: 3}
	h := SliceHeader{IDR: true, Type: SliceI}
	_, err := odd.Picture(u, h, SliceReferences{},
		SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 4, POCType: 1}, PPS{})
	if !errors.Is(err, ErrPOCType) {
		t.Errorf("the order count's refusal became %v, want ErrPOCType", err)
	}

	// The initial lists: a field-coded picture, which they do not derive.
	//
	// ⛔ It asserts WHICH step refused. The reference set refuses a field-coded
	// picture too, so a composition that swallowed the lists' refusal would
	// still return an error -- from a later step, about a different thing, after
	// building lists from a step that did not happen. Only ErrRefLists says the
	// refusal came from where it should.
	field := NewPredictor()
	_, err = offer(t, field, coded{frameNum: 1, refIDC: 2, kind: SliceP, field: true})
	if !errors.Is(err, ErrRefLists) {
		t.Errorf("a field-coded picture became %v, want ErrRefLists", err)
	}
	if errors.Is(err, ErrRefSet) {
		t.Errorf("the refusal came from the reference set (%v); the lists refuse first", err)
	}

	// A modification naming a picture the set does not hold.
	absent := NewPredictor()
	mustOffer(t, absent, coded{idr: true, refIDC: 3, kind: SliceI})
	_, err = offer(t, absent, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 2,
		modifyL0: []RefListOp{{Kind: 0, Value: 9}}})
	if !errors.Is(err, ErrRefLists) {
		t.Errorf("a modification naming nothing became %v, want ErrRefLists", err)
	}

	// The same, on list 1.
	absent1 := NewPredictor()
	mustOffer(t, absent1, coded{idr: true, refIDC: 3, kind: SliceI})
	mustOffer(t, absent1, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 8})
	_, err = offer(t, absent1, coded{frameNum: 2, refIDC: 2, kind: SliceB, pocLSB: 4,
		modifyL1: []RefListOp{{Kind: 0, Value: 9}}})
	if !errors.Is(err, ErrRefLists) {
		t.Errorf("a list-1 modification naming nothing became %v, want ErrRefLists", err)
	}

	// The marking: an operation naming a picture the set does not hold.
	marked := NewPredictor()
	mustOffer(t, marked, coded{idr: true, refIDC: 3, kind: SliceI})
	_, err = offer(t, marked, coded{frameNum: 1, refIDC: 2, kind: SliceP, pocLSB: 2,
		marking: []MarkOp{{Operation: 1, Value: 9}}})
	if !errors.Is(err, ErrRefSet) {
		t.Errorf("the marking's refusal became %v, want ErrRefSet", err)
	}
}

// TestResetForgetsBothHalvesOfTheState: the order count and the set are both
// carried from picture to picture, and a seek that cleared only one would
// derive counts against a sequence that is no longer there.
func TestResetForgetsBothHalvesOfTheState(t *testing.T) {
	p := NewPredictor()
	mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	for i := uint32(1); i <= 3; i++ {
		mustOffer(t, p, coded{frameNum: i, refIDC: 2, kind: SliceP, pocLSB: 2 * i})
	}
	if n := len(p.Held()); n != 4 {
		t.Fatalf("the set holds %d pictures, want 4", n)
	}

	// ⛔ Two things make this witness see anything at all.
	//
	// The picture after the Reset must be a NON-IDR one: an IDR restarts the
	// count whatever the counter held, so feeding one passes with the count
	// left untouched and the half of Reset that clears it has no witness.
	//
	// And the two least significant bits below are not just any pair. The
	// counter's wrap rule is asymmetric -- a rise of MORE than half a cycle is
	// read as a wrap down, a fall of AT LEAST half as a wrap up -- so most
	// pairs give a reset counter and a stale one the same answer, and the first
	// pair tried did exactly that. All 3969 pairs were walked: 1023 separate
	// the two cases and this is one of them. Seeding at 1 leaves prevPicOrderCntLsb
	// at 1, and 33 is a rise of 32 from there, which is NOT more than half of 64
	// -- so a stale counter answers 33, while a cleared one sees a rise of 33
	// from zero, reads it as a wrap, and answers -31.
	mustOffer(t, p, coded{frameNum: 4, refIDC: 2, kind: SliceP, pocLSB: 1})

	p.Reset()
	if n := len(p.Held()); n != 0 {
		t.Fatalf("after Reset the set holds %d pictures", n)
	}
	again := mustOffer(t, p, coded{frameNum: 7, refIDC: 2, kind: SliceP, pocLSB: 33})
	if again.POC.Value != -31 {
		t.Fatalf("the first picture after Reset counts %d, want -31: a counter "+
			"still holding 1 answers 33", again.POC.Value)
	}
	if again.Handle != 0 {
		t.Fatalf("handle %d, want 0: nothing from before is addressable", again.Handle)
	}
	if len(again.L0) != 0 {
		t.Fatalf("it predicts from %s; the set was emptied", names(again.L0))
	}
}

// TestAPredictionCarriesTheListTheSliceCanIndex.
//
// ⛔ Not the ordering it was built from. Clause 8.2.4.2 orders every reference
// picture held, and 8.2.4.2.1 then discards "the extra entries beyond position
// num_ref_idx_lX_active_minus1". Measured on a real 14-picture stream whose
// every slice declares ONE active reference: L0 and L1 each carried three
// entries. The first was right, so a caller reading L0[ref_idx] saw nothing
// wrong -- while one asking len(L0) was told the slice had three references to
// choose from when the bitstream gives ref_idx exactly one value.
//
// ⛔ And it was INCONSISTENT, which is worse than either answer on its own:
// ApplyRefListOps truncates, so the length depended on whether the slice
// happened to state a modification. Two slices of the same picture could
// disagree about how long the list is.
func TestAPredictionCarriesTheListTheSliceCanIndex(t *testing.T) {
	p := NewPredictor()
	mustOffer(t, p, coded{idr: true, refIDC: 3, kind: SliceI})
	for i := uint32(1); i <= 3; i++ {
		mustOffer(t, p, coded{frameNum: i, refIDC: 2, kind: SliceP, pocLSB: 2 * i})
	}
	if n := len(p.Held()); n != 4 {
		t.Fatalf("the set holds %d pictures, want 4", n)
	}

	// Four pictures held, one active reference declared.
	one := mustOffer(t, p, coded{frameNum: 4, refIDC: 2, kind: SliceP, pocLSB: 8,
		activeL0: 1, activeL1: 1})
	if len(one.L0) != 1 {
		t.Fatalf("list 0 holds %d entries for a slice declaring 1: %s",
			len(one.L0), names(one.L0))
	}
	if got := names(one.L0); got != "[f3/3]" {
		t.Fatalf("list 0 is %s, want [f3/3] -- the nearest reference", got)
	}

	// A B slice declaring one on each side.
	b := mustOffer(t, p, coded{frameNum: 5, refIDC: 2, kind: SliceB, pocLSB: 20,
		activeL0: 1, activeL1: 1})
	if len(b.L0) != 1 || len(b.L1) != 1 {
		t.Fatalf("a B slice declaring 1/1 got %d/%d entries: %s and %s",
			len(b.L0), len(b.L1), names(b.L0), names(b.L1))
	}

	// ⛔ A list SHORTER than the slice declares is left alone. 8.2.4.2.1 leaves
	// those entries unspecified -- there is no picture to put there -- and
	// padding would invent a reference the stream never named.
	fresh := NewPredictor()
	mustOffer(t, fresh, coded{idr: true, refIDC: 3, kind: SliceI})
	short := mustOffer(t, fresh, coded{frameNum: 1, refIDC: 2, kind: SliceP,
		pocLSB: 2, activeL0: 4})
	if len(short.L0) != 1 {
		t.Fatalf("one picture is held and the slice declares 4; list 0 holds %d: %s",
			len(short.L0), names(short.L0))
	}

	// And the modified path agrees with the unmodified one about the length,
	// which is the inconsistency this fixes.
	p2 := NewPredictor()
	mustOffer(t, p2, coded{idr: true, refIDC: 3, kind: SliceI})
	for i := uint32(1); i <= 3; i++ {
		mustOffer(t, p2, coded{frameNum: i, refIDC: 2, kind: SliceP, pocLSB: 2 * i})
	}
	plain := mustOffer(t, p2, coded{frameNum: 4, refIDC: 2, kind: SliceP,
		pocLSB: 8, activeL0: 2})
	modified := mustOffer(t, p2, coded{frameNum: 5, refIDC: 2, kind: SliceP,
		pocLSB: 10, activeL0: 2, modifyL0: []RefListOp{{Kind: 0, Value: 0}}})
	if len(plain.L0) != len(modified.L0) {
		t.Fatalf("a slice stating no modification gets %d entries and one stating "+
			"a modification gets %d; both declare 2", len(plain.L0), len(modified.L0))
	}
}
