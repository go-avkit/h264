package decoder

import (
	"testing"
	"time"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/syntax"
)

func newRef(frameNum uint32, longTerm bool, idx int) *refFrame {
	return &refFrame{pic: frame.NewPicture(1, 1), frameNum: frameNum, longTerm: longTerm, longTermIdx: idx}
}

func newCurrent() *refFrame {
	return &refFrame{pic: frame.NewPicture(1, 1)}
}

func newTestHeader(frameNum uint32, idr bool, nalRefIDC uint8) *syntax.SliceHeader {
	return &syntax.SliceHeader{FrameNum: frameNum, IDR: idr, NalRefIDC: nalRefIDC}
}

func assertRefOrder(t *testing.T, got, want []*refFrame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: got=%v want=%v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %p want %p", i, got[i], want[i])
		}
	}
}

func TestComputePOCType0(t *testing.T) {
	sps := &syntax.SPS{PicOrderCntType: 0, Log2MaxPicOrderCntLsbMinus4: 0}

	cases := []struct {
		name        string
		initMsb     int
		initLsb     int
		hdr         *syntax.SliceHeader
		wantPOC     int
		wantPrevMsb int
		wantPrevLsb int
	}{
		{
			name:        "idr resets previous msb",
			initMsb:     5,
			initLsb:     10,
			hdr:         &syntax.SliceHeader{IDR: true, PicOrderCntLsb: 3, NalRefIDC: 1},
			wantPOC:     3,
			wantPrevMsb: 0,
			wantPrevLsb: 3,
		},
		{
			name:        "normal increasing sequence",
			initMsb:     0,
			initLsb:     3,
			hdr:         &syntax.SliceHeader{PicOrderCntLsb: 5, NalRefIDC: 1},
			wantPOC:     5,
			wantPrevMsb: 0,
			wantPrevLsb: 5,
		},
		{
			name:        "wrap up: lsb drops by more than half range, msb increases",
			initMsb:     0,
			initLsb:     14,
			hdr:         &syntax.SliceHeader{PicOrderCntLsb: 2, NalRefIDC: 1},
			wantPOC:     18,
			wantPrevMsb: 16,
			wantPrevLsb: 2,
		},
		{
			name:        "wrap down: lsb rises by more than half range, msb decreases",
			initMsb:     16,
			initLsb:     2,
			hdr:         &syntax.SliceHeader{PicOrderCntLsb: 15, NalRefIDC: 1},
			wantPOC:     15,
			wantPrevMsb: 0,
			wantPrevLsb: 15,
		},
		{
			name:        "non-reference picture does not update stored state",
			initMsb:     0,
			initLsb:     5,
			hdr:         &syntax.SliceHeader{PicOrderCntLsb: 6, NalRefIDC: 0},
			wantPOC:     6,
			wantPrevMsb: 0,
			wantPrevLsb: 5,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := &dpb{prevPicOrderCntMsb: c.initMsb, prevPicOrderCntLsb: c.initLsb}
			got := b.computePOC(sps, c.hdr)
			if got != c.wantPOC {
				t.Fatalf("POC = %d, want %d", got, c.wantPOC)
			}
			if b.prevPicOrderCntMsb != c.wantPrevMsb || b.prevPicOrderCntLsb != c.wantPrevLsb {
				t.Fatalf("prev state = (%d,%d), want (%d,%d)", b.prevPicOrderCntMsb, b.prevPicOrderCntLsb, c.wantPrevMsb, c.wantPrevLsb)
			}
		})
	}
}

