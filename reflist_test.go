// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"strings"
	"testing"
)

// pic makes a short-term reference at a given count, identified by it so that a
// list reads as the counts it holds.
func pic(poc int32, frameNum uint32) RefPicture {
	return RefPicture{ID: int(poc), POC: poc, FrameNum: frameNum}
}

func longPic(id int, idx uint32) RefPicture {
	return RefPicture{ID: id, LongTerm: true, LongTermIdx: idx}
}

// order renders a list by the counts it holds, so a test says what it means.
func order(list []RefPicture) string {
	var b strings.Builder
	for i, r := range list {
		if i > 0 {
			b.WriteByte(' ')
		}
		if r.LongTerm {
			b.WriteString("L")
			b.WriteString(itoa32(int32(r.LongTermIdx)))
			continue
		}
		b.WriteString(itoa32(r.POC))
	}
	return b.String()
}

func itoa32(v int32) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var d [12]byte
	i := len(d)
	for v > 0 {
		i--
		d[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		d[i] = '-'
	}
	return string(d[i:])
}

// TestAPSliceTakesItsMostRecentReferenceFirst: short-term references in
// descending picture number, then long-term in ascending index.
func TestAPSliceTakesItsMostRecentReferenceFirst(t *testing.T) {
	refs := []RefPicture{pic(2, 1), pic(8, 4), pic(4, 2), longPic(99, 1), longPic(98, 0)}
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceP}, refs, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(l0), "8 4 2 L0 L1"; got != want {
		t.Errorf("L0 = %q, want %q", got, want)
	}
	if l1 != nil {
		t.Errorf("a P slice gave an L1 of %q", order(l1))
	}
}

// TestAFrameNumberWrapDoesNotSendTheNewestReferenceToTheBack.
//
// ⛔ Frame numbers wrap. Without expressing them relative to the current one, the
// picture whose number wrapped to zero sorts FIRST instead of last, and a P slice
// then predicts from the oldest reference it holds while believing it took the
// newest.
func TestAFrameNumberWrapDoesNotSendTheNewestReferenceToTheBack(t *testing.T) {
	// The cycle is 16. The current picture is frame 1, having wrapped; frames 14
	// and 15 are older than it.
	refs := []RefPicture{
		{ID: 14, POC: 28, FrameNum: 14},
		{ID: 15, POC: 30, FrameNum: 15},
		{ID: 0, POC: 32, FrameNum: 0},
	}
	l0, _, err := InitialRefLists(SliceHeader{Type: SliceP}, refs, 34, 1, 16)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int
	for _, r := range l0 {
		ids = append(ids, r.ID)
	}
	// Newest first: frame 0 (just before the current 1), then 15, then 14.
	if len(ids) != 3 || ids[0] != 0 || ids[1] != 15 || ids[2] != 14 {
		t.Errorf("order by identity = %v, want [0 15 14]", ids)
	}
}

// TestTheTwoListsOfABSliceAreNotEachOthersReverse.
//
// ⛔ L0 runs backwards through the pictures BEFORE this one and then forwards
// through those after; L1 runs forwards through those after and then backwards
// through those before. A reader that built one and reversed it would be right
// only when the references are symmetric about the current picture -- the common
// case, and so the one that hides the error.
func TestTheTwoListsOfABSliceAreNotEachOthersReverse(t *testing.T) {
	// Asymmetric on purpose: two before, three after.
	refs := []RefPicture{pic(4, 2), pic(8, 4), pic(16, 8), pic(20, 10), pic(24, 12)}
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceB}, refs, 12, 6, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(l0), "8 4 16 20 24"; got != want {
		t.Errorf("L0 = %q, want %q", got, want)
	}
	if got, want := order(l1), "16 20 24 8 4"; got != want {
		t.Errorf("L1 = %q, want %q", got, want)
	}
	// And neither is the other reversed, which is the claim.
	reversed := make([]RefPicture, len(l0))
	for i := range l0 {
		reversed[i] = l0[len(l0)-1-i]
	}
	if order(reversed) == order(l1) {
		t.Error("L1 came out as L0 reversed, so this fixture cannot tell the two rules apart")
	}
}

