package level

import (
	"errors"
	"testing"

	h264 "github.com/go-avkit/h264"
)

func TestTableIsMonotonic(t *testing.T) {
	columns := []struct {
		name string
		of   func(int) int
	}{
		{"MaxMBPS", func(i int) int { return table[i].MaxMBPS }},
		{"MaxFS", func(i int) int { return table[i].MaxFS }},
		{"MaxDpbMbs", func(i int) int { return table[i].MaxDpbMbs }},
		{"MaxBR", func(i int) int { return table[i].MaxBR }},
		{"MaxCPB", func(i int) int { return table[i].MaxCPB }},
	}
	for _, c := range columns {
		for i := 1; i < len(table); i++ {
			if c.of(i) < c.of(i-1) {
				t.Errorf("%s falls from %d at level %d to %d at level %d",
					c.name, c.of(i-1), table[i-1].IDC, c.of(i), table[i].IDC)
			}
		}
	}
	for i := 1; i < len(table); i++ {
		if table[i].IDC <= table[i-1].IDC {
			t.Errorf("level %d does not follow level %d", table[i].IDC, table[i-1].IDC)
		}
	}
	if table[0].IDC != 10 {
		t.Errorf("the table starts at level %d, want 10", table[0].IDC)
	}
	for _, l := range table {
		if l.MaxCPB < l.MaxBR {
			t.Errorf("level %d holds %d of buffer against a peak of %d, so it cannot hold one second",
				l.IDC, l.MaxCPB, l.MaxBR)
		}
		if l.MaxDpbMbs < l.MaxFS {
			t.Errorf("level %d buffers %d macroblocks but allows pictures of %d",
				l.IDC, l.MaxDpbMbs, l.MaxFS)
		}
		if l.MaxMBPS < l.MaxFS {
			t.Errorf("level %d allows %d macroblocks per second but pictures of %d",
				l.IDC, l.MaxMBPS, l.MaxFS)
		}
	}
}

func TestTableMatchesTheDecoderBufferSizes(t *testing.T) {
	for _, l := range table {
		sps := &h264.SPS{Level: l.IDC, MBWidth: 1, MBHeight: 1, FrameMBSOnly: true}
		want := l.MaxDpbMbs
		if want > 16 {
			want = 16
		}
		if got := sps.MaxDpbFrames(); got != want {
			t.Errorf("level %d: the decoder allows %d frames of buffer for one macroblock, the level table says %d",
				l.IDC, got, want)
		}
	}
}

func TestTableHandsOutACopy(t *testing.T) {
	rows := Table()
	if len(rows) != len(table) {
		t.Fatalf("Table returns %d rows, the table holds %d", len(rows), len(table))
	}
	rows[0].MaxFS = 0
	if again := Table(); again[0].MaxFS != table[0].MaxFS {
		t.Errorf("writing to a handed-out row changed the table: MaxFS is now %d, want %d",
			again[0].MaxFS, table[0].MaxFS)
	}
}

func TestLookupFindsEveryLevelAndNothingElse(t *testing.T) {
	for _, l := range table {
		got, ok := Lookup(l.IDC)
		if !ok {
			t.Errorf("level %d is in the table but Lookup does not find it", l.IDC)
			continue
		}
		if got != l {
			t.Errorf("Lookup(%d) = %+v, want %+v", l.IDC, got, l)
		}
	}
	for _, idc := range []uint8{0, 9, 33, 43, 63, 255} {
		if _, ok := Lookup(idc); ok {
			t.Errorf("Lookup(%d) found a level H.264 does not define", idc)
		}
	}
}

func TestMaxRatesFollowTheProfileFactor(t *testing.T) {
	l, ok := Lookup(31)
	if !ok {
		t.Fatal("level 31 is missing from the table")
	}
	main := l.MaxBitsPerSecond(77)
	high := l.MaxBitsPerSecond(100)
	if main != int64(l.MaxBR)*1200 {
		t.Errorf("level 31 allows %d bit/s in Main profile, want %d", main, int64(l.MaxBR)*1200)
	}
	if high != int64(l.MaxBR)*1500 {
		t.Errorf("level 31 allows %d bit/s in High profile, want %d", high, int64(l.MaxBR)*1500)
	}
	if l.MaxBufferBits(66) != int64(l.MaxCPB)*1200 {
		t.Errorf("level 31 holds %d bits of buffer in Baseline profile, want %d",
			l.MaxBufferBits(66), int64(l.MaxCPB)*1200)
	}
	if l.MaxBufferBits(100) != int64(l.MaxCPB)*1500 {
		t.Errorf("level 31 holds %d bits of buffer in High profile, want %d",
			l.MaxBufferBits(100), int64(l.MaxCPB)*1500)
	}
}

func TestMacroblockCountsRoundUp(t *testing.T) {
	s := Stream{Width: 1920, Height: 1080}
	if got := s.MacroblockWidth(); got != 120 {
		t.Errorf("1920 pixels span %d macroblocks, want 120", got)
	}
	if got := s.MacroblockHeight(); got != 68 {
		t.Errorf("1080 pixels span %d macroblocks, want 68", got)
	}
}