func TestComputePOCType2(t *testing.T) {
	sps := &syntax.SPS{PicOrderCntType: 2}

	t.Run("reference and non-reference formula", func(t *testing.T) {
		b := &dpb{maxFrameNum: 16}

		if got := b.computePOC(sps, newTestHeader(0, true, 1)); got != 0 {
			t.Fatalf("frame 0 (IDR, ref): POC = %d, want 0", got)
		}
		if got := b.computePOC(sps, newTestHeader(1, false, 1)); got != 2 {
			t.Fatalf("frame 1 (ref): POC = %d, want 2", got)
		}
		if got := b.computePOC(sps, newTestHeader(2, false, 0)); got != 3 {
			t.Fatalf("frame 2 (non-ref): POC = %d, want 3 (2*frameNum-1)", got)
		}
		if got := b.computePOC(sps, newTestHeader(3, false, 1)); got != 6 {
			t.Fatalf("frame 3 (ref): POC = %d, want 6", got)
		}
	})

	t.Run("frame number offset accumulates across a wrap", func(t *testing.T) {
		b := &dpb{maxFrameNum: 16, prevFrameNumOffset: 0, prevFrameNum: 15}

		got := b.computePOC(sps, newTestHeader(0, false, 1))
		if got != 32 {
			t.Fatalf("POC = %d, want 32 (offset wraps to 16)", got)
		}
		if b.prevFrameNumOffset != 16 {
			t.Fatalf("prevFrameNumOffset = %d, want 16", b.prevFrameNumOffset)
		}
		got = b.computePOC(sps, newTestHeader(1, false, 1))
		if got != 34 {
			t.Fatalf("POC = %d, want 34", got)
		}
	})
}

func TestPocType1AcrossMultipleCycles(t *testing.T) {
	sps := &syntax.SPS{
		PicOrderCntType:    1,
		OffsetForRefFrame:  []int32{4, 2, 1},
		OffsetForNonRefPic: -1,
	}
	b := &dpb{maxFrameNum: 16}

	cases := []struct {
		frameNum  uint32
		idr       bool
		nalRefIDC uint8
		delta0    int32
		want      int
	}{
		{frameNum: 0, idr: true, nalRefIDC: 1, want: 0},
		{frameNum: 1, nalRefIDC: 1, want: 4},
		{frameNum: 2, nalRefIDC: 1, want: 6},
		{frameNum: 3, nalRefIDC: 1, want: 7},
		{frameNum: 4, nalRefIDC: 1, want: 11},
		{frameNum: 5, nalRefIDC: 0, want: 10},
		{frameNum: 6, nalRefIDC: 1, delta0: 5, want: 19},
	}
	for _, c := range cases {
		hdr := &syntax.SliceHeader{FrameNum: c.frameNum, IDR: c.idr, NalRefIDC: c.nalRefIDC}
		hdr.DeltaPicOrderCnt[0] = c.delta0
		got := b.computePOC(sps, hdr)
		if got != c.want {
			t.Fatalf("frameNum %d: POC = %d, want %d", c.frameNum, got, c.want)
		}
	}
}

func TestPocType1FrameNumWrap(t *testing.T) {
	sps := &syntax.SPS{
		PicOrderCntType:    1,
		OffsetForRefFrame:  []int32{4, 2, 1},
		OffsetForNonRefPic: -1,
	}
	b := &dpb{maxFrameNum: 16}

	got := b.computePOC(sps, newTestHeader(0, true, 1))
	if got != 0 {
		t.Fatalf("frame 0: POC = %d, want 0", got)
	}
	got = b.computePOC(sps, newTestHeader(15, false, 1))
	if got != 35 {
		t.Fatalf("frame 15: POC = %d, want 35", got)
	}
	got = b.computePOC(sps, newTestHeader(0, false, 1))
	if got != 39 {
		t.Fatalf("frame 0 (wrapped): POC = %d, want 39", got)
	}
}

func TestUpdatePicNumsFrameNumWrap(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	rLow := newRef(2, false, 0)
	rHigh := newRef(14, false, 0)
	rLongTerm := newRef(10, true, 0)
	b.refs = []*refFrame{rLow, rHigh, rLongTerm}

	b.updatePicNums(5)

	if rLow.frameNumWrap != 2 || rLow.picNum != 2 {
		t.Fatalf("rLow: frameNumWrap=%d picNum=%d, want 2,2", rLow.frameNumWrap, rLow.picNum)
	}
	if rHigh.frameNumWrap != -2 || rHigh.picNum != -2 {
		t.Fatalf("rHigh: frameNumWrap=%d picNum=%d, want -2,-2", rHigh.frameNumWrap, rHigh.picNum)
	}
	if rLongTerm.frameNumWrap != 0 {
		t.Fatalf("long-term ref frameNumWrap changed to %d, want unchanged (0)", rLongTerm.frameNumWrap)
	}
}

