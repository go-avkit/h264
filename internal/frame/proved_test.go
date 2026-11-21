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
