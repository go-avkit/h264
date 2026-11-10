package decoder

import (
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

func TestMinPositivePrefersTheSmallerUsedReference(t *testing.T) {
	cases := []struct{ a, b, want int8 }{
		{1, 2, 1},
		{2, 1, 1},
		{0, 3, 0},
		{-1, 2, 2},
		{2, -1, 2},
		{-1, -1, -1},
	}
	for _, c := range cases {
		if got := minPositive(c.a, c.b); got != c.want {
			t.Fatalf("minPositive(%d, %d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func newSliceDecoderForScaling(currPOC, poc0, poc1 int, longTerm bool) *sliceDecoder {
	p0 := frame.NewPicture(1, 1)
	p0.POC = poc0
	p0.LongTerm = longTerm
	p1 := frame.NewPicture(1, 1)
	p1.POC = poc1
	cur := frame.NewPicture(1, 1)
	cur.POC = currPOC
	return &sliceDecoder{pic: cur, refList: []*frame.Picture{p0}, refListL1: []*frame.Picture{p1}}
}

func TestDistScaleFactorHalvesAMidpointPicture(t *testing.T) {
	d := newSliceDecoderForScaling(4, 0, 8, false)
	got, ok := d.distScaleFactor(d.refList[0])
	if !ok {
		t.Fatal("the distance was reported as unscalable")
	}
	if got != 128 {
		t.Fatalf("distScaleFactor = %d, want 128 for a picture halfway between", got)
	}
	if v := scaleMV(20, got); v != 10 {
		t.Fatalf("scaleMV(20) = %d, want 10", v)
	}
}

func TestDistScaleFactorRefusesLongTermAndCoincidentPictures(t *testing.T) {
	if _, ok := newSliceDecoderForScaling(4, 0, 8, true).distScaleFactor(nil); ok {
		t.Fatal("a nil picture was accepted")
	}
	d := newSliceDecoderForScaling(4, 0, 8, true)
	if _, ok := d.distScaleFactor(d.refList[0]); ok {
		t.Fatal("a long term picture was scaled")
	}
	same := newSliceDecoderForScaling(4, 8, 8, false)
	if _, ok := same.distScaleFactor(same.refList[0]); ok {
		t.Fatal("two pictures with the same count were scaled")
	}
}

func TestImplicitWeightsSplitEvenlyAtTheMidpoint(t *testing.T) {
	d := newSliceDecoderForScaling(4, 0, 8, false)
	w0, w1 := d.implicitWeights(0, 0)
	if w0 != 32 || w1 != 32 {
		t.Fatalf("implicit weights = %d/%d, want 32/32", w0, w1)
	}
}

func TestImplicitWeightsLeanTowardTheNearerPicture(t *testing.T) {
	d := newSliceDecoderForScaling(2, 0, 8, false)
	w0, w1 := d.implicitWeights(0, 0)
	if w0+w1 != 64 {
		t.Fatalf("implicit weights %d/%d do not sum to 64", w0, w1)
	}
	if w0 <= w1 {
		t.Fatalf("implicit weights = %d/%d, want the nearer picture to weigh more", w0, w1)
	}
}

func TestImplicitWeightsFallBackToAnEvenSplit(t *testing.T) {
	long := newSliceDecoderForScaling(4, 0, 8, true)
	if w0, w1 := long.implicitWeights(0, 0); w0 != 32 || w1 != 32 {
		t.Fatalf("long term implicit weights = %d/%d, want 32/32", w0, w1)
	}
	same := newSliceDecoderForScaling(4, 8, 8, false)
	if w0, w1 := same.implicitWeights(0, 0); w0 != 32 || w1 != 32 {
		t.Fatalf("coincident implicit weights = %d/%d, want 32/32", w0, w1)
	}
	far := newSliceDecoderForScaling(-400, 0, 8, false)
	if w0, w1 := far.implicitWeights(0, 0); w0 != 32 || w1 != 32 {
		t.Fatalf("distant implicit weights = %d/%d, want 32/32", w0, w1)
	}
}

func TestMapColToList0FindsThePictureByCount(t *testing.T) {
	a := frame.NewPicture(1, 1)
	a.POC = 4
	b := frame.NewPicture(1, 1)
	b.POC = 12
	d := &sliceDecoder{refList: []*frame.Picture{a, b}}
	if got := d.mapColToList0(12); got != 1 {
		t.Fatalf("mapColToList0(12) = %d, want 1", got)
	}
	if got := d.mapColToList0(99); got != 0 {
		t.Fatalf("mapColToList0 of an absent count = %d, want 0", got)
	}
}
