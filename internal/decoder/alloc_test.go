package decoder

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/go-avkit/h264/internal/testutil"
)

func clipBytes(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.CorpusDir(), name+".264"))
	if err != nil {
		t.Skipf("%s: %v", name, err)
	}
	return data
}

// A motion field is 18 bytes per 4x4 block; one is kept per reference picture
// and recycled when the picture leaves the reference set, so the number ever
// made stays near the reference count however long the stream is, and a
// picture that is not a reference keeps none.
func TestMotionFieldsAreRecycled(t *testing.T) {
	for _, name := range []string{"base_ip_qp26", "main_ipb_cabac"} {
		data := clipBytes(t, name)
		d := New()
		pics, err := d.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rest, _ := d.Flush()
		pics = append(pics, rest...)
		if len(pics) < 10 {
			t.Fatalf("%s: only %d pictures, too few to show recycling", name, len(pics))
		}
		made := len(d.motionOwners) + len(d.motionFree)
		if made > d.buffer.maxNumRefs+2 {
			t.Fatalf("%s: %d motion fields for %d pictures, want about the %d references",
				name, made, len(pics), d.buffer.maxNumRefs)
		}
		for _, p := range pics {
			if p.Motion != nil {
				held := false
				for _, r := range d.buffer.refs {
					held = held || r.pic == p
				}
				if !held {
					t.Fatalf("%s: picture POC %d left the reference set but kept its motion field", name, p.POC)
				}
			}
		}
	}
}

func TestDecodingDoesNotAllocatePerMacroblock(t *testing.T) {
	for _, name := range []string{"base_ip_qp26", "main_ipb_cabac"} {
		data := clipBytes(t, name)
		d := New()
		if _, err := d.Decode(data); err != nil {
			t.Fatalf("%s: warming: %v", name, err)
		}
		d.Flush()

		d = New()
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		pics, err := d.Decode(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		rest, _ := d.Flush()
		runtime.ReadMemStats(&after)

		frames := len(pics) + len(rest)
		if frames == 0 {
			t.Fatalf("%s decoded no frames", name)
		}
		perFrame := (after.Mallocs - before.Mallocs) / uint64(frames)
		t.Logf("%s: %d frames, %d objects per frame", name, frames, perFrame)
		if perFrame > 60 {
			t.Fatalf("%s allocates %d objects per frame; the picture, its motion field and the reference lists are a handful, so this is per macroblock work escaping to the heap",
				name, perFrame)
		}
	}
}
