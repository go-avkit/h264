package syntax

import "testing"

func TestMaxDpbMbsRisesWithTheLevel(t *testing.T) {
	prev := 0
	for _, e := range maxDpbMbsByLevel {
		if e.levelIDC == 9 || e.constraint3 {
			continue
		}
		if e.maxDpbMbs < prev {
			t.Fatalf("level %d has %d macroblocks of buffer, below the %d before it",
				e.levelIDC, e.maxDpbMbs, prev)
		}
		prev = e.maxDpbMbs
	}
}

func TestMaxDpbFramesAtTheLevelsTableAnchors(t *testing.T) {
	cases := []struct {
		level     uint8
		widthMBs  int
		heightMBs int
		want      int
	}{
		{30, 45, 36, 5},
		{31, 80, 45, 5},
		{40, 120, 68, 4},
		{10, 11, 9, 4},
		{51, 120, 68, 16},
	}
	for _, c := range cases {
		s := &SPS{
			LevelIDC:                  c.level,
			PicWidthInMbsMinus1:       uint32(c.widthMBs - 1),
			PicHeightInMapUnitsMinus1: uint32(c.heightMBs - 1),
			FrameMbsOnly:              true,
		}
		if got := s.MaxDpbFrames(); got != c.want {
			t.Fatalf("level %d at %dx%d macroblocks: MaxDpbFrames() = %d, want %d",
				c.level, c.widthMBs, c.heightMBs, got, c.want)
		}
	}
}

func TestLevel1bTakesTheSmallerBuffer(t *testing.T) {
	base := SPS{LevelIDC: 11, PicWidthInMbsMinus1: 10, PicHeightInMapUnitsMinus1: 8, FrameMbsOnly: true}
	full := base
	if full.MaxDpbFrames() != 9 {
		t.Fatalf("level 1.1 MaxDpbFrames() = %d, want 9", full.MaxDpbFrames())
	}
	oneB := base
	oneB.ConstraintSet = 0x10
	if oneB.MaxDpbFrames() != 4 {
		t.Fatalf("level 1b MaxDpbFrames() = %d, want 4", oneB.MaxDpbFrames())
	}
}

func TestMaxNumReorderPrefersTheBitstreamRestriction(t *testing.T) {
	s := &SPS{LevelIDC: 51, PicWidthInMbsMinus1: 10, PicHeightInMapUnitsMinus1: 8, FrameMbsOnly: true}
	if got := s.MaxNumReorder(); got != 16 {
		t.Fatalf("without a restriction MaxNumReorder() = %d, want 16", got)
	}
	s.VUIPresent = true
	s.VUI.BitstreamRestriction = true
	s.VUI.MaxNumReorderFrames = 2
	if got := s.MaxNumReorder(); got != 2 {
		t.Fatalf("with a restriction MaxNumReorder() = %d, want 2", got)
	}
}

func TestRestrictBitstreamDeclaresTheReordering(t *testing.T) {
	cases := []struct {
		refs, reorder int
		dpb           uint32
	}{
		{1, 0, 1},
		{4, 0, 4},
		{0, 0, 1},
		{2, 1, 2},
		{1, 2, 2},
	}
	for _, c := range cases {
		s := baseSPS()
		s.MaxNumRefFrames = uint32(c.refs)
		s.VUIPresent = true
		s.VUI.TimingInfoPresent = true
		s.VUI.NumUnitsInTick = 1
		s.VUI.TimeScale = 60
		s.VUI.FixedFrameRate = true
		s.RestrictBitstream(c.reorder)
		rbsp, err := WriteSPS(s)
		if err != nil {
			t.Fatalf("WriteSPS: %v", err)
		}
		got, err := ParseSPS(rbsp)
		if err != nil {
			t.Fatalf("ParseSPS: %v", err)
		}
		v := got.VUI
		if !got.VUIPresent || !v.BitstreamRestriction {
			t.Fatalf("%d references, reordering %d: the restriction did not survive a round trip", c.refs, c.reorder)
		}
		if int(v.MaxNumReorderFrames) != c.reorder || v.MaxDecFrameBuffering != c.dpb {
			t.Errorf("%d references, reordering %d: max_num_reorder_frames %d, max_dec_frame_buffering %d, want %d and %d",
				c.refs, c.reorder, v.MaxNumReorderFrames, v.MaxDecFrameBuffering, c.reorder, c.dpb)
		}
		if !v.MotionVectorsOverPicBoundary || v.MaxBytesPerPicDenom != 2 || v.MaxBitsPerMBDenom != 1 ||
			v.Log2MaxMvLengthHorizontal != 15 || v.Log2MaxMvLengthVertical != 15 {
			t.Errorf("%d references, reordering %d: the other limits differ from the values inferred without a restriction: %+v",
				c.refs, c.reorder, v)
		}
		if !v.TimingInfoPresent || v.TimeScale != 60 || !v.FixedFrameRate {
			t.Errorf("%d references, reordering %d: the timing information was lost", c.refs, c.reorder)
		}
		if got.MaxNumReorder() != c.reorder {
			t.Errorf("%d references, reordering %d: MaxNumReorder() = %d", c.refs, c.reorder, got.MaxNumReorder())
		}
	}
}
