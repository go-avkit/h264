package decoder

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	idec "github.com/go-avkit/h264/internal/decoder"
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
// census counts, per stream, the direct-mode derivations the decoder made (one
// per B_Skip or B_Direct_16x16 macroblock, one per direct 8x8 sub-macroblock),
// by kind: the paths a stream exercises, so that a code path no conformance
// stream reaches is known, not assumed.
type census struct {
	spatial8x8, spatial4x4, temporal8x8, temporal4x4 int
}

func (c *census) add(o census) {
	c.spatial8x8 += o.spatial8x8
	c.spatial4x4 += o.spatial4x4
	c.temporal8x8 += o.temporal8x8
	c.temporal4x4 += o.temporal4x4
}

func (c census) String() string {
	return fmt.Sprintf("direct derivations: spatial %d (inference on) %d (inference off), temporal %d (on) %d (off)",
		c.spatial8x8, c.spatial4x4, c.temporal8x8, c.temporal4x4)
}

func TestJVTConformance(t *testing.T) {
	dir := os.Getenv("H264_JVT_DIR")
	if dir == "" {
		t.Skip("H264_JVT_DIR is not set")
	}
	var total census
	var cur census
	idec.DirectCensus = func(spatial, direct8x8 bool, _ int) {
		switch {
		case spatial && direct8x8:
			cur.spatial8x8++
		case spatial:
			cur.spatial4x4++
		case direct8x8:
			cur.temporal8x8++
		default:
			cur.temporal4x4++
		}
	}
	defer func() { idec.DirectCensus = nil }()
	defer func() {
		// Asserted only when every stream was present (CI): a partial local run
		// does not claim coverage it did not measure.
		t.Logf("census over all streams: %v", total)
		if os.Getenv("H264_JVT_REQUIRE") == "1" {
			if total.spatial8x8 == 0 || total.temporal8x8+total.temporal4x4 == 0 {
				t.Errorf("a direct path no conformance stream exercises: %v", total)
			}
			if total.spatial4x4 == 0 {
				t.Errorf("no stream exercises spatial direct with direct_8x8_inference_flag = 0 (the 4x4 path): %v", total)
			}
		}
	}()
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
			cur = census{}
			defer func() {
				t.Logf("%s: %v", name, cur)
				total.add(cur)
			}()
			// A picture is valid until the next call, so each call's pictures
			// are hashed before the next one is made.
			frames, err := d.Decode(data)
			if err != nil {
				t.Fatal(err)
			}
			got := frameSums(frames)
			rest, err := d.Flush()
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, frameSums(rest)...)
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
