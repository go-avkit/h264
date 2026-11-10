package syntax

import (
	"errors"
	"math"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/nal"
)

func TestSPSRejectsOffsetsOutsideTheSignedRange(t *testing.T) {
	w := bits.NewWriter()
	w.WriteBits(77, 8)
	w.WriteBits(0, 8)
	w.WriteBits(30, 8)
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteUE(1)
	w.WriteFlag(false)
	w.WriteBits(0, 32)
	w.WriteBit(1)
	w.WriteBits(1, 32)
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSPS(w.Bytes()); !errors.Is(err, bits.ErrRange) {
		t.Fatalf("offset_for_non_ref_pic of -2^31 accepted: %v", err)
	}
}

func TestFrameCroppingOffsetsCannotWrap(t *testing.T) {
	s := baseSPS()
	s.FrameCropping = true
	for _, c := range []struct {
		name                     string
		left, right, top, bottom uint32
		accepted                 bool
	}{
		{"no cropping at all", 0, 0, 0, 0, true},
		{"a legal crop", 1, 1, 1, 1, true},
		{"the widest legal crop", 39, 40, 31, 32, true},
		{"one crop unit too many horizontally", 40, 40, 0, 0, false},
		{"one crop unit too many vertically", 0, 0, 32, 32, false},
		{"offsets that sum past 2^32", 1 << 31, 1 << 31, 0, 0, false},
		{"offsets that sum to one past 2^32", 1<<31 + 1, 1 << 31, 0, 0, false},
		{"vertical offsets that sum past 2^32", 0, 0, 1 << 31, 1 << 31, false},
		{"a single enormous offset", 1 << 20, 0, 0, 0, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			s.FrameCropLeftOffset = c.left
			s.FrameCropRightOffset = c.right
			s.FrameCropTopOffset = c.top
			s.FrameCropBottomOffset = c.bottom
			b, err := WriteSPS(s)
			if err != nil {
				t.Fatalf("WriteSPS: %v", err)
			}
			parsed, err := ParseSPS(b)
			if c.accepted {
				if err != nil {
					t.Fatalf("ParseSPS: %v", err)
				}
				if parsed.CroppedWidth() <= 0 || parsed.CroppedWidth() > parsed.Width() {
					t.Fatalf("CroppedWidth() = %d, picture is %d wide", parsed.CroppedWidth(), parsed.Width())
				}
				if parsed.CroppedHeight() <= 0 || parsed.CroppedHeight() > parsed.Height() {
					t.Fatalf("CroppedHeight() = %d, picture is %d high", parsed.CroppedHeight(), parsed.Height())
				}
				return
			}
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("ParseSPS = %v, want ErrInvalidValue", err)
			}
		})
	}
}

func TestMaxNumReorderNeverExceedsTheLargestFrameBuffer(t *testing.T) {
	s := &SPS{
		LevelIDC:                  10,
		FrameMbsOnly:              true,
		PicWidthInMbsMinus1:       10,
		PicHeightInMapUnitsMinus1: 8,
		VUIPresent:                true,
	}
	s.VUI.BitstreamRestriction = true
	cases := []struct {
		stated uint32
		want   int
	}{
		{0, 0}, {1, 1}, {4, 4}, {5, 5}, {16, 16}, {17, 16}, {1 << 20, 16}, {math.MaxUint32, 16},
	}
	for _, c := range cases {
		s.VUI.MaxNumReorderFrames = c.stated
		if got := s.MaxNumReorder(); got != c.want {
			t.Fatalf("max_num_reorder_frames %d gave MaxNumReorder() = %d, want %d", c.stated, got, c.want)
		}
	}
}

func TestMaxNumReorderTrustsTheStreamWhenItsLevelUndersizesTheBuffer(t *testing.T) {
	s := &SPS{
		LevelIDC:                  40,
		FrameMbsOnly:              true,
		PicWidthInMbsMinus1:       239,
		PicHeightInMapUnitsMinus1: 134,
		VUIPresent:                true,
	}
	if s.MaxDpbFrames() != 1 {
		t.Fatalf("a 4K picture at level 4.0 gave MaxDpbFrames() = %d, want 1", s.MaxDpbFrames())
	}
	s.VUI.BitstreamRestriction = true
	s.VUI.MaxNumReorderFrames = 2
	if got := s.MaxNumReorder(); got != 2 {
		t.Fatalf("MaxNumReorder() = %d, want the stated 2", got)
	}
}