// TestIdenticalListsHaveTheirFirstTwoSwapped.
//
// ⛔ This is the rule that is easy to miss. When the two lists come out identical
// and hold more than one picture, the first two entries of L1 are swapped. Without
// it a bi-predicted block takes the same picture twice and the second prediction
// adds nothing -- which looks like a plain single prediction and is wrong only by
// how much it fails to blur. It is reached whenever every reference sits on one
// side of the current picture, which is what the pictures at the end of a sequence
// do.
func TestIdenticalListsHaveTheirFirstTwoSwapped(t *testing.T) {
	// Every reference before the current picture: both lists would be "8 4 2".
	refs := []RefPicture{pic(2, 1), pic(4, 2), pic(8, 4)}
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceB}, refs, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(l0), "8 4 2"; got != want {
		t.Errorf("L0 = %q, want %q", got, want)
	}
	if got, want := order(l1), "4 8 2"; got != want {
		t.Errorf("L1 = %q, want %q -- the first two were not swapped", got, want)
	}
	// With ONE reference there is nothing to swap, and swapping anyway would read
	// past the end.
	one := []RefPicture{pic(4, 2)}
	l0, l1, err = InitialRefLists(SliceHeader{Type: SliceB}, one, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if order(l0) != "4" || order(l1) != "4" {
		t.Errorf("with one reference: L0 %q L1 %q", order(l0), order(l1))
	}
	// And when the lists genuinely differ, nothing is swapped.
	both := []RefPicture{pic(4, 2), pic(16, 8)}
	l0, l1, err = InitialRefLists(SliceHeader{Type: SliceB}, both, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if order(l0) != "4 16" || order(l1) != "16 4" {
		t.Errorf("lists that differ: L0 %q L1 %q", order(l0), order(l1))
	}
}

// TestAReferenceAtTheSameInstantCountsAsAfter: the split is "before" against
// "not before", so a reference sharing the current count goes with those after it.
// Putting it with those before would place it first in L0, ahead of the genuinely
// nearest picture.
func TestAReferenceAtTheSameInstantCountsAsAfter(t *testing.T) {
	refs := []RefPicture{pic(4, 2), pic(10, 5), pic(16, 8)}
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceB}, refs, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(l0), "4 10 16"; got != want {
		t.Errorf("L0 = %q, want %q", got, want)
	}
	if got, want := order(l1), "10 16 4"; got != want {
		t.Errorf("L1 = %q, want %q", got, want)
	}
}

func TestAnISliceAsksForNothingAndIsNotAnError(t *testing.T) {
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceI}, []RefPicture{pic(4, 2)}, 10, 5, 16)
	if err != nil {
		t.Fatalf("an I slice was refused: %v", err)
	}
	if l0 != nil || l1 != nil {
		t.Errorf("an I slice gave %q and %q", order(l0), order(l1))
	}
}

// TestAFieldCodedPictureIsRefused: a field interleaves the two parities and splits
// every reference in two, which is a different derivation. Of 158 sequences
// measured in one library, every one was frame-only -- and returning a
// frame-shaped answer for a field would be wrong in a way nothing here could see.
func TestAFieldCodedPictureIsRefused(t *testing.T) {
	_, _, err := InitialRefLists(SliceHeader{Type: SliceP, FieldPic: true},
		[]RefPicture{pic(4, 2)}, 10, 5, 16)
	if !errors.Is(err, ErrRefLists) {
		t.Errorf("err = %v, want ErrRefLists", err)
	}
}

