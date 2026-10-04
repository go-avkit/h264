// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

// Prediction is what one coded picture predicts from.
type Prediction struct {
	// POC is the picture's order count.
	POC POC
	// L0 and L1 are the reference lists the slice uses, after any modification
	// it states. An I slice has neither; a P slice has only L0.
	L0, L1 []RefPicture
	// Handle is what the reference set gave this picture, or -1 when the
	// picture was not kept as a reference.
	Handle int
}

// Predictor walks a stream's coded pictures in decode order and answers, for
// each one, what it predicts from.
//
// It exists because the five steps below have to happen in one particular
// order, and nothing in their signatures says so:
//
//  1. the picture order count, which is state carried from the picture before;
//  2. the initial reference lists, FROM THE SET AS IT STANDS;
//  3. the modifications the slice states, searched in that same set;
//  4. the slice's own marking, applied to the set;
//  5. the picture joining the set, if it is a reference.
//
// ⛔ Step 2 before step 5 is the whole point. A picture predicts from the set as
// it was BEFORE the picture joined it -- take the picture in first and it
// predicts from itself, which decodes to a plausible and wrong image rather
// than to an error. Three separate callers in this project's own harnesses had
// to rediscover that ordering from the doc comments, which is the signal that
// the composition belonged here.
//
// It holds no buffers and copies no samples: a Prediction names pictures by the
// handles the set gave them, and a caller keeps its own frame store.
type Predictor struct {
	poc POCCounter
	set RefSet
}

// NewPredictor is a predictor at the start of a stream.
func NewPredictor() *Predictor { return &Predictor{set: *NewRefSet()} }

// Reset forgets everything, which is what a seek is.
func (p *Predictor) Reset() {
	p.poc.Reset()
	p.set.Reset()
}

// Held is the set of pictures currently kept as references, in the order they
// were taken in. It is what the next picture will predict from.
func (p *Predictor) Held() []RefPicture { return p.set.Pictures() }

// Picture answers what this coded picture predicts from, and advances the
// state. Feed it every coded picture of the stream in decode order, after
// reading the header of the slice that begins it.
//
// ⛔ Pass the FIRST slice of each picture, the one with first_mb_in_slice zero
// (see SliceHeader.BeginsPicture). A picture carried by several slices states
// its order count and its marking once, in every slice; feeding each of them
// would count the picture several times -- sliding the window too often and
// applying one marking repeatedly, which for an operation that releases a
// picture means refusing the second time round.
func (p *Predictor) Picture(u Unit, h SliceHeader, ref SliceReferences,
	sps SPS, pps PPS) (Prediction, error) {
	poc, err := p.poc.Next(u, h, ref, sps)
	if err != nil {
		return Prediction{}, err
	}

	// The set as it stands, which is what this picture predicts from.
	refs := p.set.Pictures()
	maxFrameNum := uint32(1) << sps.Log2MaxFrameNum
	l0, l1, err := InitialRefLists(h, refs, poc.Value, h.FrameNum, maxFrameNum)
	if err != nil {
		return Prediction{}, err
	}
	// ⛔ The modifications are searched in the SET and not in the list being
	// rewritten -- clause 8.2.4.3.1 selects from the pictures marked as used for
	// reference. It makes no observable difference HERE, and that was measured
	// rather than assumed: passing the list instead gives byte-identical counts
	// over 5392 pictures, because an initial list holds exactly the set's
	// members and only their order differs. It is written the way the clause
	// says so that it stays right if a list ever stops holding all of them --
	// and because ApplyRefListOps truncates internally, where the difference is
	// real and has its own witness.
	if len(ref.ModifyL0) > 0 {
		l0, err = ApplyRefListOps(l0, refs, ref.ModifyL0,
			int(ref.NumRefIdxL0Active), h.FrameNum, maxFrameNum)
		if err != nil {
			return Prediction{}, err
		}
	}
	if len(ref.ModifyL1) > 0 {
		l1, err = ApplyRefListOps(l1, refs, ref.ModifyL1,
			int(ref.NumRefIdxL1Active), h.FrameNum, maxFrameNum)
		if err != nil {
			return Prediction{}, err
		}
	}

	// ⛔ And only now does the picture itself reach the set.
	handle, err := p.set.AfterPicture(u, h, ref, poc, sps)
	if err != nil {
		return Prediction{}, err
	}
	return Prediction{
		POC:    poc,
		L0:     first(l0, ref.NumRefIdxL0Active),
		L1:     first(l1, ref.NumRefIdxL1Active),
		Handle: handle,
	}, nil
}

// first is the list a slice can actually index: its first active entries.
//
// ⛔ The initial lists are built from the WHOLE set -- 8.2.4.2 orders every
// reference picture held, and 8.2.4.2.1 then discards "the extra entries beyond
// position num_ref_idx_lX_active_minus1". Without this, Prediction carried the
// ordering rather than the list: measured on a 14-picture stream whose every
// slice declares one active reference, L0 and L1 each held THREE entries. The
// first was right, so a caller reading L0[ref_idx] saw nothing wrong, while one
// asking len(L0) was told the slice had three references to choose from when
// the bitstream gives ref_idx one value.
//
// It was also INCONSISTENT, which is worse than either answer: ApplyRefListOps
// truncates, so the length depended on whether the slice happened to state a
// modification.
//
// A list shorter than active is left as it is. 8.2.4.2.1 leaves those entries
// unspecified -- there is no picture to put there -- and padding would invent a
// reference.
func first(list []RefPicture, active uint32) []RefPicture {
	if uint32(len(list)) <= active {
		return list
	}
	return list[:active]
}
