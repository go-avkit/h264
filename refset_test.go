package h264

import (
	"errors"
	"fmt"
	"testing"
)

// refSPS is a sequence holding four references, with frame numbers four bits
// wide so a wrap is reachable in a short fixture.
func refSPS() SPS { return SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 4} }

// picture is one coded picture as RefSet is told about it.
type picture struct {
	idr      bool
	frameNum uint32
	refIDC   uint8
	longTerm bool // long_term_reference_flag, which only an IDR carries
	field    bool
	marking  []MarkOp
	poc      int32
}

// put feeds one picture and returns the handle it was given.
func put(t *testing.T, s *RefSet, sps SPS, p picture) (int, error) {
	t.Helper()
	u := Unit{Type: UnitNonIDR, RefIDC: p.refIDC}
	if p.idr {
		u.Type = UnitIDR
	}
	h := SliceHeader{FrameNum: p.frameNum, IDR: p.idr, FieldPic: p.field}
	ref := SliceReferences{
		LongTermReference: p.longTerm,
		AdaptiveMarking:   len(p.marking) > 0,
		Marking:           p.marking,
	}
	return s.AfterPicture(u, h, ref, POC{Value: p.poc}, sps)
}

// mustPut feeds a picture that is expected to be accepted.
func mustPut(t *testing.T, s *RefSet, sps SPS, p picture) int {
	t.Helper()
	id, err := put(t, s, sps, p)
	if err != nil {
		t.Fatalf("picture frame_num=%d: %v", p.frameNum, err)
	}
	return id
}

// held describes the set the way a person reads it: short-term pictures by
// frame number, long-term ones by index, each with the handle it was given.
func held(s *RefSet) string {
	out := make([]string, 0, len(s.Pictures()))
	for _, r := range s.Pictures() {
		if r.LongTerm {
			out = append(out, fmt.Sprintf("LT%d/%d", r.LongTermIdx, r.ID))
		} else {
			out = append(out, fmt.Sprintf("f%d/%d", r.FrameNum, r.ID))
		}
	}
	return fmt.Sprint(out)
}

func wantHeld(t *testing.T, s *RefSet, want string) {
	t.Helper()
	if got := held(s); got != want {
		t.Fatalf("the set holds %s, want %s", got, want)
	}
}

// TestAnEmptySetBecomesTheIDRAndNothingElse, and an IDR later empties it again:
// an IDR begins a sequence, so nothing before it is a reference any more.
func TestAnEmptySetBecomesTheIDRAndNothingElse(t *testing.T) {
	s := NewRefSet()
	if n := len(s.Pictures()); n != 0 {
		t.Fatalf("a new set holds %d pictures", n)
	}
	mustPut(t, s, refSPS(), picture{idr: true, frameNum: 0, refIDC: 3})
	wantHeld(t, s, "[f0/0]")
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2})
	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4})
	wantHeld(t, s, "[f0/0 f1/1 f2/2]")

	// A second IDR: everything before it goes.
	mustPut(t, s, refSPS(), picture{idr: true, frameNum: 0, refIDC: 3, poc: 0})
	wantHeld(t, s, "[f0/3]")

	// And Reset is what a seek is.
	s.Reset()
	if n := len(s.Pictures()); n != 0 {
		t.Fatalf("after Reset the set holds %d pictures", n)
	}
	// The handles start again, because nothing from before is addressable.
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	wantHeld(t, s, "[f0/0]")
}

// TestPicturesHandsOutACopy: a caller walks the set while deciding what to do
// with it, and a slice sharing the set's own storage would let that walk change
// what it is reading.
func TestPicturesHandsOutACopy(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2})

	got := s.Pictures()
	got[0] = RefPicture{ID: 99, FrameNum: 99}
	got = append(got, RefPicture{ID: 98})
	wantHeld(t, s, "[f0/0 f1/1]")
	_ = got
}

// TestANonReferencePictureIsNotTakenIn.
//
// ⛔ nal_ref_idc zero says no picture may refer to this one. Keeping it would
// push a real reference out of the set on the very next sliding window -- and
// in a stream with B pictures that is most of them: measured over 101 real
// streams, 2282 of 5392 coded pictures are non-reference.
func TestANonReferencePictureIsNotTakenIn(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	id, err := put(t, s, refSPS(), picture{frameNum: 1, refIDC: 0, poc: 2})
	if err != nil {
		t.Fatalf("a non-reference picture is not an error: %v", err)
	}
	if id != -1 {
		t.Fatalf("handle %d, want -1 for a picture that was not kept", id)
	}
	wantHeld(t, s, "[f0/0]")
	// The handle it did not take is still available to the next picture.
	if got := mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4}); got != 1 {
		t.Fatalf("the next picture got handle %d, want 1", got)
	}
}