func TestShortTermByPicNum(t *testing.T) {
	r1 := newRef(1, false, 0)
	r1.picNum = 5
	r2 := newRef(2, false, 0)
	r2.picNum = -3
	rLT := newRef(3, true, 7)
	b := &dpb{refs: []*refFrame{r1, r2, rLT}}

	if got := b.shortTermByPicNum(5); got != r1 {
		t.Fatalf("picNum 5: got %v want r1", got)
	}
	if got := b.shortTermByPicNum(-3); got != r2 {
		t.Fatalf("picNum -3: got %v want r2", got)
	}
	if got := b.shortTermByPicNum(7); got != nil {
		t.Fatalf("a long-term entry must not satisfy a short-term lookup, got %v", got)
	}
	if got := b.shortTermByPicNum(99); got != nil {
		t.Fatalf("not found: got %v want nil", got)
	}
}

func TestLongTermByPicNum(t *testing.T) {
	r1 := newRef(1, false, 0)
	rLT1 := newRef(2, true, 4)
	rLT2 := newRef(3, true, 9)
	b := &dpb{refs: []*refFrame{r1, rLT1, rLT2}}

	if got := b.longTermByPicNum(4); got != rLT1 {
		t.Fatalf("idx 4: got %v want rLT1", got)
	}
	if got := b.longTermByPicNum(9); got != rLT2 {
		t.Fatalf("idx 9: got %v want rLT2", got)
	}
	if got := b.longTermByPicNum(0); got != nil {
		t.Fatalf("not found: got %v want nil", got)
	}
}

func TestRemove(t *testing.T) {
	r1 := newRef(1, false, 0)
	r2 := newRef(2, false, 0)
	r3 := newRef(3, false, 0)
	b := &dpb{refs: []*refFrame{r1, r2, r3}}

	b.remove(r2)
	assertRefOrder(t, b.refs, []*refFrame{r1, r3})

	notPresent := newRef(4, false, 0)
	b.remove(notPresent)
	assertRefOrder(t, b.refs, []*refFrame{r1, r3})
}

func TestBuildListPOrderingAndTruncation(t *testing.T) {
	mk := func(picNum int) *refFrame {
		r := newRef(0, false, 0)
		r.picNum = picNum
		return r
	}
	mkLT := func(idx int) *refFrame {
		return newRef(0, true, idx)
	}

	s1, s2, s3, s4, s5 := mk(5), mk(3), mk(8), mk(1), mk(9)
	l1, l2 := mkLT(5), mkLT(2)

	b := &dpb{refs: []*refFrame{s1, s2, s3, s4, s5, l1, l2}}
	hdr := &syntax.SliceHeader{}

	t.Run("truncated to the active count", func(t *testing.T) {
		out := b.buildListP(hdr, 3)
		want := []*frame.Picture{s5.pic, s3.pic, s1.pic}
		if len(out) != len(want) {
			t.Fatalf("len = %d, want %d", len(out), len(want))
		}
		for i := range want {
			if out[i] != want[i] {
				t.Fatalf("index %d: got %p want %p", i, out[i], want[i])
			}
		}
	})

	t.Run("fewer refs than active: short desc then long asc, not padded", func(t *testing.T) {
		out := b.buildListP(hdr, 100)
		want := []*frame.Picture{s5.pic, s3.pic, s1.pic, s2.pic, s4.pic, l2.pic, l1.pic}
		if len(out) != len(want) {
			t.Fatalf("len = %d, want %d", len(out), len(want))
		}
		for i := range want {
			if out[i] != want[i] {
				t.Fatalf("index %d: got %p want %p", i, out[i], want[i])
			}
		}
	})
}

