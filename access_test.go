// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "testing"

// sliceAt writes a slice header for a picture identified by frameNum and poc,
// starting at the macroblock given.
func sliceAt(sps SPS, pps PPS, firstMB, frameNum, poc uint32, typ uint32, idr bool) Unit {
	return slice{firstMB: firstMB, typ: typ, frameNum: frameNum, pocLSB: poc, idr: idr}.build(sps, pps)
}

func TestSeveralSlicesOfOnePictureGroupTogether(t *testing.T) {
	sps, pps := plain()
	units := []Unit{
		sliceAt(sps, pps, 0, 1, 2, 2, false),
		sliceAt(sps, pps, 40, 1, 2, 2, false),
		sliceAt(sps, pps, 80, 1, 2, 2, false),
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 1 {
		t.Fatalf("%d pictures, want 1 -- a picture carried by three slices", len(pics))
	}
	if len(pics[0].Units) != 3 {
		t.Errorf("%d units in the picture, want 3", len(pics[0].Units))
	}
	if pics[0].Type != SliceI {
		t.Errorf("type %v, want I", pics[0].Type)
	}
}

// TestTwoPicturesBothBeginningAtMacroblockZeroAreTwo is the ordinary case, and the
// reason the macroblock-zero condition works at all.
func TestTwoPicturesBothBeginningAtMacroblockZeroAreTwo(t *testing.T) {
	sps, pps := plain()
	units := []Unit{
		sliceAt(sps, pps, 0, 1, 2, 2, false),
		sliceAt(sps, pps, 0, 2, 4, 0, false),
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 2 {
		t.Fatalf("%d pictures, want 2", len(pics))
	}
	if pics[0].Type != SliceI || pics[1].Type != SliceP {
		t.Errorf("types %v and %v, want I then P", pics[0].Type, pics[1].Type)
	}
}

// TestASliceRepeatingAPictureDoesNotBeginANewOne.
//
// ⛔ This is why macroblock zero alone is not the rule. A redundant slice carries a
// picture already carried, from macroblock zero, and identifies the SAME picture in
// every field. Counting it as new would report more pictures than the stream has --
// and a sample table built from that count would be wrong about every frame after
// it.
func TestASliceRepeatingAPictureDoesNotBeginANewOne(t *testing.T) {
	sps, pps := plain()
	first := sliceAt(sps, pps, 0, 1, 2, 2, false)
	units := []Unit{first, first}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 1 {
		t.Fatalf("%d pictures, want 1 -- the second slice repeats the first", len(pics))
	}
	if len(pics[0].Units) != 2 {
		t.Errorf("%d units, want both slices in the one picture", len(pics[0].Units))
	}
}

func TestEachIdentifyingFieldBeginsANewPicture(t *testing.T) {
	sps, pps := plain()
	sps.FrameMBSOnly = false
	pps.BottomFieldPicOrder = true
	base := slice{typ: 2, frameNum: 1, pocLSB: 2}
	for _, tc := range []struct {
		name string
		next slice
	}{
		{"a different frame number", slice{typ: 2, frameNum: 2, pocLSB: 2}},
		{"a different picture order count", slice{typ: 2, frameNum: 1, pocLSB: 3}},
		{"a different bottom delta", slice{typ: 2, frameNum: 1, pocLSB: 2, deltaBot: 1}},
		{"a field rather than a frame", slice{typ: 2, frameNum: 1, pocLSB: 2, fieldPic: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pics, err := SplitPictures([]Unit{base.build(sps, pps), tc.next.build(sps, pps)}, sps, pps)
			if err != nil {
				t.Fatalf("SplitPictures: %v", err)
			}
			if len(pics) != 2 {
				t.Errorf("%d pictures, want 2", len(pics))
			}
		})
	}

	// The other field of the same frame: field_pic is set in both and only the
	// field differs.
	top := slice{typ: 2, frameNum: 1, pocLSB: 2, fieldPic: true}
	bottom := slice{typ: 2, frameNum: 1, pocLSB: 2, fieldPic: true, bottom: true}
	pics, err := SplitPictures([]Unit{top.build(sps, pps), bottom.build(sps, pps)}, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 2 {
		t.Errorf("%d pictures for two fields of one frame, want 2", len(pics))
	}
}

func TestAnIDRBeginsAPictureAndIsASyncSample(t *testing.T) {
	sps, pps := plain()
	units := []Unit{
		sliceAt(sps, pps, 0, 0, 0, 2, true),
		sliceAt(sps, pps, 0, 1, 2, 0, false),
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 2 {
		t.Fatalf("%d pictures, want 2", len(pics))
	}
	if !pics[0].Sync {
		t.Error("an IDR picture is not a sync sample")
	}
	if pics[1].Sync {
		t.Error("a picture that is not an IDR was called a sync sample")
	}
}

// TestParameterSetsBelongToThePictureTheyPrecede: a decoder must hold them before
// the slice that uses them, so a sample gathered without them would not decode.
func TestParameterSetsBelongToThePictureTheyPrecede(t *testing.T) {
	sps, pps := plain()
	spsUnit := Unit{Type: UnitSPS, Payload: []byte{1, 2}}
	ppsUnit := Unit{Type: UnitPPS, Payload: []byte{3}}
	aud := Unit{Type: UnitAUD, Payload: []byte{4}}
	units := []Unit{
		aud, spsUnit, ppsUnit,
		sliceAt(sps, pps, 0, 0, 0, 2, true),
		aud,
		sliceAt(sps, pps, 0, 1, 2, 0, false),
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 2 {
		t.Fatalf("%d pictures, want 2", len(pics))
	}
	if len(pics[0].Units) != 4 {
		t.Errorf("the first picture holds %d units, want the delimiter, both sets and the slice", len(pics[0].Units))
	}
	if pics[0].Units[0].Type != UnitAUD || pics[0].Units[1].Type != UnitSPS {
		t.Errorf("the units are out of order: %v", pics[0].Units)
	}
	if len(pics[1].Units) != 2 {
		t.Errorf("the second picture holds %d units, want the delimiter and the slice", len(pics[1].Units))
	}
	// Bytes counts payloads and headers, which is what a sample is made of.
	want := 0
	for _, u := range pics[0].Units {
		want += len(u.Payload) + 1
	}
	if pics[0].Bytes != want {
		t.Errorf("Bytes = %d, want %d", pics[0].Bytes, want)
	}
}

// TestUnitsAfterTheLastSliceBelongToNoPicture: a trailing delimiter, or a
// parameter set for a picture that never arrived, is not a picture. Inventing one
// would add a frame to a sample table that holds no slice.
func TestUnitsAfterTheLastSliceBelongToNoPicture(t *testing.T) {
	sps, pps := plain()
	units := []Unit{
		sliceAt(sps, pps, 0, 0, 0, 2, true),
		{Type: UnitAUD, Payload: []byte{4}},
		{Type: UnitSPS, Payload: []byte{1}},
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 1 {
		t.Fatalf("%d pictures, want 1", len(pics))
	}
	if len(pics[0].Units) != 1 {
		t.Errorf("the picture gathered %d units, want only its slice", len(pics[0].Units))
	}
}

// TestSlicesBeforeAnyPictureBeganAreKept covers a stream joined part way through:
// the first slice seen starts at a macroblock other than zero, so nothing has
// begun. Dropping it would make a count of pictures disagree with a count of
// slices for a reason nobody could see.
func TestSlicesBeforeAnyPictureBeganAreKept(t *testing.T) {
	sps, pps := plain()
	units := []Unit{
		sliceAt(sps, pps, 40, 1, 2, 0, false),
		sliceAt(sps, pps, 80, 1, 2, 0, false),
		sliceAt(sps, pps, 0, 2, 4, 2, false),
	}
	pics, err := SplitPictures(units, sps, pps)
	if err != nil {
		t.Fatalf("SplitPictures: %v", err)
	}
	if len(pics) != 2 {
		t.Fatalf("%d pictures, want 2 -- the joined one and the whole one", len(pics))
	}
	if len(pics[0].Units) != 2 || pics[0].Type != SliceP {
		t.Errorf("the joined picture: %d units, type %v", len(pics[0].Units), pics[0].Type)
	}
}

// TestASliceThatCannotBeReadStopsTheWalkAndKeepsWhatCameBefore.
func TestASliceThatCannotBeReadStopsTheWalkAndKeepsWhatCameBefore(t *testing.T) {
	sps, pps := plain()
	good := sliceAt(sps, pps, 0, 0, 0, 2, true)
	bad := Unit{Type: UnitNonIDR, Payload: nil}
	pics, err := SplitPictures([]Unit{good, bad}, sps, pps)
	if err == nil {
		t.Fatal("a slice with no header at all was accepted")
	}
	if len(pics) != 1 {
		t.Errorf("%d pictures kept, want the one that did read", len(pics))
	}
}

func TestAStreamWithNoSlicesHoldsNoPictures(t *testing.T) {
	sps, pps := plain()
	pics, err := SplitPictures([]Unit{{Type: UnitSPS, Payload: []byte{1}}}, sps, pps)
	if err != nil {
		t.Fatal(err)
	}
	if len(pics) != 0 {
		t.Errorf("%d pictures from a stream with no slice", len(pics))
	}
}
