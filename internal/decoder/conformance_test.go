package decoder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/testutil"
)

func decodeAll(t *testing.T, stream []byte) []*frame.Picture {
	t.Helper()
	d := New()
	pics, err := d.Decode(stream)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	rest, err := d.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return append(pics, rest...)
}

func runClip(t *testing.T, clip testutil.Clip, frames int) {
	stream := testutil.LoadStream(t, clip)
	want := testutil.LoadReferenceYUV(t, clip)
	pics := decodeAll(t, stream)
	if len(pics) < frames {
		t.Fatalf("decoded %d pictures, want at least %d", len(pics), frames)
	}
	for i := 0; i < frames; i++ {
		p := pics[i]
		if p.Width != clip.Width || p.Height != clip.Height {
			t.Fatalf("frame %d: got %dx%d, want %dx%d", i, p.Width, p.Height, clip.Width, clip.Height)
		}
		got := make([]byte, clip.FrameSize())
		p.CopyOut(got)
		ref := clip.Frame(want, i)
		n := clip.Width * clip.Height
		testutil.ComparePlanes(t, labelf(clip.Name, i, "Y"), got[:n], ref[:n], clip.Width, clip.Height)
		cw, ch := clip.Width/2, clip.Height/2
		testutil.ComparePlanes(t, labelf(clip.Name, i, "Cb"), got[n:n+cw*ch], ref[n:n+cw*ch], cw, ch)
		testutil.ComparePlanes(t, labelf(clip.Name, i, "Cr"), got[n+cw*ch:], ref[n+cw*ch:], cw, ch)
		if t.Failed() {
			t.Fatalf("%s: stopping after the first mismatching frame", clip.Name)
		}
	}
}

func labelf(name string, frame int, plane string) string {
	return name + " frame " + itoa(frame) + " " + plane
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}

func TestIntraNoDeblockFirstFrame(t *testing.T) {
	runClip(t, testutil.Corpus[1], 1)
}

func TestIntraNoDeblockAllFrames(t *testing.T) {
	clip := testutil.Corpus[1]
	runClip(t, clip, clip.Frames)
}

func TestIntraDeblockedAllFrames(t *testing.T) {
	clip := testutil.Corpus[0]
	runClip(t, clip, clip.Frames)
}

func TestInterFirstFrames(t *testing.T) {
	for _, i := range []int{2, 3, 4} {
		clip := testutil.Corpus[i]
		t.Run(clip.Name, func(t *testing.T) { runClip(t, clip, clip.Frames) })
	}
}

func TestAllCorpusClips(t *testing.T) {
	for _, clip := range testutil.Corpus {
		t.Run(clip.Name, func(t *testing.T) { runClip(t, clip, clip.Frames) })
	}
}

func TestDecodeInChunks(t *testing.T) {
	clip := testutil.Corpus[3]
	stream := testutil.LoadStream(t, clip)
	want := testutil.LoadReferenceYUV(t, clip)
	for _, size := range []int{1, 7, 64, 1000} {
		d := New()
		var pics []*frame.Picture
		for off := 0; off < len(stream); off += size {
			end := off + size
			if end > len(stream) {
				end = len(stream)
			}
			got, err := d.Decode(stream[off:end])
			if err != nil {
				t.Fatalf("chunk size %d: %v", size, err)
			}
			pics = append(pics, got...)
		}
		rest, err := d.Flush()
		if err != nil {
			t.Fatalf("chunk size %d flush: %v", size, err)
		}
		pics = append(pics, rest...)
		if len(pics) != clip.Frames {
			t.Fatalf("chunk size %d: decoded %d frames, want %d", size, len(pics), clip.Frames)
		}
		buf := make([]byte, clip.FrameSize())
		for i, p := range pics {
			p.CopyOut(buf)
			ref := clip.Frame(want, i)
			for j := range buf {
				if buf[j] != ref[j] {
					t.Fatalf("chunk size %d frame %d differs at sample %d", size, i, j)
				}
			}
		}
	}
}

func FuzzDecoderNeverPanics(f *testing.F) {
	for _, clip := range testutil.AllClips() {
		data, err := os.ReadFile(filepath.Join(testutil.CorpusDir(), clip.Name+".264"))
		if err == nil {
			f.Add(data)
		}
	}
	for _, u := range testutil.EBSPUnits() {
		f.Add(append([]byte{0, 0, 1}, u...))
	}
	f.Add([]byte{0, 0, 1, 0x67, 0x42})
	f.Add([]byte{})
	f.Add([]byte{0, 0, 1})
	f.Add([]byte{0, 0, 1, 0x65, 0x88, 0x84, 0x00, 0x00, 0x01, 0x68})
	f.Fuzz(func(t *testing.T, data []byte) {
		d := New()
		d.SetLimits(Limits{MaxFrameMBs: 1620, MaxNALBytes: 1 << 20})
		pics, _ := d.Decode(data)
		rest, _ := d.Flush()
		for _, p := range append(pics, rest...) {
			if p.Width <= 0 || p.Height <= 0 {
				t.Fatalf("decoded a picture of size %dx%d", p.Width, p.Height)
			}
			buf := make([]byte, p.Size())
			p.CopyOut(buf)
		}
	})
}

func TestMainProfileIntraCABAC(t *testing.T) {
	runClip(t, testutil.MainCorpus[0], testutil.MainCorpus[0].Frames)
}

func TestMainProfileIntraCABACNoDeblock(t *testing.T) {
	clip := testutil.MainCorpus[1]
	runClip(t, clip, clip.Frames)
}

func TestMainProfilePCABAC(t *testing.T) {
	clip := testutil.MainCorpus[2]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileWeightedP(t *testing.T) {
	clip := testutil.MainCorpus[4]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileWeightedPFade(t *testing.T) {
	clip := testutil.MainCorpus[5]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileBCAVLC(t *testing.T) {
	clip := testutil.MainCorpus[6]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileBCABAC(t *testing.T) {
	clip := testutil.MainCorpus[3]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileBTemporalDirect(t *testing.T) {
	clip := testutil.MainCorpus[7]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileBImplicitWeights(t *testing.T) {
	clip := testutil.MainCorpus[8]
	runClip(t, clip, clip.Frames)
}

func TestMainProfileBPyramid(t *testing.T) {
	clip := testutil.MainCorpus[9]
	runClip(t, clip, clip.Frames)
}

func TestMainProfilePyramidAtCommonIntermediateFormat(t *testing.T) {
	clip := testutil.MainCorpus[10]
	runClip(t, clip, clip.Frames)
}

func TestHighProfileCorpus(t *testing.T) {
	for _, clip := range testutil.HighCorpus {
		t.Run(clip.Name, func(t *testing.T) { runClip(t, clip, clip.Frames) })
	}
}