func TestBuildListPAppliesModifications(t *testing.T) {
	rA := newRef(0, false, 0)
	rA.picNum = 1
	rB := newRef(0, false, 0)
	rB.picNum = 2
	b := &dpb{maxFrameNum: 16, refs: []*refFrame{rA, rB}}

	hdr := &syntax.SliceHeader{
		FrameNum:              5,
		ModificationL0Present: true,
		RefPicListModificationL0: []syntax.RefPicListModification{
			{IDC: 0, Value: 3}, // diff=4, predPicNum=5-4=1 -> targets rA
		},
	}

	out := b.buildListP(hdr, 2)
	if len(out) != 2 || out[0] != rA.pic || out[1] != rB.pic {
		t.Fatalf("out = %v, want [rA, rB] (modification moves rA to front)", out)
	}
}

func TestApplyModificationsStopsAtActiveCount(t *testing.T) {
	rA := newRef(0, false, 0)
	rA.picNum = 4
	rB := newRef(0, false, 0)
	rB.picNum = 1
	rOther := newRef(0, false, 0)
	rOther.picNum = 0
	b := &dpb{maxFrameNum: 16, refs: []*refFrame{rOther, rA, rB}}
	list := []*refFrame{rOther, rA, rB}

	mods := []syntax.RefPicListModification{
		{IDC: 0, Value: 0}, // predPicNum=5-1=4 -> rA, refIdx becomes 1 == active: loop must break here
		{IDC: 0, Value: 5}, // would target rB if it were processed
	}

	out := b.applyModifications(list, mods, 5, 1)
	assertRefOrder(t, out, []*refFrame{rA, rOther, rB})
}

func TestStoreAppliesMMCOBeforeAppending(t *testing.T) {
	toRemove := newRef(0, false, 0)
	toRemove.picNum = 7
	b := &dpb{maxNumRefs: 4, maxFrameNum: 16, refs: []*refFrame{toRemove}}
	pic := frame.NewPicture(1, 1)
	hdr := &syntax.SliceHeader{
		NalRefIDC:             1,
		FrameNum:              10,
		AdaptiveRefPicMarking: true,
		MMCOs:                 []syntax.MMCO{{Op: 1, DifferenceOfPicNumsMinus1: 2}}, // pn = 10-2-1 = 7
	}

	b.store(pic, hdr)

	if len(b.refs) != 1 || b.refs[0].pic != pic {
		t.Fatalf("refs = %v, want only the newly stored picture (MMCO op 1 must remove picNum 7 first)", b.refs)
	}
}

func TestMoveToIndex(t *testing.T) {
	r1, r2, r3 := newRef(1, false, 0), newRef(2, false, 0), newRef(3, false, 0)
	rOutside := newRef(4, false, 0)

	t.Run("move an existing element to the front", func(t *testing.T) {
		list := []*refFrame{r1, r2, r3}
		out := moveToIndex(list, r2, 0)
		assertRefOrder(t, out, []*refFrame{r2, r1, r3})
	})

	t.Run("append when idx equals length", func(t *testing.T) {
		list := []*refFrame{r1, r2}
		out := moveToIndex(list, rOutside, 2)
		assertRefOrder(t, out, []*refFrame{r1, r2, rOutside})
	})

	t.Run("idx beyond length clamps to append", func(t *testing.T) {
		list := []*refFrame{r1, r2}
		out := moveToIndex(list, rOutside, 50)
		assertRefOrder(t, out, []*refFrame{r1, r2, rOutside})
	})
}

func TestApplyModifications(t *testing.T) {
	rOther := newRef(0, false, 0)
	rOther.picNum = 0
	rA := newRef(0, false, 0)
	rA.picNum = 4
	rB := newRef(0, false, 0)
	rB.picNum = -2
	rC := newRef(0, false, 0)
	rC.picNum = 1
	rLT := newRef(0, true, 3)

	b := &dpb{maxFrameNum: 16, refs: []*refFrame{rOther, rA, rB, rC, rLT}}
	list := []*refFrame{rOther, rA, rB, rC, rLT}

	mods := []syntax.RefPicListModification{
		{IDC: 0, Value: 0},
		{IDC: 0, Value: 5},
		{IDC: 1, Value: 2},
		{IDC: 1, Value: 100},
		{IDC: 2, Value: 3},
		{IDC: 2, Value: 999},
	}

	out := b.applyModifications(list, mods, 5, 10)
	assertRefOrder(t, out, []*refFrame{rA, rB, rC, rLT, rOther})
}

