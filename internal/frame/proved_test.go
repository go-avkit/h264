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
	p := &Picture{Y: make([]byte, 16), StrideY: 4}
	if p.MCProved {
		t.Fatal("a Picture with planes but no NewPicture claims to be proved")
	}
	p.proveMC()
	if p.MCProved {
		t.Fatal("planes the kernels refuse (no chroma) were proved")
	}
}
