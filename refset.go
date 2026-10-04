// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "fmt"

// ErrRefSet means a picture's marking cannot be applied as stated.
var ErrRefSet = fmt.Errorf("%w: reference set", ErrSliceHeader)

// RefSet is the set of pictures a stream currently holds as references.
//
// Feed it every coded picture in decode order, after reading its slice header: it
// applies what that picture says about the pictures before it, and then takes the
// picture itself in if it is a reference. Pictures reports what a slice may
// predict from, which is what InitialRefLists wants.
//
// ⛔ The set is state, like the picture order counts, and for the same reason: a
// picture's marking speaks about what came before it. A caller asking which
// pictures a slice may use, without having walked the ones before it, is asking a
// question that has no answer.
type RefSet struct {
	held []RefPicture
	// next is the handle given to the next picture taken in. The caller gets it
	// back on every list, so it can find its own buffer again.
	next int
	// maxLongTermIdx is max_long_term_frame_idx, as operation four sets it. It
	// starts at -1: no long-term picture is allowed until something says so.
	maxLongTermIdx int32
}

// NewRefSet is an empty set, as a stream begins.
func NewRefSet() *RefSet { return &RefSet{maxLongTermIdx: -1} }

// Pictures is what a slice may predict from, in the order they were taken in. The
// ordering a slice actually uses is InitialRefLists'.
func (s *RefSet) Pictures() []RefPicture {
	return append(make([]RefPicture, 0, len(s.held)), s.held...)
}

// Reset empties the set, which is what a seek is.
func (s *RefSet) Reset() { *s = RefSet{maxLongTermIdx: -1} }

// AfterPicture applies one picture's marking and takes the picture in if it is a
// reference. It returns the handle the picture was given, or -1 when the picture
// was not kept.
//
// ⛔ The order is: apply what this picture SAYS about the others, then take this
// picture in. Doing it the other way round lets the sliding window throw out the
// picture being added, and lets operation one address the new picture by a number
// that was meant for an older one.
func (s *RefSet) AfterPicture(u Unit, h SliceHeader, ref SliceReferences, p POC, sps SPS) (int, error) {
	if h.FieldPic {
		return -1, fmt.Errorf("%w is not kept for a field-coded picture", ErrRefSet)
	}
	// ⛔ A picture nothing may refer to is not taken in at all. Keeping it would
	// push a real reference out of the set on the very next sliding window.
	if u.RefIDC == 0 {
		return -1, nil
	}
	maxFrameNum := uint32(1) << sps.Log2MaxFrameNum
	capacity := int(sps.MaxNumRefFrames)
	if capacity < 1 {
		// ⛔ Max(max_num_ref_frames, 1): a set stated as holding nothing still
		// holds the one picture a P slice needs, and a capacity of zero would
		// throw every picture out as it arrived.
		capacity = 1
	}

	if h.IDR {
		// An IDR begins a sequence: nothing before it is a reference any more.
		s.held = s.held[:0]
		s.maxLongTermIdx = -1
		taken := RefPicture{ID: s.next, POC: p.Value, FrameNum: h.FrameNum}
		if ref.LongTermReference {
			taken.LongTerm, taken.LongTermIdx = true, 0
			s.maxLongTermIdx = 0
		}
		s.held = append(s.held, taken)
		s.next++
		return taken.ID, nil
	}

	frameNum := h.FrameNum
	if ref.AdaptiveMarking {
		restarted, err := s.applyMarking(ref.Marking, h.FrameNum, maxFrameNum, p)
		if err != nil {
			return -1, err
		}
		if restarted {
			// ⛔ 7.4.3: a picture stating operation five "shall be inferred to
			// have had frame_num equal to 0 for all subsequent use". Keeping the
			// number it was coded with makes it look far older than every
			// picture that follows -- the counting restarts at 0 after it -- so
			// the sliding window throws it out first and a list modification
			// naming it computes a picture number for somebody else.
			frameNum = 0
		}
		// ⛔ Operation six marks THIS picture, so it may already be in the set.
		if id, taken := s.tookCurrent(); taken {
			return id, nil
		}
	} else if len(s.held) >= capacity {
		if !s.slideWindow(h.FrameNum, maxFrameNum) {
			// ⛔ Nothing could be let go: every picture held is long-term, and a
			// long-term picture is released only by an explicit operation. Going
			// on would hold MORE pictures than max_num_ref_frames says -- and a
			// decoder sized its frame store from that number.
			return -1, fmt.Errorf(
				"%w: %d pictures are held, all long-term, and max_num_ref_frames is %d",
				ErrRefSet, len(s.held), capacity)
		}
	}

	taken := RefPicture{ID: s.next, POC: p.Value, FrameNum: frameNum}
	s.held = append(s.held, taken)
	s.next++
	return taken.ID, nil
}

// tookCurrent reports whether the marking already took the current picture in,
// which operation six does, and consumes its handle.
func (s *RefSet) tookCurrent() (int, bool) {
	if at := s.currentAt(); at >= 0 {
		id := s.held[at].ID
		s.next++
		return id, true
	}
	return 0, false
}

// currentAt is where the current picture sits, if the marking has already taken
// it in, or -1. It looks without consuming the handle, which is what lets
// operation six be refused a second time.
func (s *RefSet) currentAt() int {
	for i, r := range s.held {
		if r.ID == s.next {
			return i
		}
	}
	return -1
}

