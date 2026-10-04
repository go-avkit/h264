// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"fmt"
	"sort"
)

// RefPicture is one picture a slice may predict from.
//
// The caller holds the samples; this package orders the pictures. ID is the
// caller's own handle, carried through untouched so that an ordered list can be
// turned back into buffers.
type RefPicture struct {
	ID          int
	POC         int32
	FrameNum    uint32
	LongTerm    bool
	LongTermIdx uint32
}

// ErrRefLists means a slice's lists cannot be built as stated.
var ErrRefLists = fmt.Errorf("%w: reference lists", ErrSliceHeader)

// frameNumWrap is a reference's frame number expressed relative to the current
// one, so that a picture from before a wrap sorts before one from after it.
//
// ⛔ Without it, the picture whose frame number wrapped to zero sorts FIRST
// instead of last, and a P slice then predicts from the oldest reference it has
// while believing it took the newest.
func frameNumWrap(ref uint32, current uint32, maxFrameNum uint32) int32 {
	if ref > current {
		return int32(ref) - int32(maxFrameNum)
	}
	return int32(ref)
}

// InitialRefLists builds the reference lists a slice starts from, before any
// instruction it states is applied.
//
// refs are the pictures held as references, in any order. currFrameNum and
// currPOC describe the slice's own picture, and maxFrameNum is 1 << the set's
// log2_max_frame_num.
//
// ⛔ Only frames. A field-coded picture interleaves the two parities and splits
// every reference in two, which is a different derivation altogether -- and of 158
// sequences measured in one library, every one was frame-only. Returning a
// frame-shaped answer for a field would be wrong in a way nothing here could see.
func InitialRefLists(h SliceHeader, refs []RefPicture, currPOC int32,
	currFrameNum uint32, maxFrameNum uint32) (l0, l1 []RefPicture, err error) {
	if h.FieldPic {
		return nil, nil, fmt.Errorf("%w are not derived for a field-coded picture", ErrRefLists)
	}
	switch h.Type {
	case SliceP, SliceSP:
		return pListFor(refs, currFrameNum, maxFrameNum), nil, nil
	case SliceB:
		a, b := bListsFor(refs, currPOC)
		return a, b, nil
	case SliceI, SliceSI:
		// An I slice predicts from nothing, and empty is the answer rather than
		// an error: a caller walking a picture's slices should not have to ask
		// which kind each one is before asking this.
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("%w: slice type %v", ErrRefLists, h.Type)
	}
}

// pListFor orders the references of a P slice: the short-term ones by DESCENDING
// picture number, so the most recent comes first, then the long-term ones by
// ascending index.
func pListFor(refs []RefPicture, currFrameNum, maxFrameNum uint32) []RefPicture {
	short, long := split(refs)
	sort.SliceStable(short, func(i, j int) bool {
		return frameNumWrap(short[i].FrameNum, currFrameNum, maxFrameNum) >
			frameNumWrap(short[j].FrameNum, currFrameNum, maxFrameNum)
	})
	sort.SliceStable(long, func(i, j int) bool { return long[i].LongTermIdx < long[j].LongTermIdx })
	return append(short, long...)
}

// bListsFor orders the references of a B slice.
//
// ⛔ The two lists are not each other's reverse. L0 runs backwards through the
// pictures BEFORE this one and then forwards through those after; L1 runs forwards
// through those after and then backwards through those before. A reader that built
// one and reversed it would be right only when the references are symmetric about
// the current picture, which is the common case and so the one that hides the
// error.
func bListsFor(refs []RefPicture, currPOC int32) (l0, l1 []RefPicture) {
	short, long := split(refs)
	var before, after []RefPicture
	for _, r := range short {
		if r.POC < currPOC {
			before = append(before, r)
		} else {
			after = append(after, r)
		}
	}
	sort.SliceStable(before, func(i, j int) bool { return before[i].POC > before[j].POC })
	sort.SliceStable(after, func(i, j int) bool { return after[i].POC < after[j].POC })
	sort.SliceStable(long, func(i, j int) bool { return long[i].LongTermIdx < long[j].LongTermIdx })

	l0 = append(append(copyOf(before), after...), long...)
	l1 = append(append(copyOf(after), before...), long...)

	// ⛔ When the two lists come out identical and hold more than one picture, the
	// first two entries of L1 are SWAPPED. Without it a bi-predicted block takes
	// the same picture twice and the second prediction adds nothing -- which looks
	// like a plain single prediction and is wrong only by how much it blurs. It is
	// reached whenever every reference sits on one side of the current picture,
	// which is what the pictures at the end of a sequence do.
	if len(l1) > 1 && sameList(l0, l1) {
		l1[0], l1[1] = l1[1], l1[0]
	}
	return l0, l1
}

