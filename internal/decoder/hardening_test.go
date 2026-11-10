package decoder

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/level"
	"github.com/go-avkit/h264/internal/nal"
	"github.com/go-avkit/h264/internal/syntax"
	"github.com/go-avkit/h264/internal/testutil"
)

func hostileStream(t *testing.T, sps *syntax.SPS, cabac bool) []byte {
	t.Helper()
	spsBytes := mustWriteSPSBytes(t, sps)
	pps := minimalPPS(sps.ID, cabac)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))
	hdr := &syntax.SliceHeader{SliceType: syntax.SliceI, IDR: true, NalRefIDC: 1}
	sliceBytes := mustSliceBytes(t, hdr, sps, pps)

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 3, sliceBytes)...)
	return data
}

func TestSyntaxMaximumPictureIsRefusedAndAllocatesNothing(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 10
	sps.PicWidthInMbsMinus1 = 1023
	sps.PicHeightInMapUnitsMinus1 = 1023
	data := hostileStream(t, sps, false)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	d := New()
	_, err := d.Decode(data)
	runtime.ReadMemStats(&after)

	if !errors.Is(err, ErrOverLimit) {
		t.Fatalf("Decode() = %v, want ErrOverLimit", err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 4<<20 {
		t.Fatalf("a %d byte stream allocated %d bytes before being refused", len(data), grew)
	}
}

func TestPictureBeyondTheDeclaredLevelIsRefused(t *testing.T) {
	for _, c := range []struct {
		name      string
		levelIDC  uint8
		widthMBs  uint32
		heightMBs uint32
		refs      uint32
	}{
		{"frame size above MaxFS", 10, 40, 40, 1},
		{"width above the aspect bound", 30, 200, 8, 1},
		{"height above the aspect bound", 30, 8, 200, 1},
		{"more reference frames than the buffer holds", 10, 20, 20, 16},
	} {
		t.Run(c.name, func(t *testing.T) {
			sps := minimalSPS()
			sps.LevelIDC = c.levelIDC
			sps.PicWidthInMbsMinus1 = c.widthMBs - 1
			sps.PicHeightInMapUnitsMinus1 = c.heightMBs - 1
			sps.MaxNumRefFrames = c.refs
			d := New()
			d.SetLimits(Limits{EnforceLevel: true})
			_, err := d.Decode(hostileStream(t, sps, false))
			if !errors.Is(err, ErrOverLimit) {
				t.Fatalf("Decode() = %v, want ErrOverLimit", err)
			}
		})
	}
}

func TestUnknownLevelIsRefused(t *testing.T) {
	for _, idc := range []uint8{0, 1, 14, 33, 99, 255} {
		sps := minimalSPS()
		sps.LevelIDC = idc
		d := New()
		d.SetLimits(Limits{EnforceLevel: true})
		if _, err := d.Decode(hostileStream(t, sps, false)); !errors.Is(err, ErrOverLimit) {
			t.Fatalf("level_idc %d: Decode() = %v, want ErrOverLimit", idc, err)
		}
	}
}

func TestLevel1bIsAcceptedAtItsOwnFrameSize(t *testing.T) {
	sps := minimalSPS()
	sps.ProfileIDC = syntax.ProfileMain
	sps.LevelIDC = 11
	sps.ConstraintSet = 0x10
	sps.PicWidthInMbsMinus1 = 10
	sps.PicHeightInMapUnitsMinus1 = 8
	if err := level.CheckSPS(rootSPS(sps)); err != nil {
		t.Fatalf("level 1b at 99 macroblocks: %v", err)
	}
	sps.PicWidthInMbsMinus1 = 21
	sps.PicHeightInMapUnitsMinus1 = 17
	if err := level.CheckSPS(rootSPS(sps)); !errors.Is(err, level.ErrExceedsLevel) {
		t.Fatalf("level 1b at 396 macroblocks: %v, want ErrExceedsLevel", err)
	}
}

func TestCallerFrameSizeCeiling(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 30
	sps.PicWidthInMbsMinus1 = 43
	sps.PicHeightInMapUnitsMinus1 = 35
	data := hostileStream(t, sps, false)

	d := New()
	if _, err := d.Decode(data); errors.Is(err, ErrOverLimit) {
		t.Fatalf("Decode() = %v, want the level to allow this picture", err)
	}

	d = New()
	d.SetLimits(Limits{MaxFrameMBs: 99})
	if got := d.Limits().MaxFrameMBs; got != 99 {
		t.Fatalf("Limits().MaxFrameMBs = %d, want 99", got)
	}
	if _, err := d.Decode(data); !errors.Is(err, ErrOverLimit) {
		t.Fatalf("Decode() = %v, want ErrOverLimit from the caller ceiling", err)
	}
}

func TestCallerNALByteCeiling(t *testing.T) {
	data := append([]byte{0, 0, 1, 0x65}, make([]byte, 1<<16)...)
	d := New()
	d.SetLimits(Limits{MaxNALBytes: 4096})
	if _, err := d.Decode(data); !errors.Is(err, ErrOverLimit) {
		t.Fatalf("Decode() = %v, want ErrOverLimit from the caller ceiling", err)
	}

	d = New()
	if _, err := d.Decode(data); errors.Is(err, ErrOverLimit) {
		t.Fatal("Decode() refused a large unit with no caller ceiling set")
	}
}

func TestReorderQueueIsBoundedByTheLargestFrameBuffer(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 10
	sps.VUIPresent = true
	sps.VUI.BitstreamRestriction = true
	sps.VUI.MaxNumReorderFrames = 1 << 30
	if got := sps.MaxNumReorder(); got != 16 {
		t.Fatalf("MaxNumReorder() = %d, want it clamped to the 16 frame buffer maximum", got)
	}
}

func TestAdaptiveMarkingCannotGrowTheReferenceListForever(t *testing.T) {
	b := &dpb{maxNumRefs: 2, maxFrameNum: 16}
	for i := 0; i < 64; i++ {
		hdr := newTestHeader(uint32(i%16), false, 1)
		hdr.AdaptiveRefPicMarking = true
		b.store(frame.NewPicture(1, 1), hdr)
		if len(b.refs) > 2 {
			t.Fatalf("after %d adaptively marked pictures the buffer holds %d references, want at most 2",
				i+1, len(b.refs))
		}
	}
}

func TestLongTermMarkingCannotGrowTheReferenceListForever(t *testing.T) {
	b := &dpb{maxNumRefs: 2, maxFrameNum: 16}
	for i := 0; i < 64; i++ {
		hdr := newTestHeader(uint32(i%16), false, 1)
		hdr.AdaptiveRefPicMarking = true
		hdr.MMCOs = []mmco{{Op: 6, LongTermFrameIdx: uint32(i)}}
		b.store(frame.NewPicture(1, 1), hdr)
		if len(b.refs) > 2 {
			t.Fatalf("after %d long term pictures the buffer holds %d references, want at most 2",
				i+1, len(b.refs))
		}
	}
}

// decodeStorm times the decoder over n bytes of start codes, best of three.
func decodeStorm(n int) time.Duration {
	stream := make([]byte, 0, n)
	for len(stream) < n {
		stream = append(stream, 0x00, 0x00, 0x01, 0x0C)
	}
	var best time.Duration
	for range 3 {
		start := time.Now()
		d := New()
		_, _ = d.Decode(stream)
		_, _ = d.Flush()
		if took := time.Since(start); best == 0 || took < best {
			best = took
		}
	}
	return best
}

// Linear is judged by the ratio of two sizes, not by a wall-clock bound, so a
// slow or emulated machine does not fail it; a quadratic scanner costs about
// sixteen times for four times the input.
func TestStartCodeStormStaysLinear(t *testing.T) {
	const n = 1 << 18
	small, large := decodeStorm(n), decodeStorm(4*n)
	if large > 8*small+10*time.Millisecond {
		t.Fatalf("%d bytes of start codes took %v and %d bytes %v; a quadratic scanner is back",
			n, small, 4*n, large)
	}
}

func TestCABACStopsAtTheEndOfTheSliceData(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 30
	sps.PicWidthInMbsMinus1 = 44
	sps.PicHeightInMapUnitsMinus1 = 35
	spsBytes := mustWriteSPSBytes(t, sps)
	pps := minimalPPS(sps.ID, true)
	ppsBytes := mustWritePPSBytes(t, pps, lookupOne(sps))
	hdr := &syntax.SliceHeader{SliceType: syntax.SliceI, IDR: true, NalRefIDC: 1}

	w := bits.NewWriter()
	if err := syntax.WriteSliceHeader(w, hdr, sps, pps); err != nil {
		t.Fatal(err)
	}
	w.AlignOne()
	w.WriteBits(0, 32)
	if err := w.Err(); err != nil {
		t.Fatal(err)
	}

	var data []byte
	data = append(data, annexBUnit(nal.TypeSPS, 3, spsBytes)...)
	data = append(data, annexBUnit(nal.TypePPS, 3, ppsBytes)...)
	data = append(data, annexBUnit(nal.TypeSliceIDR, 3, w.Bytes())...)
	data = append(data, 0x00, 0x00, 0x01, 0x09, 0x10, 0x00, 0x00, 0x01)

	start := time.Now()
	d := New()
	_, err := d.Decode(data)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("Decode() = %v, want ErrCorrupt: an empty CABAC slice must not decode a whole picture from padding", err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("refusing an empty CABAC slice took %v", took)
	}
}

func TestDimensionsMayChangeMidStream(t *testing.T) {
	small := testutil.LoadStream(t, testutil.Corpus[0])
	large := testutil.LoadStream(t, testutil.MainCorpus[10])

	d := New()
	pics, err := d.Decode(append(append([]byte{}, small...), large...))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rest, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	all := append(pics, rest...)
	if len(all) != testutil.Corpus[0].Frames+testutil.MainCorpus[10].Frames {
		t.Fatalf("decoded %d pictures, want %d", len(all),
			testutil.Corpus[0].Frames+testutil.MainCorpus[10].Frames)
	}
	for i, p := range all {
		want := testutil.Corpus[0]
		if i >= want.Frames {
			want = testutil.MainCorpus[10]
		}
		if p.CropWidth != want.Width || p.CropHeight != want.Height {
			t.Fatalf("picture %d is %dx%d, want %dx%d",
				i, p.CropWidth, p.CropHeight, want.Width, want.Height)
		}
		p.CopyOut(make([]byte, p.Size()))
	}
}

func TestSliceContinuingIntoAResizedPictureDoesNotEscapeTheGrid(t *testing.T) {
	stream := testutil.LoadStream(t, testutil.Corpus[6])
	units := nal.SplitAnnexB(stream)

	wide := minimalSPS()
	wide.LevelIDC = 30
	wide.PicWidthInMbsMinus1 = 43
	wide.PicHeightInMapUnitsMinus1 = 35
	wideBytes := mustWriteSPSBytes(t, wide)

	var data []byte
	injected := false
	for _, u := range units {
		unit, err := nal.Parse(u)
		if err != nil {
			continue
		}
		if !injected && unit.Type == nal.TypeSliceIDR {
			data = append(data, annexBUnit(nal.TypeSPS, 3, wideBytes)...)
			injected = true
		}
		data = append(data, nal.AppendAnnexB(nil, unit, true)...)
	}

	d := New()
	pics, _ := d.Decode(data)
	rest, _ := d.Flush()
	for _, p := range append(pics, rest...) {
		if p.Width <= 0 || p.Height <= 0 {
			t.Fatalf("decoded a picture of size %dx%d", p.Width, p.Height)
		}
		p.CopyOut(make([]byte, p.Size()))
	}
}

func TestGarbageWithoutStartCodesDoesNotAccumulate(t *testing.T) {
	d := New()
	for i := 0; i < 256; i++ {
		if _, err := d.Decode(make([]byte, 4096)); err != nil {
			t.Fatal(err)
		}
	}
	if n := d.scanner.Buffered(); n > 64 {
		t.Fatalf("1 MiB of data with no start code left %d bytes buffered", n)
	}
}

func TestAPictureAboveItsDeclaredLevelStillDecodesByDefault(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 10
	sps.PicWidthInMbsMinus1 = 39
	sps.PicHeightInMapUnitsMinus1 = 39
	if err := level.CheckSPS(rootSPS(sps)); err == nil {
		t.Fatal("this picture is meant to exceed level 1.0, so the test proves nothing")
	}
	d := New()
	if _, err := d.Decode(hostileStream(t, sps, false)); errors.Is(err, ErrOverLimit) {
		t.Fatalf("a stream that only overstates its level was refused without the caller asking: %v", err)
	}
}

func TestThePictureCeilingHoldsWithoutTheCallerAskingForAnything(t *testing.T) {
	sps := minimalSPS()
	sps.LevelIDC = 62
	sps.PicWidthInMbsMinus1 = 1023
	sps.PicHeightInMapUnitsMinus1 = 1023
	d := New()
	if _, err := d.Decode(hostileStream(t, sps, false)); !errors.Is(err, ErrOverLimit) {
		t.Fatalf("a picture beyond every level in Table A-1 was accepted: %v", err)
	}
}
