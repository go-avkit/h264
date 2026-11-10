package testutil

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type Clip struct {
	Name   string
	Width  int
	Height int
	Frames int
}

var Corpus = []Clip{
	{Name: "base_intra_qp26", Width: 176, Height: 144, Frames: 10},
	{Name: "base_intra_nodb", Width: 176, Height: 144, Frames: 10},
	{Name: "base_ip_qp10", Width: 176, Height: 144, Frames: 10},
	{Name: "base_ip_qp26", Width: 176, Height: 144, Frames: 10},
	{Name: "base_ip_qp40", Width: 176, Height: 144, Frames: 10},
	{Name: "base_ip_ref3", Width: 176, Height: 144, Frames: 10},
	{Name: "base_ip_slices", Width: 176, Height: 144, Frames: 10},
}

var MainCorpus = []Clip{
	{Name: "main_intra_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "main_intra_cabac_nodb", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ip_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ipb_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ip_weightp", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ip_fade", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ipb_cavlc", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ipb_temporal", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ipb_weightb", Width: 176, Height: 144, Frames: 10},
	{Name: "main_ipb_pyramid", Width: 176, Height: 144, Frames: 10},
	{Name: "main_cif_pyramid", Width: 352, Height: 288, Frames: 8},
}

var HighCorpus = []Clip{
	{Name: "high_intra_8x8_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "high_intra_8x8_cavlc", Width: 176, Height: 144, Frames: 10},
	{Name: "high_ip_8x8_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "high_ip_8x8_cavlc", Width: 176, Height: 144, Frames: 10},
	{Name: "high_ipb_8x8_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "high_ipb_8x8_cavlc", Width: 176, Height: 144, Frames: 10},
	{Name: "high_cqm_jvt_cabac", Width: 176, Height: 144, Frames: 10},
	{Name: "high_cqm_jvt_cavlc", Width: 176, Height: 144, Frames: 10},
	{Name: "high_cqm_jvt_4x4", Width: 176, Height: 144, Frames: 10},
	{Name: "high_cqm_custom", Width: 176, Height: 144, Frames: 10},
	{Name: "high_cqm_custom_cavlc", Width: 176, Height: 144, Frames: 10},
}

func (c Clip) FrameSize() int { return c.Width * c.Height * 3 / 2 }

func CorpusDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("testdata", "conformance")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "conformance")
}

func LoadStream(t *testing.T, c Clip) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(CorpusDir(), c.Name+".264"))
	if err != nil {
		t.Fatalf("reading reference stream: %v", err)
	}
	return data
}

func LoadReferenceYUV(t *testing.T, c Clip) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join(CorpusDir(), c.Name+".yuv.gz"))
	if err != nil {
		t.Fatalf("opening reference frames: %v", err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("decompressing reference frames: %v", err)
	}
	defer zr.Close()
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading reference frames: %v", err)
	}
	want := c.FrameSize() * c.Frames
	if len(data) != want {
		t.Fatalf("reference frames for %s: got %d bytes, want %d", c.Name, len(data), want)
	}
	return data
}

func (c Clip) Frame(all []byte, i int) []byte {
	n := c.FrameSize()
	return all[i*n : (i+1)*n]
}

func ComparePlanes(t *testing.T, label string, got, want []byte, width, height int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d samples, want %d", label, len(got), len(want))
	}
	diff := 0
	firstX, firstY := -1, -1
	var maxDelta int
	for i := range got {
		d := int(got[i]) - int(want[i])
		if d == 0 {
			continue
		}
		if d < 0 {
			d = -d
		}
		if d > maxDelta {
			maxDelta = d
		}
		if diff == 0 {
			firstX = i % width
			firstY = i / width
		}
		diff++
	}
	if diff != 0 {
		t.Errorf("%s: %d of %d samples differ (%.4f%%), first at (%d,%d) got %d want %d, max delta %d",
			label, diff, width*height, 100*float64(diff)/float64(width*height),
			firstX, firstY, got[firstY*width+firstX], want[firstY*width+firstX], maxDelta)
	}
}
