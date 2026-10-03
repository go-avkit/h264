// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"errors"
	"testing"
)

// counted is one picture handed to the counter.
type counted struct {
	typ      uint32
	refIDC   uint8
	idr      bool
	frameNum uint32
	pocLSB   uint32
	deltaBot int32
	fieldPic bool
	bottom   bool
	resets   bool
}

// count runs a sequence of pictures through a fresh counter.
func count(t *testing.T, sps SPS, pics []counted) []POC {
	t.Helper()
	var c POCCounter
	out := make([]POC, 0, len(pics))
	for i, p := range pics {
		u := Unit{Type: UnitNonIDR, RefIDC: p.refIDC}
		if p.idr {
			u.Type = UnitIDR
		}
		h := SliceHeader{Type: SliceType(p.typ), IDR: p.idr, FrameNum: p.frameNum,
			POCLSB: p.pocLSB, DeltaPOCBottom: p.deltaBot,
			FieldPic: p.fieldPic, BottomField: p.bottom}
		got, err := c.Next(u, h, SliceReferences{ResetsPOC: p.resets}, sps)
		if err != nil {
			t.Fatalf("picture %d: %v", i, err)
		}
		out = append(out, got)
	}
	return out
}

func setsType0(log2MaxLsb uint8) SPS {
	return SPS{POCType: 0, Log2MaxPOCLSB: log2MaxLsb, Log2MaxFrameNum: 4, FrameMBSOnly: true}
}

// TestTheTwoHalvesOfTheWrapAreNotSymmetric.
//
// ⛔ The format says the wrap UP happens when the low bits fell by AT LEAST half a
// cycle, and the wrap DOWN when they rose by MORE than half. Writing both the same
// way misplaces the picture that sits exactly half a cycle away -- which is what a
// long run of B pictures produces, since their counts spread out from the anchors.
//
// With four bits the cycle is 16 and half of it is 8.
func TestTheTwoHalvesOfTheWrapAreNotSymmetric(t *testing.T) {
	sps := setsType0(4) // MaxPicOrderCntLsb = 16

	// ⛔ The state to test from has to be REACHED without wrapping, in steps of no
	// more than half a cycle. Jumping straight to it wraps on the way, and then the
	// test measures the step it did not mean to take -- which is what the first
	// version of this did: it set out to test a fall from 12 and got a rise of 12
	// from zero instead.
	reach := func(lsbs ...uint32) []counted {
		pics := []counted{{typ: 2, refIDC: 3, idr: true, pocLSB: 0}}
		for _, l := range lsbs {
			pics = append(pics, counted{typ: 0, refIDC: 2, pocLSB: l})
		}
		return pics
	}
	for _, c := range []struct {
		name  string
		steps []uint32 // the last one is the step under test
		want  int32
	}{
		// A fall of exactly half wraps UP: the condition is "at least".
		{"fall of exactly half", []uint32{8, 0}, 16},
		{"fall of more than half", []uint32{4, 8, 12, 2}, 18},
		{"fall of less than half", []uint32{6, 2}, 2},
		// A rise of exactly half does NOT wrap down: the condition is "more than".
		{"rise of exactly half", []uint32{8}, 8},
		{"rise of more than half", []uint32{2, 12}, -4},
		{"rise of less than half", []uint32{2, 6}, 6},
	} {
		got := count(t, sps, reach(c.steps...))
		last := got[len(got)-1].Value
		if last != c.want {
			t.Errorf("%s (steps %v): count %d, want %d", c.name, c.steps, last, c.want)
		}
	}
}

