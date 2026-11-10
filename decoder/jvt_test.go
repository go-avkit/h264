package decoder

import (
	"crypto/md5"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// pendingUpstream lists the conformance bitstreams that fail with go.264
// v1.12.0 and pass with the fix proposed in oops1/go.264#2 ("decoder: handle
// MMCO 5 and gaps in frame_num"). The test fails on an unexpected pass, so this
// list cannot go stale.
var pendingUpstream = map[string]bool{
	"MR2_TANDBERG_E": true,
	"MR3_TANDBERG_B": true,
	"MR4_TANDBERG_C": true,
	"MR5_TANDBERG_C": true,
}

func frameSums(frames []*Frame) []string {
	var out []string
	for _, f := range frames {
		sum := md5.Sum(f.AppendI420(nil))
		out = append(out, hex.EncodeToString(sum[:]))
	}
	return out
}

// bitstreamIn returns the largest file of dir with a bitstream extension, which
// is the stream: the archives also hold a readme and the reference YUV, and a
// few name their stream differently from the archive (cabac_mot_frm0_full.zip
// holds camp_mot_frm0_full.26l). It returns "" when there is none.
func bitstreamIn(dir string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, size := "", int64(-1)
	for _, e := range entries {
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".264", ".jsv", ".jvt", ".avc", ".26l", ".h264", ".bit", ".bits":
			if info, err := e.Info(); err == nil && info.Size() > size {
				best, size = filepath.Join(dir, e.Name()), info.Size()
			}
		}
	}
	return best
}

func manifest(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		l = strings.TrimRight(l, "\r") // a checkout with autocrlf ends lines with \r\n
		if l != "" && l[0] != '#' {
			out = append(out, l)
		}
	}
	return out
}

// TestJVTConformance decodes ITU-T H.264.1 conformance bitstreams and compares
// every picture's MD5 with what ffmpeg -f framemd5 produced for it
// (testdata/jvt/NAME.md5). The bitstreams are not in the repository: set
// H264_JVT_DIR to a directory holding NAME/NAME.264 (or .jsv, .jvt, ...) as
// `go run ./tools/jvtfetch` unpacks them, from
// https://www.itu.int/wftp3/av-arch/jvt-site/draft_conformance/AVCv1/.
// With H264_JVT_REQUIRE=1 a stream that is missing fails instead of skipping,
// which is how CI runs it.
func TestJVTConformance(t *testing.T) {
	dir := os.Getenv("H264_JVT_DIR")
	if dir == "" {
		t.Skip("H264_JVT_DIR is not set")
	}
	manifests, err := filepath.Glob(filepath.Join("..", "testdata", "jvt", "*.md5"))
	if err != nil || len(manifests) == 0 {
		t.Fatalf("no manifests: %v", err)
	}
	for _, m := range manifests {
		name := strings.TrimSuffix(filepath.Base(m), ".md5")
		t.Run(name, func(t *testing.T) {
			stream := bitstreamIn(filepath.Join(dir, name))
			if stream == "" {
				if os.Getenv("H264_JVT_REQUIRE") == "1" {
					t.Fatalf("%s is not in %s", name, dir)
				}
				t.Skipf("%s is not in %s", name, dir)
			}
			data, err := os.ReadFile(stream)
			if err != nil {
				t.Fatal(err)
			}
			d := New(Limits{MaxFrameMBs: 1 << 17})
			frames, err := d.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			rest, err := d.Flush()
			if err != nil {
				t.Fatal(err)
			}
			got := frameSums(append(frames, rest...))
			want := manifest(t, m)
			match := slices.Equal(got, want)
			switch {
			case pendingUpstream[name] && match:
				t.Fatalf("%s now matches: remove it from pendingUpstream", name)
			case pendingUpstream[name]:
				t.Skipf("%s: known failure, fixed in the proposal oops1/go.264#2", name)
			case !match:
				t.Fatalf("%d pictures, ffmpeg has %d, or a picture differs", len(got), len(want))
			}
		})
	}
}