// split separates the short-term references from the long-term ones, copying so
// that the caller's slice is not reordered under it.
func split(refs []RefPicture) (short, long []RefPicture) {
	for _, r := range refs {
		if r.LongTerm {
			long = append(long, r)
		} else {
			short = append(short, r)
		}
	}
	return short, long
}

func copyOf(in []RefPicture) []RefPicture {
	return append(make([]RefPicture, 0, len(in)), in...)
}

// sameList reports whether two lists hold the same pictures in the same order.
//
// It takes no length check: the two lists are built from the same three groups, so
// they are always the same length, and a branch no input can reach is a branch
// nothing proves.
func sameList(a, b []RefPicture) bool {
	for i := range a {
		if a[i].ID != b[i].ID {
			return false
		}
	}
	return true
}

// ApplyRefListOps rewrites a list the way a slice's instructions ask.
//
// active is num_ref_idx_lX_active: the length the list must have when this
// returns. maxPicNum is 1 << log2_max_frame_num for a frame-coded picture, and
// currPicNum is the slice's own frame number.
//
// ⛔ The list is active+1 long while the instructions are applied, and is cut back
// at the end. The format says so, and it matters: an instruction that moves a
// picture forward needs somewhere to put the one it displaces, and a reader
// working in a fixed-length list drops that picture instead.
func ApplyRefListOps(list []RefPicture, ops []RefListOp, active int,
	currPicNum uint32, maxPicNum uint32) ([]RefPicture, error) {
	if active < 0 {
		return nil, fmt.Errorf("%w: %d active references", ErrRefLists, active)
	}
	if active == 0 {
		return nil, nil
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("%w: no reference is held", ErrRefLists)
	}
	// ⛔ Short of references the initial list REPEATS rather than holding gaps: a
	// slice may name more active references than the stream holds, and an entry
	// that is nothing would be read as a picture of nothing.
	work := make([]RefPicture, active+1)
	for i := range work {
		if i < len(list) {
			work[i] = list[i]
		} else {
			work[i] = list[i%len(list)]
		}
	}

	pred := int32(currPicNum)
	at := 0
	for _, op := range ops {
		if at >= active {
			return nil, fmt.Errorf("%w: an instruction names position %d of %d",
				ErrRefLists, at, active)
		}
		var matches func(RefPicture) bool
		var describe string
		switch op.Kind {
		case 0, 1:
			picNum, next := shortPicNum(op, pred, int32(currPicNum), int32(maxPicNum))
			pred = next
			matches = func(r RefPicture) bool {
				return !r.LongTerm && frameNumWrap(r.FrameNum, currPicNum, maxPicNum) == picNum
			}
			describe = fmt.Sprintf("short-term picture %d", picNum)
		case 2:
			want := op.Value
			matches = func(r RefPicture) bool { return r.LongTerm && r.LongTermIdx == want }
			describe = fmt.Sprintf("long-term picture %d", want)
		default:
			return nil, fmt.Errorf("%w: instruction %d", ErrRefLists, op.Kind)
		}
		if err := place(work, at, matches, describe); err != nil {
			return nil, err
		}
		at++
	}
	return work[:active], nil
}

// shortPicNum walks the prediction the format keeps while reading instructions:
// each one states a DIFFERENCE from the last picture named, not a number.
//
// ⛔ The difference wraps, in both directions, against MaxPicNum. A reader that
// added or subtracted without wrapping names a picture that is not there as soon
// as a list reaches across a frame number wrap.
func shortPicNum(op RefListOp, pred, currPicNum, maxPicNum int32) (picNum, next int32) {
	delta := int32(op.Value) + 1
	var noWrap int32
	if op.Kind == 0 {
		noWrap = pred - delta
		if noWrap < 0 {
			noWrap += maxPicNum
		}
	} else {
		noWrap = pred + delta
		if noWrap >= maxPicNum {
			noWrap -= maxPicNum
		}
	}
	picNum = noWrap
	if noWrap > currPicNum {
		picNum = noWrap - maxPicNum
	}
	return picNum, noWrap
}

// place performs the format's reordering step, in place: shift everything from at
// onwards up by one, put the named picture at at, then close the gap the picture's
// other copy leaves.
//
// It is written as the three loops the format states rather than rebuilt from
// parts, because the thing that is easy to get wrong here is WHICH entries the
// closing pass walks -- it starts after the inserted picture, not at the front.
func place(work []RefPicture, at int, matches func(RefPicture) bool, describe string) error {
	picked, found := RefPicture{}, false
	for _, r := range work {
		if matches(r) {
			picked, found = r, true
			break
		}
	}
	if !found {
		return fmt.Errorf("%w: %s is not held", ErrRefLists, describe)
	}
	for c := len(work) - 1; c > at; c-- {
		work[c] = work[c-1]
	}
	work[at] = picked
	n := at + 1
	for c := at + 1; c < len(work); c++ {
		if !matches(work[c]) {
			work[n] = work[c]
			n++
		}
	}
	return nil
}
