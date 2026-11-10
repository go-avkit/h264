package level

import (
	"errors"
	"testing"

	h264 "github.com/go-avkit/h264"
)

func spsAt(levelIDC uint8, widthMBs, heightMBs int) *h264.SPS {
	return &h264.SPS{
		Profile:      77,
		Level:        levelIDC,
		ChromaFormat: 1,
		FrameMBSOnly: true,
		MBWidth:      uint32(widthMBs),
		MBHeight:     uint32(heightMBs),
	}
}

func TestCheckSPSAcceptsTheLevelsOwnMaximum(t *testing.T) {
	for _, l := range table {
		s := spsAt(l.IDC, 1, l.MaxFS)
		bound := isqrt(8 * l.MaxFS)
		if l.MaxFS > bound {
			s = spsAt(l.IDC, bound, l.MaxFS/bound)
		}
		if err := CheckSPS(s); err != nil {
			t.Fatalf("level %d at %dx%d macroblocks: %v", l.IDC, s.MBWidth, s.MBHeight, err)
		}
	}
}

func TestCheckSPSRejectsWhatTheLevelCannotCarry(t *testing.T) {
	l, ok := Lookup(30)
	if !ok {
		t.Fatal("level 3.0 missing from the table")
	}
	tooBig := spsAt(30, 41, l.MaxFS/41+2)
	if err := CheckSPS(tooBig); !errors.Is(err, ErrExceedsLevel) {
		t.Fatalf("frame size above MaxFS: %v, want ErrExceedsLevel", err)
	}
	bound := isqrt(8 * l.MaxFS)
	tooWide := spsAt(30, bound+1, 1)
	if err := CheckSPS(tooWide); !errors.Is(err, ErrExceedsLevel) {
		t.Fatalf("%d macroblocks wide: %v, want ErrExceedsLevel", bound+1, err)
	}
	tooTall := spsAt(30, 1, bound+1)
	if err := CheckSPS(tooTall); !errors.Is(err, ErrExceedsLevel) {
		t.Fatalf("%d macroblocks high: %v, want ErrExceedsLevel", bound+1, err)
	}
}

func TestCheckSPSRejectsMoreReferencesThanTheBufferHolds(t *testing.T) {
	l, ok := Lookup(40)
	if !ok {
		t.Fatal("level 4.0 missing from the table")
	}
	s := spsAt(40, 120, 68)
	dpb := l.DpbFrames(120 * 68)
	s.MaxNumRefFrames = uint32(dpb)
	if err := CheckSPS(s); err != nil {
		t.Fatalf("%d reference frames at 1080p on level 4.0: %v", dpb, err)
	}
	s.MaxNumRefFrames = uint32(dpb + 1)
	if err := CheckSPS(s); !errors.Is(err, ErrExceedsLevel) {
		t.Fatalf("%d reference frames: %v, want ErrExceedsLevel", dpb+1, err)
	}
}

func TestCheckSPSRejectsLevelsOutsideTableA1(t *testing.T) {
	for _, idc := range []uint8{0, 1, 8, 14, 19, 23, 33, 43, 59, 63, 100, 255} {
		if err := CheckSPS(spsAt(idc, 1, 1)); !errors.Is(err, ErrUnknownLevel) {
			t.Fatalf("level_idc %d: %v, want ErrUnknownLevel", idc, err)
		}
	}
}

func TestDeclaredResolvesLevel1b(t *testing.T) {
	s := spsAt(11, 1, 1)
	s.ConstraintFlags = 0x10
	oneB, ok := Declared(s)
	if !ok || oneB.MaxFS != 99 || oneB.MaxDpbMbs != 396 || oneB.MaxBR != 128 || oneB.MaxCPB != 350 {
		t.Fatalf("level_idc 11 with constraint_set3_flag = %+v, %v", oneB, ok)
	}
	alias := spsAt(IDCLevel1bAlias, 1, 1)
	got, ok := Declared(alias)
	if !ok || got != oneB {
		t.Fatalf("level_idc %d = %+v, %v", IDCLevel1bAlias, got, ok)
	}
	full, ok := Declared(spsAt(11, 1, 1))
	if !ok || full.MaxFS != 396 {
		t.Fatalf("level_idc 11 without constraint_set3_flag = %+v, %v, want level 1.1", full, ok)
	}
	high := spsAt(IDCLevel1bAlias, 1, 1)
	high.Profile = 100
	got, ok = Declared(high)
	if !ok || got != oneB {
		t.Fatalf("level_idc %d on a High profile stream = %+v, %v, want level 1b",
			IDCLevel1bAlias, got, ok)
	}
}

func TestDpbFramesNeverExceedsSixteen(t *testing.T) {
	for _, l := range table {
		if n := l.DpbFrames(1); n != 16 {
			t.Fatalf("level %d with a one macroblock picture: DpbFrames = %d, want 16", l.IDC, n)
		}
		if n := l.DpbFrames(l.MaxFS); n < 1 {
			t.Fatalf("level %d at its own maximum frame size: DpbFrames = %d", l.IDC, n)
		}
		if n := l.DpbFrames(0); n != 0 {
			t.Fatalf("level %d with no macroblocks: DpbFrames = %d, want 0", l.IDC, n)
		}
	}
}

func TestHighProfileDoesNotResolveLevel1b(t *testing.T) {
	s := spsAt(11, 11, 9)
	s.Profile = 100
	s.ConstraintFlags = 0x10
	s.MBWidth = 22
	s.MBHeight = 18
	if err := CheckSPS(s); err != nil {
		t.Fatalf("High profile level 1.1 at 396 macroblocks: %v", err)
	}
}