func TestSelectTakesTheSmallestLevelThatFits(t *testing.T) {
	cases := []struct {
		name string
		s    Stream
		want uint8
	}{
		{"quarter CIF", Stream{Width: 176, Height: 144, FPSNum: 15, FPSDen: 1}, 10},
		{"quarter CIF faster", Stream{Width: 176, Height: 144, FPSNum: 25, FPSDen: 1}, 11},
		{"CIF", Stream{Width: 352, Height: 288, FPSNum: 30, FPSDen: 1}, 13},
		{"standard definition", Stream{Width: 720, Height: 576, FPSNum: 25, FPSDen: 1}, 30},
		{"720p", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1}, 31},
		{"1080p", Stream{Width: 1920, Height: 1080, FPSNum: 30, FPSDen: 1}, 40},
		{"1080p at 60", Stream{Width: 1920, Height: 1080, FPSNum: 60, FPSDen: 1}, 42},
		{"2160p", Stream{Width: 3840, Height: 2160, FPSNum: 30, FPSDen: 1}, 51},
		{"720p with a bitrate", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1, PeakKbps: 20000}, 32},
		{"720p in High profile", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1,
			PeakKbps: 20000, ProfileIDC: 100}, 31},
		{"720p with a buffer", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1,
			PeakKbps: 20000, BufferKbits: 40000}, 41},
		{"720p with references", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1, RefFrames: 6}, 40},
	}
	for _, c := range cases {
		got, err := Select(c.s)
		if err != nil {
			t.Errorf("%s: Select = %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: Select announces level %d, want %d", c.name, got, c.want)
		}
	}
}

func TestSelectHoldsWhatItAnnounces(t *testing.T) {
	streams := []Stream{
		{Width: 176, Height: 144, FPSNum: 15, FPSDen: 1},
		{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1, RefFrames: 4, PeakKbps: 8000, BufferKbits: 16000},
		{Width: 1920, Height: 1080, FPSNum: 60, FPSDen: 1, RefFrames: 2, PeakKbps: 40000,
			BufferKbits: 40000, ProfileIDC: 100},
		{Width: 3840, Height: 2160, FPSNum: 30, FPSDen: 1, RefFrames: 3, PeakKbps: 60000},
	}
	for _, s := range streams {
		idc, err := Select(s)
		if err != nil {
			t.Errorf("%dx%d: Select = %v", s.Width, s.Height, err)
			continue
		}
		l, ok := Lookup(idc)
		if !ok {
			t.Errorf("%dx%d: Select announces level %d, which is not in the table", s.Width, s.Height, idc)
			continue
		}
		frameMBs := s.MacroblockWidth() * s.MacroblockHeight()
		if frameMBs > l.MaxFS {
			t.Errorf("%dx%d: level %d allows %d macroblocks, the picture holds %d",
				s.Width, s.Height, idc, l.MaxFS, frameMBs)
		}
		if rate := frameMBs * s.FPSNum / s.FPSDen; rate > l.MaxMBPS {
			t.Errorf("%dx%d: level %d allows %d macroblocks per second, the stream runs %d",
				s.Width, s.Height, idc, l.MaxMBPS, rate)
		}
		dpb := l.MaxDpbMbs / frameMBs
		if dpb > 16 {
			dpb = 16
		}
		if s.RefFrames > dpb {
			t.Errorf("%dx%d: level %d buffers %d frames, the stream keeps %d",
				s.Width, s.Height, idc, dpb, s.RefFrames)
		}
		if peak := int64(s.PeakKbps) * 1000; peak > l.MaxBitsPerSecond(s.ProfileIDC) {
			t.Errorf("%dx%d: level %d allows %d bit/s, the stream peaks at %d",
				s.Width, s.Height, idc, l.MaxBitsPerSecond(s.ProfileIDC), peak)
		}
		if buffer := int64(s.BufferKbits) * 1000; buffer > l.MaxBufferBits(s.ProfileIDC) {
			t.Errorf("%dx%d: level %d holds %d bits of buffer, the stream asks for %d",
				s.Width, s.Height, idc, l.MaxBufferBits(s.ProfileIDC), buffer)
		}
	}
}

func TestSelectCoversTheAspectRatio(t *testing.T) {
	s := Stream{Width: 4096, Height: 64, FPSNum: 25, FPSDen: 1}
	idc, err := Select(s)
	if err != nil {
		t.Fatalf("Select = %v", err)
	}
	l, ok := Lookup(idc)
	if !ok {
		t.Fatalf("Select announces level %d, which is not in the table", idc)
	}
	widthMBs := s.MacroblockWidth()
	if widthMBs*widthMBs > 8*l.MaxFS {
		t.Fatalf("level %d allows %d macroblocks of picture, too few for a picture %d macroblocks wide",
			idc, l.MaxFS, widthMBs)
	}
	t.Logf("4096x64 announces level %d", idc)
}

func TestSelectRefusesWhatNoLevelCarries(t *testing.T) {
	cases := []struct {
		name string
		s    Stream
	}{
		{"no picture", Stream{Width: 0, Height: 0, FPSNum: 25, FPSDen: 1}},
		{"no frame rate", Stream{Width: 176, Height: 144, FPSNum: 25, FPSDen: 0}},
		{"picture too large", Stream{Width: 16384, Height: 16384, FPSNum: 30, FPSDen: 1}},
		{"frame rate too high", Stream{Width: 3840, Height: 2160, FPSNum: 1000, FPSDen: 1}},
		{"bitrate too high", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1, PeakKbps: 1000000}},
		{"buffer too large", Stream{Width: 1280, Height: 720, FPSNum: 30, FPSDen: 1, BufferKbits: 2000000}},
		{"too many references", Stream{Width: 3840, Height: 2160, FPSNum: 30, FPSDen: 1, RefFrames: 17}},
	}
	for _, c := range cases {
		idc, err := Select(c.s)
		if err == nil {
			t.Errorf("%s: Select announces level %d, want an error", c.name, idc)
			continue
		}
		if !errors.Is(err, ErrNoLevel) {
			t.Errorf("%s: Select = %v, want an error wrapping ErrNoLevel", c.name, err)
		}
		if idc != 0 {
			t.Errorf("%s: Select returns level %d beside its error, want 0", c.name, idc)
		}
	}
}