// TestOnlyAReferencePictureMovesTheCarry.
//
// ⛔ A disposable picture reads the carry and leaves it where it was. A counter
// that updated from every picture would place every picture after the first
// disposable one wrongly -- and they would stay in order, so nothing would look
// broken until two sequences were compared.
func TestOnlyAReferencePictureMovesTheCarry(t *testing.T) {
	sps := setsType0(4)
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, pocLSB: 0},
		{typ: 0, refIDC: 2, pocLSB: 6},  // reference: the carry is now 6
		{typ: 1, refIDC: 0, pocLSB: 14}, // disposable: a rise of 8, which does not wrap
		{typ: 0, refIDC: 2, pocLSB: 2},  // reference: a fall of 4 from 6
	})
	if got[2].Value != 14 {
		t.Errorf("the disposable picture counted %d, want 14", got[2].Value)
	}
	// ⛔ This is the whole test. Had the disposable picture moved the carry to 14,
	// the last picture's low bits of 2 would be a fall of 12 -- at least half a
	// cycle -- and would wrap to 18. The two answers are far apart on purpose.
	if got[3].Value != 2 {
		t.Errorf("the picture after it counted %d, want 2: the disposable picture moved the carry", got[3].Value)
	}
}

// TestAnIDRBeginsTheCountAgain: an IDR starts a coded video sequence, and nothing
// before it counts. Counts from two sequences are not comparable, which is why one
// restarting at zero is right rather than a collision.
func TestAnIDRBeginsTheCountAgain(t *testing.T) {
	sps := setsType0(4)
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, pocLSB: 0},
		{typ: 0, refIDC: 2, pocLSB: 12},
		{typ: 2, refIDC: 3, idr: true, pocLSB: 0},
		{typ: 0, refIDC: 2, pocLSB: 4},
	})
	if got[0].Value != 0 || got[2].Value != 0 {
		t.Errorf("the two IDRs counted %d and %d, want 0 and 0", got[0].Value, got[2].Value)
	}
	// ⛔ Without the IDR reset, 4 after a carry of 12 would read as a wrap and
	// count 20.
	if got[3].Value != 4 {
		t.Errorf("the picture after the second IDR counted %d, want 4", got[3].Value)
	}
}

// TestAFrameTakesTheSmallerOfItsTwoCounts: a frame has a top and a bottom count,
// and is ordered by the smaller. A reader using the top alone would order two
// frames wrongly whenever the bottom field leads.
func TestAFrameTakesTheSmallerOfItsTwoCounts(t *testing.T) {
	sps := setsType0(4)
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, pocLSB: 0, deltaBot: 0},
		{typ: 0, refIDC: 2, pocLSB: 4, deltaBot: 2},  // bottom later
		{typ: 0, refIDC: 2, pocLSB: 6, deltaBot: -3}, // bottom EARLIER
	})
	if got[1].Top != 4 || got[1].Bottom != 6 || got[1].Value != 4 {
		t.Errorf("top later: %+v", got[1])
	}
	if got[2].Top != 6 || got[2].Bottom != 3 || got[2].Value != 3 {
		t.Errorf("bottom earlier: %+v, want the smaller of 6 and 3", got[2])
	}
}

func TestAFieldCountsOnlyItself(t *testing.T) {
	sps := SPS{POCType: 0, Log2MaxPOCLSB: 4, Log2MaxFrameNum: 4} // FrameMBSOnly false
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, pocLSB: 0, fieldPic: true},
		{typ: 0, refIDC: 2, pocLSB: 4, fieldPic: true},
		{typ: 0, refIDC: 2, pocLSB: 6, fieldPic: true, bottom: true},
	})
	if got[1].Top != 4 || got[1].Bottom != 0 || got[1].Value != 4 {
		t.Errorf("a top field: %+v", got[1])
	}
	if got[2].Bottom != 6 || got[2].Top != 0 || got[2].Value != 6 {
		t.Errorf("a bottom field: %+v", got[2])
	}
}