func TestApplyMMCOOp1RemovesShortTerm(t *testing.T) {
	target := newRef(0, false, 0)
	target.picNum = 7
	keep := newRef(0, false, 0)
	keep.picNum = 99
	b := &dpb{refs: []*refFrame{target, keep}}

	hdr := &syntax.SliceHeader{FrameNum: 10, MMCOs: []syntax.MMCO{
		{Op: 1, DifferenceOfPicNumsMinus1: 2},  // pn = 10-2-1 = 7
		{Op: 1, DifferenceOfPicNumsMinus1: 50}, // pn = 10-50-1 = -41: absent, no-op
	}}
	b.applyMMCO(hdr, newCurrent())

	assertRefOrder(t, b.refs, []*refFrame{keep})
}

func TestApplyMMCOOp2RemovesLongTerm(t *testing.T) {
	target := newRef(0, true, 5)
	keep := newRef(0, true, 6)
	b := &dpb{refs: []*refFrame{target, keep}}

	hdr := &syntax.SliceHeader{MMCOs: []syntax.MMCO{
		{Op: 2, LongTermPicNum: 5},
		{Op: 2, LongTermPicNum: 42}, // absent, no-op
	}}
	b.applyMMCO(hdr, newCurrent())

	assertRefOrder(t, b.refs, []*refFrame{keep})
}

func TestApplyMMCOOp3ConvertsToLongTerm(t *testing.T) {
	r := newRef(0, false, 0)
	r.picNum = 9
	b := &dpb{refs: []*refFrame{r}}

	hdr := &syntax.SliceHeader{FrameNum: 10, MMCOs: []syntax.MMCO{
		{Op: 3, DifferenceOfPicNumsMinus1: 0, LongTermFrameIdx: 8}, // pn = 10-0-1 = 9
	}}
	b.applyMMCO(hdr, newCurrent())

	if !r.longTerm || r.longTermIdx != 8 {
		t.Fatalf("r = %+v, want longTerm=true longTermIdx=8", r)
	}
}

func TestApplyMMCOOp3AbsentPictureIsNoop(t *testing.T) {
	r := newRef(0, false, 0)
	r.picNum = 9
	b := &dpb{refs: []*refFrame{r}}

	hdr := &syntax.SliceHeader{FrameNum: 10, MMCOs: []syntax.MMCO{
		{Op: 3, DifferenceOfPicNumsMinus1: 99, LongTermFrameIdx: 8},
	}}
	b.applyMMCO(hdr, newCurrent())

	if r.longTerm {
		t.Fatalf("r should be unchanged for an absent target, got longTerm=true")
	}
}

func TestApplyMMCOOp4DropsAboveMaxLongTermIdx(t *testing.T) {
	keep1 := newRef(0, true, 0)
	keep2 := newRef(0, true, 2)
	drop1 := newRef(0, true, 3)
	drop2 := newRef(0, true, 9)
	short := newRef(0, false, 0)
	b := &dpb{refs: []*refFrame{keep1, keep2, drop1, drop2, short}}

	hdr := &syntax.SliceHeader{MMCOs: []syntax.MMCO{
		{Op: 4, MaxLongTermFrameIdxPlus1: 3}, // max = 2
	}}
	b.applyMMCO(hdr, newCurrent())

	assertRefOrder(t, b.refs, []*refFrame{keep1, keep2, short})
}

func TestApplyMMCOOp5ClearsBuffer(t *testing.T) {
	b := &dpb{refs: []*refFrame{newRef(0, false, 0), newRef(0, true, 1)}}
	hdr := &syntax.SliceHeader{MMCOs: []syntax.MMCO{{Op: 5}}}
	b.applyMMCO(hdr, newCurrent())

	if len(b.refs) != 0 {
		t.Fatalf("refs = %v, want empty", b.refs)
	}
}