// slideWindow throws out one picture to make room.
//
// ⛔ It removes the SHORT-TERM picture with the smallest frame number expressed
// relative to the current one -- not the smallest count, and not the one taken in
// first. Those three agree until a frame number wraps or a stream reorders, and
// then they do not.
//
// ⛔ A long-term picture is never thrown out this way. Only an explicit operation
// releases one, which is the whole point of its being long-term. It therefore
// reports whether it found anything at all: a set made entirely of long-term
// pictures has no room to give, and its caller must refuse rather than grow.
func (s *RefSet) slideWindow(currFrameNum, maxFrameNum uint32) bool {
	oldest, at := int32(0), -1
	for i, r := range s.held {
		if r.LongTerm {
			continue
		}
		w := frameNumWrap(r.FrameNum, currFrameNum, maxFrameNum)
		if at < 0 || w < oldest {
			oldest, at = w, i
		}
	}
	if at < 0 {
		return false
	}
	s.held = append(s.held[:at], s.held[at+1:]...)
	return true
}

// applyMarking carries out what a picture states about the reference set. It
// reports whether the set was restarted, which operation five does.
func (s *RefSet) applyMarking(ops []MarkOp, currFrameNum, maxFrameNum uint32,
	p POC) (restarted bool, err error) {
	currPicNum := int32(currFrameNum)
	for _, op := range ops {
		switch op.Operation {
		case 1:
			// A short-term picture, named by how far below the current one it is.
			want := currPicNum - int32(op.Value) - 1
			if !s.release(func(r RefPicture) bool {
				return !r.LongTerm && frameNumWrap(r.FrameNum, currFrameNum, maxFrameNum) == want
			}) {
				return restarted, fmt.Errorf(
					"%w: short-term picture %d is not held", ErrRefSet, want)
			}
		case 2:
			want := op.Value
			if !s.release(func(r RefPicture) bool { return r.LongTerm && r.LongTermIdx == want }) {
				return restarted, fmt.Errorf(
					"%w: long-term picture %d is not held", ErrRefSet, want)
			}
		case 3:
			// A short-term picture becomes long-term, under the index given.
			want := currPicNum - int32(op.Value) - 1
			idx := op.Extra
			// ⛔ No `maxLongTermIdx >= 0` precondition. It starts at -1 because
			// max_long_term_frame_idx_plus1 starts at ZERO: until an operation
			// four says otherwise, NO long-term index is allowed. Guarding only
			// when an allowance HAD been stated let every index through before
			// the first operation four -- so a stream could hold a long-term
			// picture under a name no list is permitted to address.
			if int32(idx) > s.maxLongTermIdx {
				return restarted, fmt.Errorf("%w: long-term index %d is above the %d allowed",
					ErrRefSet, idx, s.maxLongTermIdx)
			}
			// ⛔ The index is exclusive: whatever held it is released first, or the
			// set would hold two pictures answering to one name and a list would
			// take whichever it met first.
			s.release(func(r RefPicture) bool { return r.LongTerm && r.LongTermIdx == idx })
			promoted := false
			for i := range s.held {
				r := s.held[i]
				if !r.LongTerm && frameNumWrap(r.FrameNum, currFrameNum, maxFrameNum) == want {
					s.held[i].LongTerm, s.held[i].LongTermIdx = true, idx
					promoted = true
					break
				}
			}
			if !promoted {
				return restarted, fmt.Errorf(
					"%w: short-term picture %d is not held", ErrRefSet, want)
			}
		case 4:
			// ⛔ The value is a PLUS ONE: zero means no long-term picture is
			// allowed at all, and every one held is released.
			s.maxLongTermIdx = int32(op.Value) - 1
			s.releaseAll(func(r RefPicture) bool {
				return r.LongTerm && int32(r.LongTermIdx) > s.maxLongTermIdx
			})
		case 5:
			// Everything goes, and the counts restart -- which POCCounter was told
			// about separately, through the same flag.
			s.held = s.held[:0]
			s.maxLongTermIdx = -1
			restarted = true
			// ⛔ The counting restarts HERE, within this picture's own marking:
			// 7.4.3 infers frame_num 0 for the picture stating the operation, so
			// an operation six that follows in the same marking must take the
			// picture in under 0 and not under the number it was coded with.
			currFrameNum, currPicNum = 0, 0
		case 6:
			// ⛔ THIS picture becomes long-term, so it joins the set here rather
			// than after the marking like every other reference picture does.
			idx := op.Value
			// ⛔ See operation three: -1 means nothing is allowed yet, not
			// "no limit".
			if int32(idx) > s.maxLongTermIdx {
				return restarted, fmt.Errorf("%w: long-term index %d is above the %d allowed",
					ErrRefSet, idx, s.maxLongTermIdx)
			}
			// ⛔ Once only. A second operation six would append a SECOND picture
			// carrying the same handle, and the handle is how a caller finds its
			// own buffer again -- it would decode one picture into another's.
			if s.currentAt() >= 0 {
				return restarted, fmt.Errorf(
					"%w: operation six marks this picture twice", ErrRefSet)
			}
			s.release(func(r RefPicture) bool { return r.LongTerm && r.LongTermIdx == idx })
			s.held = append(s.held, RefPicture{
				ID: s.next, POC: p.Value, FrameNum: currFrameNum,
				LongTerm: true, LongTermIdx: idx,
			})
		default:
			return restarted, fmt.Errorf("%w: operation %d", ErrRefSet, op.Operation)
		}
	}
	return restarted, nil
}

// release lets one picture go, and says whether it found one.
func (s *RefSet) release(wanted func(RefPicture) bool) bool {
	for i, r := range s.held {
		if wanted(r) {
			s.held = append(s.held[:i], s.held[i+1:]...)
			return true
		}
	}
	return false
}

// releaseAll lets every matching picture go.
func (s *RefSet) releaseAll(wanted func(RefPicture) bool) {
	kept := s.held[:0]
	for _, r := range s.held {
		if !wanted(r) {
			kept = append(kept, r)
		}
	}
	s.held = kept
}