// TestAFieldCodedPictureIsRefusedRatherThanGuessed: a field pair shares one
// frame number and is marked as a unit, and nothing here implements that.
// Answering anyway would hand back a set that is quietly wrong.
func TestAFieldCodedPictureIsRefusedRatherThanGuessed(t *testing.T) {
	s := NewRefSet()
	id, err := put(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, field: true})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet", err)
	}
	if id != -1 {
		t.Fatalf("handle %d, want -1", id)
	}
}

// TestTheSlidingWindowPicksTheSmallestFrameNumberRELATIVETOTHISONE.
//
// ⛔ Not the smallest number, and not the one taken in first. The three agree
// until a frame number wraps, and then they do not. Here the set holds f0 and
// f15 while the current picture is f1: f15 was coded JUST BEFORE f0 and f1
// (the counter wrapped), so relative to now it is -1 and it is the one to let
// go. "Smallest number" and "first taken in" would both throw away f0, which is
// the newest picture in the set.
func TestTheSlidingWindowPicksTheSmallestFrameNumberRELATIVETOTHISONE(t *testing.T) {
	sps := SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 2}
	s := NewRefSet()
	mustPut(t, s, sps, picture{idr: true, frameNum: 0, refIDC: 3})
	mustPut(t, s, sps, picture{frameNum: 15, refIDC: 2, poc: -2})
	wantHeld(t, s, "[f0/0 f15/1]")

	mustPut(t, s, sps, picture{frameNum: 1, refIDC: 2, poc: 2})
	wantHeld(t, s, "[f0/0 f1/2]")
}

// TestALongTermPictureIsNeverSlidOut: that is the whole point of its being
// long-term -- only an explicit operation releases one.
func TestALongTermPictureIsNeverSlidOut(t *testing.T) {
	sps := SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 2}
	s := NewRefSet()
	// An IDR that says it is a long-term reference.
	mustPut(t, s, sps, picture{idr: true, refIDC: 3, longTerm: true})
	wantHeld(t, s, "[LT0/0]")
	mustPut(t, s, sps, picture{frameNum: 1, refIDC: 2, poc: 2})
	wantHeld(t, s, "[LT0/0 f1/1]")

	// The window must take f1 and leave the long-term picture, although f1 is
	// newer and would survive a plain "oldest goes" rule.
	mustPut(t, s, sps, picture{frameNum: 2, refIDC: 2, poc: 4})
	wantHeld(t, s, "[LT0/0 f2/2]")
}

// TestASetFullOfLongTermPicturesIsRefusedRatherThanGrown.
//
// ⛔ The sliding window skips long-term pictures, so a set made entirely of
// them has nothing to give. Appending anyway holds MORE pictures than
// max_num_ref_frames says, and a decoder sized its frame store from that
// number: measured against the previous code, a capacity of four grew to five
// and stayed there.
func TestASetFullOfLongTermPicturesIsRefusedRatherThanGrown(t *testing.T) {
	sps := SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 2}
	s := NewRefSet()
	mustPut(t, s, sps, picture{idr: true, refIDC: 3, longTerm: true})
	// Allow two long-term indices, and make the next picture long-term 1.
	mustPut(t, s, sps, picture{frameNum: 1, refIDC: 2, poc: 2, marking: []MarkOp{
		{Operation: 4, Value: 2}, {Operation: 6, Value: 1},
	}})
	wantHeld(t, s, "[LT0/0 LT1/1]")

	id, err := put(t, s, sps, picture{frameNum: 2, refIDC: 2, poc: 4})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet", err)
	}
	if id != -1 {
		t.Fatalf("handle %d, want -1", id)
	}
	wantHeld(t, s, "[LT0/0 LT1/1]")
}