// TestTypeTwoDerivesTheOrderFromTheDecodeOrder: the pictures arrive in the order
// they are shown, so the count is the frame number doubled -- with a disposable
// picture sitting one before the reference picture it precedes.
func TestTypeTwoDerivesTheOrderFromTheDecodeOrder(t *testing.T) {
	sps := SPS{POCType: 2, Log2MaxFrameNum: 4, FrameMBSOnly: true}
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, frameNum: 0},
		{typ: 0, refIDC: 2, frameNum: 1},
		{typ: 1, refIDC: 0, frameNum: 2}, // disposable
		{typ: 0, refIDC: 2, frameNum: 2},
	})
	for i, want := range []int32{0, 2, 3, 4} {
		if got[i].Value != want {
			t.Errorf("picture %d counted %d, want %d", i, got[i].Value, want)
		}
	}
	// ⛔ A disposable picture counts ODD, one less than the reference picture that
	// follows: it is shown just before it. A counter giving it the same even number
	// would tie with that picture and the order between them would be whatever the
	// sort happened to do.
	if got[2].Value >= got[3].Value {
		t.Errorf("a disposable picture counted %d, not before the %d that follows", got[2].Value, got[3].Value)
	}
}

// TestTypeTwoSeesTheFrameNumberWrap.
//
// ⛔ The frame number wraps, and the only sign is that this picture's is SMALLER
// than the one before. Missing the wrap makes every count after it a whole cycle
// too low -- and they stay in order, so nothing looks wrong until pictures from
// either side of the wrap are compared.
func TestTypeTwoSeesTheFrameNumberWrap(t *testing.T) {
	sps := SPS{POCType: 2, Log2MaxFrameNum: 2, FrameMBSOnly: true} // the cycle is 4
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, frameNum: 0},
		{typ: 0, refIDC: 2, frameNum: 1},
		{typ: 0, refIDC: 2, frameNum: 2},
		{typ: 0, refIDC: 2, frameNum: 3},
		{typ: 0, refIDC: 2, frameNum: 0}, // wrapped
		{typ: 0, refIDC: 2, frameNum: 1},
	})
	for i, want := range []int32{0, 2, 4, 6, 8, 10} {
		if got[i].Value != want {
			t.Errorf("picture %d counted %d, want %d", i, got[i].Value, want)
		}
	}
	// And the counts never go backwards, which is the property the wrap exists for.
	for i := 1; i < len(got); i++ {
		if got[i].Value <= got[i-1].Value {
			t.Errorf("count fell from %d to %d at picture %d", got[i-1].Value, got[i].Value, i)
		}
	}
}

// TestARestartAppliesToWhatFollowsNotToThePictureItself.
//
// ⛔ Operation five empties the reference set and restarts the count. The picture
// carrying it KEEPS the count it was given -- the restart applies to what comes
// after. A counter that zeroed this picture too would place it before the pictures
// it was coded after.
func TestARestartAppliesToWhatFollowsNotToThePictureItself(t *testing.T) {
	for _, sps := range []SPS{setsType0(4), {POCType: 2, Log2MaxFrameNum: 4, FrameMBSOnly: true}} {
		got := count(t, sps, []counted{
			{typ: 2, refIDC: 3, idr: true, pocLSB: 0, frameNum: 0},
			{typ: 0, refIDC: 2, pocLSB: 8, frameNum: 1},
			{typ: 0, refIDC: 2, pocLSB: 10, frameNum: 2, resets: true},
			{typ: 0, refIDC: 2, pocLSB: 2, frameNum: 1},
		})
		if got[2].Value == 0 {
			t.Errorf("type %d: the picture carrying the restart was itself zeroed", sps.POCType)
		}
		if got[2].Value <= got[1].Value {
			t.Errorf("type %d: the restarting picture counted %d, not after the %d before it",
				sps.POCType, got[2].Value, got[1].Value)
		}
		// What follows counts from zero again.
		if sps.POCType == 0 && got[3].Value != 2 {
			t.Errorf("type 0: after the restart, %d, want 2", got[3].Value)
		}
		if sps.POCType == 2 && got[3].Value != 2 {
			t.Errorf("type 2: after the restart, %d, want 2", got[3].Value)
		}
	}
}