func TestUnknownLevelKeepsTheBufferTheTableAllows(t *testing.T) {
	s := &SPS{
		LevelIDC:                  200,
		FrameMbsOnly:              true,
		PicWidthInMbsMinus1:       10,
		PicHeightInMapUnitsMinus1: 8,
	}
	if got := s.MaxDpbFrames(); got != 16 {
		t.Fatalf("an unrecognised level gave MaxDpbFrames() = %d, want the capped 16", got)
	}
}

func TestLevel1bIsWrittenTwoWays(t *testing.T) {
	baseline := &SPS{ProfileIDC: ProfileBaseline, LevelIDC: 11, ConstraintSet: 0x10}
	if !baseline.IsLevel1b() {
		t.Fatal("level_idc 11 with constraint_set3_flag on a Baseline stream is level 1b")
	}
	high := &SPS{ProfileIDC: ProfileHigh, LevelIDC: 9}
	if !high.IsLevel1b() {
		t.Fatal("level_idc 9 is how a High profile stream writes level 1b")
	}
}

func TestHighProfileConstraintSet3IsNotLevel1b(t *testing.T) {
	s := &SPS{
		ProfileIDC:                ProfileHigh,
		LevelIDC:                  11,
		ConstraintSet:             0x10,
		FrameMbsOnly:              true,
		PicWidthInMbsMinus1:       10,
		PicHeightInMapUnitsMinus1: 8,
	}
	if s.IsLevel1b() {
		t.Fatal("a High profile stream resolved to level 1b")
	}
	if got := s.MaxDpbFrames(); got != 9 {
		t.Fatalf("High profile level 1.1 at 99 macroblocks: MaxDpbFrames() = %d, want 9", got)
	}
}

func weightedSliceBits(t *testing.T, mutate func(*bits.Writer)) []byte {
	t.Helper()
	w := bits.NewWriter()
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteUE(0)
	w.WriteBits(0, 4)
	w.WriteBits(0, 4)
	w.WriteFlag(false)
	w.WriteFlag(false)
	w.WriteUE(0)
	w.WriteUE(0)
	mutate(w)
	w.WriteRBSPTrailingBits()
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}
	return w.Bytes()
}

func TestPredictionWeightsOutsideTheSpecifiedRangeAreRejected(t *testing.T) {
	sps := baseSPS()
	pps := basePPS(sps.ID)
	pps.WeightedPred = true
	sets := newFakeParams().addSPS(sps).addPPS(pps)

	unit := nal.Header{Type: nal.TypeSliceNonIDR}

	for _, c := range []struct {
		name  string
		write func(*bits.Writer)
	}{
		{"luma weight below -128", func(w *bits.Writer) {
			w.WriteFlag(true)
			w.WriteSE(-129)
			w.WriteSE(0)
		}},
		{"luma weight above 127", func(w *bits.Writer) {
			w.WriteFlag(true)
			w.WriteSE(128)
			w.WriteSE(0)
		}},
		{"luma offset above 127", func(w *bits.Writer) {
			w.WriteFlag(true)
			w.WriteSE(1)
			w.WriteSE(1 << 20)
		}},
		{"chroma weight below -128", func(w *bits.Writer) {
			w.WriteFlag(false)
			w.WriteFlag(true)
			w.WriteSE(-1000)
			w.WriteSE(0)
			w.WriteSE(0)
			w.WriteSE(0)
		}},
		{"chroma offset above 127", func(w *bits.Writer) {
			w.WriteFlag(false)
			w.WriteFlag(true)
			w.WriteSE(0)
			w.WriteSE(0)
			w.WriteSE(0)
			w.WriteSE(9999)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := weightedSliceBits(t, c.write)
			_, _, _, err := ParseSliceHeader(bits.NewReader(data), unit, sets)
			if !errors.Is(err, ErrInvalidValue) {
				t.Fatalf("%s: %v, want ErrInvalidValue", c.name, err)
			}
		})
	}

	data := weightedSliceBits(t, func(w *bits.Writer) {
		w.WriteFlag(true)
		w.WriteSE(-128)
		w.WriteSE(127)
		w.WriteFlag(true)
		w.WriteSE(127)
		w.WriteSE(-128)
		w.WriteSE(-128)
		w.WriteSE(127)
	})
	if _, _, _, err := ParseSliceHeader(bits.NewReader(data), unit, sets); errors.Is(err, ErrInvalidValue) {
		t.Fatalf("weights at the ends of the specified range were rejected: %v", err)
	}
}