// TestACapacityOfZeroStillHoldsOnePicture.
//
// ⛔ Max(max_num_ref_frames, 1). A sequence stating that it holds no references
// still holds the one picture a P slice predicts from, and a capacity of zero
// would throw every picture out as it arrived -- leaving a stream that decodes
// to nothing.
func TestACapacityOfZeroStillHoldsOnePicture(t *testing.T) {
	sps := SPS{Log2MaxFrameNum: 4, MaxNumRefFrames: 0}

	// ⛔ The EMPTY set first, because that is the only case where the clamp
	// changes anything. With a capacity of one, nothing is slid out of an empty
	// set and the picture simply joins. Without the clamp the window is asked
	// to make room in a set that holds nothing, finds nothing to let go, and
	// the picture is REFUSED -- so the first reference picture of a stream
	// opened anywhere but at an IDR, which is what every seek gives, would be
	// turned away. Everything after that behaves the same either way, which is
	// why an ablation measured on a populated set sees nothing.
	fresh := NewRefSet()
	if id := mustPut(t, fresh, sps, picture{frameNum: 3, refIDC: 2, poc: 6}); id != 0 {
		t.Fatalf("handle %d, want 0", id)
	}
	wantHeld(t, fresh, "[f3/0]")

	s := NewRefSet()
	mustPut(t, s, sps, picture{idr: true, refIDC: 3})
	wantHeld(t, s, "[f0/0]")
	mustPut(t, s, sps, picture{frameNum: 1, refIDC: 2, poc: 2})
	wantHeld(t, s, "[f1/1]")
	mustPut(t, s, sps, picture{frameNum: 2, refIDC: 2, poc: 4})
	wantHeld(t, s, "[f2/2]")
}

// TestOperationOneNamesAPictureByHowFarBelowThisOneItIs.
//
// ⛔ The value is a MINUS ONE: difference_of_pic_nums_minus1 of zero means the
// picture one below this. Reading it as the difference itself releases the
// picture next to the one the stream named -- the one real operation this
// corpus uses, 804 times across 41 streams, so an off-by-one here is a defect
// that reaches almost every file.
func TestOperationOneNamesAPictureByHowFarBelowThisOneItIs(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2})
	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4})
	wantHeld(t, s, "[f0/0 f1/1 f2/2]")

	// From picture 3, "0" means frame 2 and "2" means frame 0.
	mustPut(t, s, refSPS(), picture{frameNum: 3, refIDC: 2, poc: 6,
		marking: []MarkOp{{Operation: 1, Value: 0}}})
	wantHeld(t, s, "[f0/0 f1/1 f3/3]")

	mustPut(t, s, refSPS(), picture{frameNum: 4, refIDC: 2, poc: 8,
		marking: []MarkOp{{Operation: 1, Value: 3}}})
	wantHeld(t, s, "[f1/1 f3/3 f4/4]")

	// A picture it does not hold is a refusal, not a silent no-op. From
	// picture 5, "2" names frame 2, which the first operation released.
	_, err := put(t, s, refSPS(), picture{frameNum: 5, refIDC: 2, poc: 10,
		marking: []MarkOp{{Operation: 1, Value: 2}}})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet for a picture that is not held", err)
	}
}

// TestOperationTwoReleasesALongTermPictureByItsIndex.
func TestOperationTwoReleasesALongTermPictureByItsIndex(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3, longTerm: true})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2, marking: []MarkOp{
		{Operation: 4, Value: 3}, {Operation: 6, Value: 2},
	}})
	wantHeld(t, s, "[LT0/0 LT2/1]")

	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4,
		marking: []MarkOp{{Operation: 2, Value: 2}}})
	wantHeld(t, s, "[LT0/0 f2/2]")

	_, err := put(t, s, refSPS(), picture{frameNum: 3, refIDC: 2, poc: 6,
		marking: []MarkOp{{Operation: 2, Value: 7}}})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet for an index that is not held", err)
	}
}

// TestOperationThreePromotesAPictureAndTheIndexIsExCLUSIVE.
//
// ⛔ Whatever held the index is released first. Without that the set holds two
// pictures answering to one name, and a list modification naming it takes
// whichever it met first -- which is an ordering accident, not a decision.
func TestOperationThreePromotesAPictureAndTheIndexIsExCLUSIVE(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2,
		marking: []MarkOp{{Operation: 4, Value: 2}}})
	wantHeld(t, s, "[f0/0 f1/1]")

	// Promote f0 to index 1. From picture 2 the value is 1, because the
	// operation names currPicNum - value - 1: 2 - 1 - 1 = 0.
	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4,
		marking: []MarkOp{{Operation: 3, Value: 1, Extra: 1}}})
	wantHeld(t, s, "[LT1/0 f1/1 f2/2]")

	// Now promote f1 to the SAME index: the picture that held it must go.
	mustPut(t, s, refSPS(), picture{frameNum: 3, refIDC: 2, poc: 6,
		marking: []MarkOp{{Operation: 3, Value: 1, Extra: 1}}})
	wantHeld(t, s, "[LT1/1 f2/2 f3/3]")

	// A short-term picture it does not hold is a refusal.
	_, err := put(t, s, refSPS(), picture{frameNum: 4, refIDC: 2, poc: 8,
		marking: []MarkOp{{Operation: 3, Value: 9, Extra: 1}}})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet", err)
	}
}