// TestAnInstructionMovesOnePictureAndClosesTheGapBehindIt.
//
// The format shifts everything from the named position up by one, puts the picture
// there, and then closes the gap its other copy leaves. The list is one longer
// while that happens and is cut back afterwards.
func TestAnInstructionMovesOnePictureAndClosesTheGapBehindIt(t *testing.T) {
	// Initial list, newest first: 8 6 4 2, and the slice asks for 4 at the front.
	list := []RefPicture{pic(8, 4), pic(6, 3), pic(4, 2), pic(2, 1)}
	// The current picture is frame 5; 4 has frame number 2, so the difference from
	// the prediction (5) is 3, stated as a minus-one of 2.
	ops := []RefListOp{{Kind: 0, Value: 2}}
	got, err := ApplyRefListOps(list, list, ops, 4, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if want := "4 8 6 2"; order(got) != want {
		t.Errorf("= %q, want %q", order(got), want)
	}
	if len(got) != 4 {
		t.Errorf("the list came back %d long, want 4", len(got))
	}
}

// TestTheDifferenceBetweenInstructionsIsCumulative: each instruction states a
// difference from the picture the LAST one named, not from the current picture.
func TestTheDifferenceBetweenInstructionsIsCumulative(t *testing.T) {
	list := []RefPicture{pic(8, 4), pic(6, 3), pic(4, 2), pic(2, 1)}
	// From 5: minus 1 names frame 4. Then from 4: minus 2 names frame 2.
	ops := []RefListOp{{Kind: 0, Value: 0}, {Kind: 0, Value: 1}}
	got, err := ApplyRefListOps(list, list, ops, 4, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if want := "8 4 6 2"; order(got) != want {
		t.Errorf("= %q, want %q", order(got), want)
	}
	// Read as differences from the current picture instead, the second instruction
	// would name frame 3 and give a different list -- so this fixture tells the
	// two readings apart.
	if order(got) == "8 6 4 2" {
		t.Error("the list is unchanged, so the instructions were not applied")
	}
}

// TestAnInstructionCanNameAPictureAcrossTheWrap: the difference wraps against
// MaxPicNum in both directions, and a reader that did not wrap would name a
// picture that is not held.
func TestAnInstructionCanNameAPictureAcrossTheWrap(t *testing.T) {
	// The cycle is 16, the current picture is frame 1, and frame 15 is two before
	// it. From a prediction of 1, minus 2 gives -1, which wraps to 15.
	list := []RefPicture{
		{ID: 0, POC: 32, FrameNum: 0},
		{ID: 15, POC: 30, FrameNum: 15},
		{ID: 14, POC: 28, FrameNum: 14},
	}
	ops := []RefListOp{{Kind: 0, Value: 1}}
	got, err := ApplyRefListOps(list, list, ops, 3, 1, 16)
	if err != nil {
		t.Fatalf("the wrap was not followed: %v", err)
	}
	if got[0].ID != 15 {
		var ids []int
		for _, r := range got {
			ids = append(ids, r.ID)
		}
		t.Errorf("order by identity = %v, want 15 first", ids)
	}
}

func TestALongTermInstructionNamesItsOwnIndex(t *testing.T) {
	list := []RefPicture{pic(8, 4), longPic(50, 2), longPic(51, 0)}
	got, err := ApplyRefListOps(list, list, []RefListOp{{Kind: 2, Value: 2}}, 3, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].ID != 50 {
		t.Errorf("first entry is %d, want the long-term picture of index 2", got[0].ID)
	}
}

func TestRefListRefusals(t *testing.T) {
	list := []RefPicture{pic(8, 4), pic(6, 3)}
	t.Run("a picture that is not held", func(t *testing.T) {
		// From 5, minus 4 names frame 1, which is not in the list.
		_, err := ApplyRefListOps(list, list, []RefListOp{{Kind: 0, Value: 3}}, 2, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("a long-term index that is not held", func(t *testing.T) {
		_, err := ApplyRefListOps(list, list, []RefListOp{{Kind: 2, Value: 7}}, 2, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("an instruction this does not read", func(t *testing.T) {
		_, err := ApplyRefListOps(list, list, []RefListOp{{Kind: 4}}, 2, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("more instructions than positions", func(t *testing.T) {
		ops := []RefListOp{{Kind: 0, Value: 0}, {Kind: 1, Value: 0}, {Kind: 0, Value: 0}}
		_, err := ApplyRefListOps(list, list, ops, 1, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("no reference at all", func(t *testing.T) {
		_, err := ApplyRefListOps(nil, list, []RefListOp{{Kind: 0}}, 2, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("a negative count", func(t *testing.T) {
		if _, err := ApplyRefListOps(list, list, nil, -1, 5, 16); !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
	t.Run("no active reference wants no list", func(t *testing.T) {
		got, err := ApplyRefListOps(list, list, nil, 0, 5, 16)
		if err != nil || got != nil {
			t.Errorf("= %q, %v", order(got), err)
		}
	})
	t.Run("an unknown slice type", func(t *testing.T) {
		_, _, err := InitialRefLists(SliceHeader{Type: SliceType(9)}, list, 10, 5, 16)
		if !errors.Is(err, ErrRefLists) {
			t.Errorf("err = %v, want ErrRefLists", err)
		}
	})
}

// TestAListShorterThanItsActiveCountRepeats.
//
// ⛔ A slice may name more active references than the stream holds. The initial
// list repeats rather than holding gaps, because an entry that is nothing would be
// read as a picture of nothing -- a block of grey where a prediction belongs.
func TestAListShorterThanItsActiveCountRepeats(t *testing.T) {
	list := []RefPicture{pic(8, 4), pic(6, 3)}
	got, err := ApplyRefListOps(list, list, nil, 4, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("%d entries, want 4", len(got))
	}
	for i, r := range got {
		if r.ID != list[i%2].ID {
			t.Errorf("entry %d is %d, want the list repeated", i, r.ID)
		}
	}
}

// TestABSliceTakesItsLongTermReferencesLast: in both lists, after every short-term
// one, in ascending index. They do not sort by count -- a long-term picture's count
// does not measure a distance in time.
func TestABSliceTakesItsLongTermReferencesLast(t *testing.T) {
	refs := []RefPicture{pic(4, 2), pic(16, 8), longPic(70, 3), longPic(71, 1)}
	l0, l1, err := InitialRefLists(SliceHeader{Type: SliceB}, refs, 10, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := order(l0), "4 16 L1 L3"; got != want {
		t.Errorf("L0 = %q, want %q", got, want)
	}
	if got, want := order(l1), "16 4 L1 L3"; got != want {
		t.Errorf("L1 = %q, want %q", got, want)
	}
}

// TestAnInstructionCanAlsoNameAPictureForward.
//
// ⛔ Instruction kind one ADDS to the prediction where kind zero subtracts, and it
// wraps at the top of the cycle rather than the bottom. A reader handling only the
// subtracting kind would place the wrong picture for every list that walks forward.
func TestAnInstructionCanAlsoNameAPictureForward(t *testing.T) {
	list := []RefPicture{pic(2, 1), pic(4, 2), pic(6, 3), pic(8, 4)}
	// From a prediction of 5, kind one with a minus-one of 0 names frame 6 -- which
	// is above the current picture, so its picture number is 6 - 16 = -10 and it is
	// not held. Walking DOWN first and then up is how a real list reaches forward.
	// From 5: minus 3 names frame 2; then plus 1 names frame 3.
	ops := []RefListOp{{Kind: 0, Value: 2}, {Kind: 1, Value: 0}}
	got, err := ApplyRefListOps(list, list, ops, 4, 5, 16)
	if err != nil {
		t.Fatal(err)
	}
	if want := "4 6 2 8"; order(got) != want {
		t.Errorf("= %q, want %q", order(got), want)
	}
}

// TestTheForwardDifferenceWrapsAtTheTopOfTheCycle: adding past MaxPicNum comes
// back round to zero, and a reader that did not wrap would name nothing held.
func TestTheForwardDifferenceWrapsAtTheTopOfTheCycle(t *testing.T) {
	// The cycle is 16, the current picture is frame 14. Walking down to 13 then
	// forward by 3 reaches 16, which wraps to 0 -- held here as the newest.
	list := []RefPicture{
		{ID: 13, POC: 26, FrameNum: 13},
		{ID: 12, POC: 24, FrameNum: 12},
		{ID: 0, POC: 32, FrameNum: 0},
	}
	ops := []RefListOp{{Kind: 0, Value: 0}, {Kind: 1, Value: 2}}
	got, err := ApplyRefListOps(list, list, ops, 3, 14, 16)
	if err != nil {
		t.Fatalf("the forward wrap was not followed: %v", err)
	}
	if got[0].ID != 13 || got[1].ID != 0 {
		var ids []int
		for _, r := range got {
			ids = append(ids, r.ID)
		}
		t.Errorf("order by identity = %v, want 13 then 0", ids)
	}
}

// TestAPictureDisplacedFromTheListIsStillAReference.
//
// ⛔ A list modification instruction names a picture in the REFERENCE SET, not in
// the list it is rewriting. The list is only num_ref_idx_active + 1 entries long,
// and placing a picture shifts the rest down -- which pushes the last entry off
// the end. A second instruction naming that entry then finds nothing, although the
// set still holds it, and §8.2.4.3.1 selects "the short-term reference picture with
// PicNum equal to picNumLX" from the pictures marked as used for reference.
//
// Measured: picture 15 of one real stream is a P slice with frame_num 12 holding
// frames 8, 9, 10 and 11, two active entries, and the instructions below. It asks
// for frame 11 and then for frame 9. Searching the working list refused the second
// with "short-term picture 9 is not held" on a stream ffmpeg decodes without
// complaint.
func TestAPictureDisplacedFromTheListIsStillAReference(t *testing.T) {
	refs := []RefPicture{
		{ID: 8, FrameNum: 8, POC: 16},
		{ID: 9, FrameNum: 9, POC: 18},
		{ID: 10, FrameNum: 10, POC: 20},
		{ID: 11, FrameNum: 11, POC: 22},
	}
	const currPicNum, maxPicNum, active = 12, 16, 2
	l0, _, err := InitialRefLists(
		SliceHeader{Type: SliceP}, refs, 24, currPicNum, maxPicNum)
	if err != nil {
		t.Fatalf("initial list: %v", err)
	}
	// The instructions as the stream states them: subtract 1, then subtract 2.
	ops := []RefListOp{{Kind: 0, Value: 0}, {Kind: 0, Value: 1}}
	got, err := ApplyRefListOps(l0, refs, ops, active, currPicNum, maxPicNum)
	if err != nil {
		t.Fatalf("the stream asks for frames 11 and 9 and the set holds both: %v", err)
	}
	want := []uint32{11, 9}
	if len(got) != len(want) {
		t.Fatalf("list of %d entries, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].FrameNum != w {
			t.Errorf("entry %d is frame %d, want %d (whole list %+v)",
				i, got[i].FrameNum, w, got)
		}
	}
}