func TestApplyMMCOOp6MarksCurrentPictureLongTerm(t *testing.T) {
	keep := newRef(0, false, 0)
	b := &dpb{refs: []*refFrame{keep}}
	current := newCurrent()

	hdr := &syntax.SliceHeader{MMCOs: []syntax.MMCO{{Op: 6, LongTermFrameIdx: 4}}}
	b.applyMMCO(hdr, current)

	if !current.longTerm || current.longTermIdx != 4 {
		t.Fatalf("current = %+v, want longTerm=true longTermIdx=4", current)
	}
	if !current.pic.LongTerm {
		t.Fatal("current.pic.LongTerm was not set")
	}
	assertRefOrder(t, b.refs, []*refFrame{keep})
}

func TestApplyMMCOOp6EvictsThePictureAlreadyHoldingTheIndex(t *testing.T) {
	holder := newRef(0, true, 4)
	other := newRef(0, true, 9)
	b := &dpb{refs: []*refFrame{holder, other}}
	current := newCurrent()

	hdr := &syntax.SliceHeader{MMCOs: []syntax.MMCO{{Op: 6, LongTermFrameIdx: 4}}}
	b.applyMMCO(hdr, current)

	assertRefOrder(t, b.refs, []*refFrame{other})
	if !current.longTerm || current.longTermIdx != 4 {
		t.Fatalf("current = %+v, want longTerm=true longTermIdx=4", current)
	}
}

func TestStoreOp6MarksTheCurrentPictureLongTermInsteadOfShortTerm(t *testing.T) {
	b := &dpb{maxNumRefs: 4, maxFrameNum: 16}
	pic := frame.NewPicture(1, 1)
	hdr := &syntax.SliceHeader{
		NalRefIDC:             1,
		FrameNum:              7,
		AdaptiveRefPicMarking: true,
		MMCOs:                 []syntax.MMCO{{Op: 6, LongTermFrameIdx: 2}},
	}

	b.store(pic, hdr)

	if len(b.refs) != 1 {
		t.Fatalf("refs = %v, want exactly one entry (the current picture must not also be appended short term)", b.refs)
	}
	if !b.refs[0].longTerm || b.refs[0].longTermIdx != 2 || b.refs[0].pic != pic {
		t.Fatalf("refs[0] = %+v, want the current picture marked longTerm=true longTermIdx=2", b.refs[0])
	}
	if !pic.LongTerm {
		t.Fatal("pic.LongTerm was not set")
	}
}

func TestStoreOp6SurvivesTheSlidingWindowThatWouldHaveDroppedAShortTermPicture(t *testing.T) {
	b := &dpb{maxNumRefs: 2, maxFrameNum: 16}

	longTermPic := frame.NewPicture(1, 1)
	hdr := &syntax.SliceHeader{
		NalRefIDC: 1, FrameNum: 0, AdaptiveRefPicMarking: true,
		MMCOs: []syntax.MMCO{{Op: 6, LongTermFrameIdx: 0}},
	}
	b.store(longTermPic, hdr)

	for _, fn := range []uint32{1, 2, 3} {
		b.store(frame.NewPicture(1, 1), &syntax.SliceHeader{NalRefIDC: 1, FrameNum: fn})
	}

	if len(b.refs) != 2 {
		t.Fatalf("refs = %v, want 2 (the long-term picture plus the single sliding-window slot)", b.refs)
	}
	found := false
	for _, r := range b.refs {
		if r.pic == longTermPic {
			found = true
			if !r.longTerm || r.longTermIdx != 0 {
				t.Fatalf("long-term entry = %+v, want longTerm=true longTermIdx=0", r)
			}
		}
	}
	if !found {
		t.Fatal("the picture marked long term by op 6 was evicted by the sliding window")
	}
}

func TestSlidingWindowEvictsSmallestFrameNumWrap(t *testing.T) {
	r1 := newRef(0, false, 0)
	r1.frameNumWrap = 5
	r2 := newRef(0, false, 0)
	r2.frameNumWrap = 1
	r3 := newRef(0, false, 0)
	r3.frameNumWrap = 3
	r4 := newRef(0, false, 0)
	r4.frameNumWrap = 7
	b := &dpb{maxNumRefs: 2, refs: []*refFrame{r1, r2, r3, r4}}

	b.slidingWindow()

	if len(b.refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(b.refs))
	}
	for _, r := range b.refs {
		if r == r2 || r == r3 {
			t.Fatalf("ref with smaller frameNumWrap survived eviction: %+v", r)
		}
	}
}

