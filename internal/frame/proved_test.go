package frame

import "testing"

func TestNewPictureIsProvedForMotionCompensation(t *testing.T) {
	p := NewPicture(3, 2)
	if !p.MCProved {
		t.Fatal("NewPicture left MCProved false")
	}
	p.Recycle()
	if !p.MCProved {
		t.Fatal("Recycle cleared the proof of planes that are the same memory")
	}
}

// A Picture copied by value and given other planes keeps MCProved and the old
// proof; Proved must see that the planes are no longer the proved memory.
func TestACopyWithNewPlanesIsNotProved(t *testing.T) {
	orig := NewPicture(2, 2)
	for name, mutate := range map[string]func(*Picture){
		"Y":  func(p *Picture) { p.Y = append([]byte(nil), p.Y...) },
		"Cb": func(p *Picture) { p.Cb = append([]byte(nil), p.Cb...) },
		"Cr": func(p *Picture) { p.Cr = append([]byte(nil), p.Cr...) },
	} {
		cp := *orig
		if !cp.Proved() {
			t.Fatalf("%s: a plain copy shares the planes and stays proved", name)
		}
		mutate(&cp)
		if cp.Proved() {
			t.Errorf("%s: a copy with a new plane is still proved", name)
		}
		if !orig.Proved() {
			t.Errorf("%s: the original lost its proof", name)
		}
	}
	empty := *orig
	empty.Y = nil
	if empty.Proved() {
		t.Error("a picture without luma is proved")
	}
}

func TestHeadOfAPlane(t *testing.T) {
	b := []byte{7, 8}
	if head(b) != &b[0] {
		t.Error("head is not the first sample")
	}
	if head(nil) != nil || head([]byte{}) != nil {
		t.Error("head of an empty plane is not nil")
	}
}

func TestAPictureLiteralIsNotProved(t *testing.T) {
	if (&Picture{}).MCProved {
		t.Fatal("a literal Picture claims to be proved")
	}
	good := NewPicture(1, 1)
	for _, c := range []struct {
		name string
		p    Picture
	}{
		{"no luma", Picture{Cb: good.Cb, Cr: good.Cr, StrideY: good.StrideY, StrideC: good.StrideC}},
		{"no chroma", Picture{Y: good.Y, StrideY: good.StrideY, StrideC: good.StrideC}},
	} {
		p := c.p
		p.proveMC()
		if p.MCProved {
			t.Errorf("%s: planes the kernels refuse were proved", c.name)
		}
	}
	whole := Picture{Y: good.Y, Cb: good.Cb, Cr: good.Cr, StrideY: good.StrideY, StrideC: good.StrideC}
	whole.proveMC()
	if !whole.MCProved {
		t.Error("planes of a NewPicture were refused")
	}
}