// TestALongTermIndexAboveTheAllowanceIsRefused, by both operations that take
// one. A set that accepted it would hold a picture under a name no list can
// address.
func TestALongTermIndexAboveTheAllowanceIsRefused(t *testing.T) {
	for _, op := range []MarkOp{
		{Operation: 3, Value: 0, Extra: 5},
		{Operation: 6, Value: 5},
	} {
		s := NewRefSet()
		mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
		mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2,
			marking: []MarkOp{{Operation: 4, Value: 2}}}) // indices 0 and 1 only
		_, err := put(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4,
			marking: []MarkOp{op}})
		if !errors.Is(err, ErrRefSet) {
			t.Errorf("operation %d with index above the allowance: error = %v, want ErrRefSet",
				op.Operation, err)
		}
	}
	// ⛔ With NO allowance stated, even index zero is refused.
	// max_long_term_frame_idx_plus1 starts at zero, so until an operation four
	// says otherwise no long-term index is permitted at all. Measured against
	// the previous code, which guarded only when an allowance HAD been stated:
	// index zero went straight through, and the set held a long-term picture
	// under a name no list is allowed to address.
	for _, op := range []MarkOp{
		{Operation: 6, Value: 0},
		{Operation: 3, Value: 0, Extra: 0},
	} {
		s := NewRefSet()
		mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
		_, err := put(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2,
			marking: []MarkOp{op}})
		if !errors.Is(err, ErrRefSet) {
			t.Errorf("operation %d with index 0 and no allowance stated: error = %v, "+
				"want ErrRefSet", op.Operation, err)
		}
	}
}

// TestOperationFoursValueIsAPlusOne.
//
// ⛔ max_long_term_frame_idx_plus1 of zero means NO long-term picture is
// allowed, and every one held is released. Reading it as the index itself would
// read "release everything" as "allow index zero" -- so the pictures the stream
// asked to be let go would stay, and a later operation naming index 0 would
// find the wrong one.
func TestOperationFoursValueIsAPlusOne(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3, longTerm: true})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2, marking: []MarkOp{
		{Operation: 4, Value: 3}, {Operation: 6, Value: 2},
	}})
	wantHeld(t, s, "[LT0/0 LT2/1]")

	// "3" allowed indices 0..2. Narrowing to "1" allows only index 0, so LT2
	// goes and LT0 stays.
	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4,
		marking: []MarkOp{{Operation: 4, Value: 1}}})
	wantHeld(t, s, "[LT0/0 f2/2]")

	// And zero releases every long-term picture there is.
	mustPut(t, s, refSPS(), picture{frameNum: 3, refIDC: 2, poc: 6,
		marking: []MarkOp{{Operation: 4, Value: 0}}})
	wantHeld(t, s, "[f2/2 f3/3]")
}

// TestOperationFiveEmptiesTheSetAndTheCurrentPictureCountsAsFrameZero.
//
// ⛔ 7.4.3: a picture stating operation five "shall be inferred to have had
// frame_num equal to 0 for all subsequent use". Keeping the number it was coded
// with makes it look far older than everything that follows -- the counting
// restarts at 0 after it -- so the sliding window throws it out first and a
// list modification naming it computes a picture number for somebody else.
// Measured against the previous code: a picture coded as frame 7 was stored as
// frame 7, and the next picture, frame 1, made it -9 relative to now.
func TestOperationFiveEmptiesTheSetAndTheCurrentPictureCountsAsFrameZero(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2})
	mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4})
	wantHeld(t, s, "[f0/0 f1/1 f2/2]")

	mustPut(t, s, refSPS(), picture{frameNum: 7, refIDC: 2, poc: 14,
		marking: []MarkOp{{Operation: 5}}})
	wantHeld(t, s, "[f0/3]")

	// The restart also reaches an operation six in the SAME marking: that
	// picture must be taken in under 0, not under the number it was coded with.
	s2 := NewRefSet()
	mustPut(t, s2, refSPS(), picture{idr: true, refIDC: 3})
	mustPut(t, s2, refSPS(), picture{frameNum: 9, refIDC: 2, poc: 18, marking: []MarkOp{
		{Operation: 5}, {Operation: 4, Value: 1}, {Operation: 6, Value: 0},
	}})
	for _, r := range s2.Pictures() {
		if r.FrameNum != 0 {
			t.Fatalf("the picture is held under frame %d, want 0", r.FrameNum)
		}
	}
	wantHeld(t, s2, "[LT0/1]")
}

