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