func TestSlidingWindowNeverEvictsLongTerm(t *testing.T) {
	shortA := newRef(0, false, 0)
	shortA.frameNumWrap = 2
	shortB := newRef(0, false, 0)
	shortB.frameNumWrap = 9
	long := newRef(0, true, 0)
	b := &dpb{maxNumRefs: 1, refs: []*refFrame{shortA, shortB, long}}

	b.slidingWindow()

	assertRefOrder(t, b.refs, []*refFrame{long})
}

func TestSlidingWindowTerminatesWithOnlyLongTermLeft(t *testing.T) {
	l1 := newRef(0, true, 0)
	l2 := newRef(0, true, 1)
	l3 := newRef(0, true, 2)
	b := &dpb{maxNumRefs: 1, refs: []*refFrame{l1, l2, l3}}

	done := make(chan struct{})
	go func() {
		b.slidingWindow()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("slidingWindow did not terminate when only long-term pictures remain")
	}

	assertRefOrder(t, b.refs, []*refFrame{l1, l2, l3})
}

func TestStoreNonReferenceIsNotStored(t *testing.T) {
	b := &dpb{maxNumRefs: 4, maxFrameNum: 16}
	pic := frame.NewPicture(1, 1)
	hdr := &syntax.SliceHeader{NalRefIDC: 0, FrameNum: 3}

	b.store(pic, hdr)

	if len(b.refs) != 0 {
		t.Fatalf("refs = %v, want empty: a non-reference picture must not be stored", b.refs)
	}
}

func TestStoreIDRClearsBufferAndMarksLongTerm(t *testing.T) {
	b := &dpb{maxNumRefs: 4, maxFrameNum: 16, refs: []*refFrame{newRef(1, false, 0), newRef(2, true, 0)}}
	pic := frame.NewPicture(1, 1)
	hdr := &syntax.SliceHeader{IDR: true, NalRefIDC: 1, FrameNum: 0, LongTermReference: true}

	b.store(pic, hdr)

	if len(b.refs) != 1 {
		t.Fatalf("refs = %v, want exactly one entry after an IDR", b.refs)
	}
	if b.refs[0].pic != pic {
		t.Fatalf("stored pic mismatch")
	}
	if !b.refs[0].longTerm {
		t.Fatalf("long_term_reference_flag on an IDR must mark the entry long term")
	}
}

func TestClear(t *testing.T) {
	b := &dpb{
		refs:               []*refFrame{newRef(1, false, 0)},
		prevPicOrderCntMsb: 7,
		prevPicOrderCntLsb: 3,
		prevFrameNumOffset: 9,
		prevFrameNum:       5,
	}
	b.clear()

	if len(b.refs) != 0 {
		t.Fatalf("refs not cleared: %v", b.refs)
	}
	if b.prevPicOrderCntMsb != 0 || b.prevPicOrderCntLsb != 0 || b.prevFrameNumOffset != 0 || b.prevFrameNum != 0 {
		t.Fatalf("prev state not reset: %+v", b)
	}
}

func newRefAt(poc int, frameNum uint32) *refFrame {
	r := newRef(frameNum, false, 0)
	r.pic.POC = poc
	return r
}

func assertPicOrder(t *testing.T, label string, got []*frame.Picture, want []*refFrame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s has %d entries, want %d", label, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i].pic {
			t.Fatalf("%s index %d holds the picture with count %d, want %d",
				label, i, got[i].POC, want[i].pic.POC)
		}
	}
}

func TestBuildListsBOrdersAroundTheCurrentPicture(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	past1 := newRefAt(2, 1)
	past2 := newRefAt(6, 3)
	future1 := newRefAt(10, 5)
	future2 := newRefAt(14, 7)
	long := newRef(9, true, 1)
	long.pic.POC = 100
	b.refs = []*refFrame{future2, past1, long, future1, past2}

	l0, l1 := b.buildListsB(newTestHeader(8, false, 1), 8, 5, 5)
	assertPicOrder(t, "list 0", l0, []*refFrame{past2, past1, future1, future2, long})
	assertPicOrder(t, "list 1", l1, []*refFrame{future1, future2, past2, past1, long})
}

