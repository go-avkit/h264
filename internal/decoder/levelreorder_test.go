package decoder

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/nal"
)

const (
	pyramidWidth  = 352
	pyramidHeight = 288
	pyramidFrames = 24
)

func pyramidStream(t *testing.T) []byte {
	t.Helper()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	out := filepath.Join(t.TempDir(), "pyramid.264")
	cmd := exec.Command(bin, "-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=352x288:rate=25",
		"-frames:v", "24", "-pix_fmt", "yuv420p", "-c:v", "libx264", "-profile:v", "main",
		"-x264-params", "keyint=24:scenecut=0:bframes=3:b-adapt=0:b-pyramid=normal:ref=4:qp=26:weightb=1",
		"-f", "h264", out)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("ffmpeg could not encode with libx264: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func withLevel(t *testing.T, stream []byte, levelIDC uint8) ([]byte, *h264.SPS) {
	t.Helper()
	var out []byte
	var last *h264.SPS
	for _, ebsp := range nal.SplitAnnexB(stream) {
		u, err := nal.Parse(ebsp)
		if err != nil {
			t.Fatal(err)
		}
		if u.Header.Type == nal.TypeSPS {
			// level_idc is the third byte of the set; the rest is left as it is.
			u.RBSP[2] = levelIDC
			sps, err := h264.ParseSPS(h264.Unit{Type: h264.UnitSPS, Payload: nal.Escape(nil, u.RBSP)})
			if err != nil {
				t.Fatal(err)
			}
			last = &sps
		}
		out = nal.AppendAnnexB(out, u, true)
	}
	return out, last
}

func ffmpegFrames(t *testing.T, stream []byte, size int) [][]byte {
	t.Helper()
	dir := t.TempDir()
	in := filepath.Join(dir, "in.264")
	outPath := filepath.Join(dir, "out.yuv")
	if err := os.WriteFile(in, stream, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-i", in, "-pix_fmt", "yuv420p", "-f", "rawvideo", outPath)
	if combined, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg could not decode the stream: %v\n%s", err, combined)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var frames [][]byte
	for i := 0; i+size <= len(data); i += size {
		frames = append(frames, data[i:i+size])
	}
	return frames
}

func TestAStatedReorderingOutlivesALevelThatCannotHoldIt(t *testing.T) {
	original := pyramidStream(t)
	stream, sps := withLevel(t, original, 10)
	if sps == nil || !sps.VUIPresent || !sps.VUI.BitstreamRestriction || sps.VUI.MaxNumReorderFrames < 2 {
		t.Fatal("x264 no longer states a reordering of two pictures for a pyramid of three B pictures")
	}
	if sps.MaxDpbFrames() >= int(sps.VUI.MaxNumReorderFrames) {
		t.Fatalf("level 1.0 holds %d CIF pictures, enough for the stated %d; the stream does not undersize its buffer",
			sps.MaxDpbFrames(), sps.VUI.MaxNumReorderFrames)
	}
	size := pyramidWidth * pyramidHeight * 3 / 2
	want := ffmpegFrames(t, original, size)
	if len(want) != pyramidFrames {
		t.Fatalf("ffmpeg decoded %d pictures, want %d", len(want), pyramidFrames)
	}
	pics := decodeAll(t, stream)
	if len(pics) != len(want) {
		t.Fatalf("decoded %d pictures, ffmpeg %d", len(pics), len(want))
	}
	for i, p := range pics {
		got := make([]byte, size)
		p.CopyOut(got)
		if !bytes.Equal(got, want[i]) {
			for j := range want {
				if bytes.Equal(got, want[j]) {
					t.Fatalf("picture %d came out in place of picture %d: the stated reordering of %d was cut to the level's %d",
						j, i, sps.VUI.MaxNumReorderFrames, sps.MaxDpbFrames())
				}
			}
			t.Fatalf("picture %d differs from ffmpeg's and matches none of its pictures", i)
		}
	}
}