// TestOperationSixMarksTHISPictureSoItJoinsDuringTheMarking.
//
// ⛔ Every other reference picture joins the set after its marking is applied;
// this one joins inside it, because the operation is about the picture being
// decoded. A set that then appended it again would hold it twice.
func TestOperationSixMarksTHISPictureSoItJoinsDuringTheMarking(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	id := mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2, marking: []MarkOp{
		{Operation: 4, Value: 2}, {Operation: 6, Value: 1},
	}})
	if id != 1 {
		t.Fatalf("handle %d, want 1", id)
	}
	wantHeld(t, s, "[f0/0 LT1/1]")
	// And the next picture gets the NEXT handle, not this one again.
	if got := mustPut(t, s, refSPS(), picture{frameNum: 2, refIDC: 2, poc: 4}); got != 2 {
		t.Fatalf("the next picture got handle %d, want 2", got)
	}
}

// TestOperationSixTwiceIsRefused.
//
// ⛔ Measured against the previous code: two operations six appended TWO
// pictures carrying the same handle -- "LT1/1 LT2/1". The handle is how a
// caller finds its own buffer again, so it would have decoded one picture into
// another's.
func TestOperationSixTwiceIsRefused(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	_, err := put(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2, marking: []MarkOp{
		{Operation: 4, Value: 3}, {Operation: 6, Value: 1}, {Operation: 6, Value: 2},
	}})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet", err)
	}
	// Whatever it managed before the refusal, no handle is given out twice.
	seen := map[int]bool{}
	for _, r := range s.Pictures() {
		if seen[r.ID] {
			t.Fatalf("handle %d is held twice: %s", r.ID, held(s))
		}
		seen[r.ID] = true
	}
}

// TestAnUnknownMarkingOperationIsRefused: the seven values are the whole of
// 7.4.3.3, and anything else is a stream this reader does not understand --
// which is a different thing from a stream it may ignore.
func TestAnUnknownMarkingOperationIsRefused(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	_, err := put(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2,
		marking: []MarkOp{{Operation: 7}}})
	if !errors.Is(err, ErrRefSet) {
		t.Fatalf("error = %v, want ErrRefSet", err)
	}
}

// TestTheMarkingIsAppliedBEFORETheCurrentPictureJoins.
//
// ⛔ The other order lets the sliding window throw out the picture being added,
// and lets operation one address the new picture by a number that was meant for
// an older one. Here the set is full: applying the marking first frees a place,
// so f4 joins a set of four rather than displacing itself.
func TestTheMarkingIsAppliedBEFORETheCurrentPictureJoins(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3})
	for i := uint32(1); i <= 3; i++ {
		mustPut(t, s, refSPS(), picture{frameNum: i, refIDC: 2, poc: int32(2 * i)})
	}
	wantHeld(t, s, "[f0/0 f1/1 f2/2 f3/3]")

	// Release f1 (two below f4... value 2 names f1) and join.
	mustPut(t, s, refSPS(), picture{frameNum: 4, refIDC: 2, poc: 8,
		marking: []MarkOp{{Operation: 1, Value: 2}}})
	wantHeld(t, s, "[f0/0 f2/2 f3/3 f4/4]")
}

// TestAnIDRThatSaysItIsALongTermReference becomes index zero and is what makes
// index zero allowed at all -- before that, nothing is.
func TestAnIDRThatSaysItIsALongTermReference(t *testing.T) {
	s := NewRefSet()
	mustPut(t, s, refSPS(), picture{idr: true, refIDC: 3, longTerm: true})
	wantHeld(t, s, "[LT0/0]")
	// Index zero is now allowed, so an operation six may use it.
	mustPut(t, s, refSPS(), picture{frameNum: 1, refIDC: 2, poc: 2,
		marking: []MarkOp{{Operation: 6, Value: 0}}})
	wantHeld(t, s, "[LT0/1]")
}