// TestResetForgetsEverything is what a seek is: the pictures before the new
// position are not the ones the counts should follow from.
func TestResetForgetsEverything(t *testing.T) {
	sps := setsType0(4)
	var c POCCounter
	run := func(p counted) POC {
		u := Unit{Type: UnitNonIDR, RefIDC: p.refIDC}
		if p.idr {
			u.Type = UnitIDR
		}
		h := SliceHeader{Type: SliceType(p.typ), IDR: p.idr, FrameNum: p.frameNum, POCLSB: p.pocLSB}
		got, err := c.Next(u, h, SliceReferences{}, sps)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	run(counted{typ: 2, refIDC: 3, idr: true, pocLSB: 0})
	run(counted{typ: 0, refIDC: 2, pocLSB: 12})
	// Without a reset, low bits of 2 after a carry of 12 read as a wrap.
	c.Reset()
	if got := run(counted{typ: 0, refIDC: 2, pocLSB: 2}); got.Value != 2 {
		t.Errorf("after Reset: %d, want 2", got.Value)
	}
}

// TestATypeThisDoesNotDeriveIsRefused.
//
// ⛔ Type one needs three offsets the set reader does not read, so there is nothing
// here to compute from. Measured over 158 sequences from one library, every one was
// type zero or type two and none was type one -- and a derivation no file can test
// is how a wrong answer gets shipped looking right.
func TestATypeThisDoesNotDeriveIsRefused(t *testing.T) {
	var c POCCounter
	for _, pocType := range []uint32{1, 3, 99} {
		sps := SPS{POCType: pocType, Log2MaxPOCLSB: 4, Log2MaxFrameNum: 4, FrameMBSOnly: true}
		_, err := c.Next(Unit{Type: UnitIDR, RefIDC: 3}, SliceHeader{IDR: true}, SliceReferences{}, sps)
		if !errors.Is(err, ErrPOCType) {
			t.Errorf("type %d: err = %v, want ErrPOCType", pocType, err)
		}
		if !errors.Is(err, ErrSliceHeader) {
			t.Errorf("type %d: the refusal does not join the package's error: %v", pocType, err)
		}
	}
}

// TestAStreamJoinedPartWayCountsFromWhereItStarts: the first picture seen is not
// read as following something, so a stream joined after its IDR still orders.
func TestAStreamJoinedPartWayCountsFromWhereItStarts(t *testing.T) {
	sps := setsType0(4)
	got := count(t, sps, []counted{
		{typ: 0, refIDC: 2, pocLSB: 6}, // no IDR first
		{typ: 0, refIDC: 2, pocLSB: 8},
	})
	if got[0].Value != 6 || got[1].Value != 8 {
		t.Errorf("joined part way: %d then %d, want 6 then 8", got[0].Value, got[1].Value)
	}
}

// TestTypeTwoCountsAFieldAsItsOwnHalf: a field has only its own count, and which
// half it is decides where the number goes. A reader filling both would claim a
// field it was never given.
func TestTypeTwoCountsAFieldAsItsOwnHalf(t *testing.T) {
	sps := SPS{POCType: 2, Log2MaxFrameNum: 4} // FrameMBSOnly false: fields allowed
	got := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, frameNum: 0, fieldPic: true},
		{typ: 0, refIDC: 2, frameNum: 1, fieldPic: true},
		{typ: 0, refIDC: 2, frameNum: 2, fieldPic: true, bottom: true},
	})
	if got[1].Top != 2 || got[1].Bottom != 0 || got[1].Value != 2 {
		t.Errorf("a top field: %+v, want the count in Top alone", got[1])
	}
	if got[2].Bottom != 4 || got[2].Top != 0 || got[2].Value != 4 {
		t.Errorf("a bottom field: %+v, want the count in Bottom alone", got[2])
	}
	// A frame in the same sequence fills both.
	all := count(t, sps, []counted{
		{typ: 2, refIDC: 3, idr: true, frameNum: 0},
		{typ: 0, refIDC: 2, frameNum: 1},
	})
	if all[1].Top != 2 || all[1].Bottom != 2 || all[1].Value != 2 {
		t.Errorf("a frame: %+v, want 2 in both", all[1])
	}
}