func TestBuildListsBSwapsTheFirstTwoWhenBothListsMatch(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	a := newRefAt(2, 1)
	c := newRefAt(4, 2)
	b.refs = []*refFrame{a, c}

	l0, l1 := b.buildListsB(newTestHeader(3, false, 1), 6, 2, 2)
	assertPicOrder(t, "list 0", l0, []*refFrame{c, a})
	assertPicOrder(t, "list 1", l1, []*refFrame{a, c})
}

func TestBuildListsBKeepsASingleEntryListUnswapped(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	only := newRefAt(2, 1)
	b.refs = []*refFrame{only}

	l0, l1 := b.buildListsB(newTestHeader(3, false, 1), 6, 1, 1)
	assertPicOrder(t, "list 0", l0, []*refFrame{only})
	assertPicOrder(t, "list 1", l1, []*refFrame{only})
}

func TestBuildListsBTruncatesToTheActiveCounts(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	past := newRefAt(2, 1)
	future := newRefAt(10, 5)
	b.refs = []*refFrame{past, future}

	l0, l1 := b.buildListsB(newTestHeader(6, false, 1), 6, 1, 1)
	assertPicOrder(t, "list 0", l0, []*refFrame{past})
	assertPicOrder(t, "list 1", l1, []*refFrame{future})
}

func TestBuildListsBAppliesEachListsModifications(t *testing.T) {
	b := &dpb{maxFrameNum: 16}
	past := newRefAt(2, 1)
	future := newRefAt(10, 5)
	b.refs = []*refFrame{past, future}
	b.updatePicNums(6)

	hdr := newTestHeader(6, false, 1)
	hdr.ModificationL1Present = true
	hdr.RefPicListModificationL1 = []syntax.RefPicListModification{{IDC: 0, Value: 4}}

	_, l1 := b.buildListsB(hdr, 6, 2, 2)
	assertPicOrder(t, "list 1", l1, []*refFrame{past, future})
}

func TestSameFramesComparesIdentityAndLength(t *testing.T) {
	a := newRefAt(0, 0)
	c := newRefAt(2, 1)
	if !sameFrames([]*refFrame{a, c}, []*refFrame{a, c}) {
		t.Fatal("identical lists compared unequal")
	}
	if sameFrames([]*refFrame{a, c}, []*refFrame{c, a}) {
		t.Fatal("reordered lists compared equal")
	}
	if sameFrames([]*refFrame{a}, []*refFrame{a, c}) {
		t.Fatal("lists of different length compared equal")
	}
}

func TestAdaptiveMarkingReplacesTheSlidingWindow(t *testing.T) {
	b := &dpb{maxNumRefs: 3, maxFrameNum: 16}
	first := newRef(0, false, 0)
	second := newRef(1, false, 0)
	b.refs = []*refFrame{first, second}
	b.updatePicNums(2)

	hdr := newTestHeader(2, false, 1)
	hdr.AdaptiveRefPicMarking = true
	b.store(frame.NewPicture(1, 1), hdr)

	if len(b.refs) != 3 {
		t.Fatalf("adaptive marking left %d references, want 3: the sliding window ran as well", len(b.refs))
	}
	if b.refs[0] != first || b.refs[1] != second {
		t.Fatal("adaptive marking evicted a reference the stream never asked to evict")
	}
}

func TestSlidingWindowStillAppliesWithoutAdaptiveMarking(t *testing.T) {
	b := &dpb{maxNumRefs: 2, maxFrameNum: 16}
	oldest := newRef(0, false, 0)
	b.refs = []*refFrame{oldest, newRef(1, false, 0)}
	b.updatePicNums(2)

	b.store(frame.NewPicture(1, 1), newTestHeader(2, false, 1))

	if len(b.refs) != 2 {
		t.Fatalf("the sliding window left %d references, want 2", len(b.refs))
	}
	for _, r := range b.refs {
		if r == oldest {
			t.Fatal("the sliding window kept the oldest reference")
		}
	}
}
