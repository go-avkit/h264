package decoder

import (
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func conformance(t testing.TB, name string) (stream, yuv []byte) {
	t.Helper()
	read := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "testdata", "conformance", p))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	zr, err := gzip.NewReader(bytes.NewReader(read(name + ".yuv.gz")))
	if err != nil {
		t.Fatal(err)
	}
	yuv, err = io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return read(name + ".264"), yuv
}

// The public Decoder returns, picture by picture, the I420 bytes ffmpeg
// produced for the stream, whether the stream arrives whole or in pieces.
func TestDecodeMatchesReference(t *testing.T) {
	for _, name := range []string{"main_ipb_cabac", "main_ipb_cavlc", "main_ip_weightp", "high_ipb_8x8_cabac"} {
		t.Run(name, func(t *testing.T) {
			stream, want := conformance(t, name)
			for _, chunk := range []int{len(stream), 1000} {
				d := New(Limits{})
				var got []byte
				for off := 0; off < len(stream); off += chunk {
					frames, err := d.Decode(stream[off:min(off+chunk, len(stream))])
					if err != nil {
						t.Fatal(err)
					}
					for _, f := range frames {
						got = f.AppendI420(got)
					}
				}
				rest, err := d.Flush()
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range rest {
					got = f.AppendI420(got)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("chunk %d: %d bytes decoded, reference has %d, or a byte differs", chunk, len(got), len(want))
				}
			}
		})
	}
}

func TestFrameGeometry(t *testing.T) {
	stream, _ := conformance(t, "main_ip_cabac")
	d := New(Limits{})
	frames, err := d.Decode(stream)
	if err != nil || len(frames) == 0 {
		t.Fatalf("%d frames, %v", len(frames), err)
	}
	f := frames[0]
	if f.I420Size() != f.Width*f.Height*3/2 || len(f.AppendI420(nil)) != f.I420Size() {
		t.Fatalf("I420Size %d, packed %d bytes for %dx%d", f.I420Size(), len(f.AppendI420(nil)), f.Width, f.Height)
	}
}

func TestLimitsRefuseAnOversizedPicture(t *testing.T) {
	stream, _ := conformance(t, "main_ip_cabac")
	d := New(Limits{MaxFrameMBs: 1})
	if _, err := d.Decode(stream); !errors.Is(err, ErrOverLimit) {
		t.Fatalf("got %v, want ErrOverLimit", err)
	}
}
