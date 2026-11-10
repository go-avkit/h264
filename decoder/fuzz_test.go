package decoder

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzDecode feeds arbitrary bytes to the decoder, whole and then in two
// pieces cut where the input says, and holds it to one thing: no input, however
// malformed, panics, hangs or allocates past its limits. Every refusal is an
// acceptable answer. The seeds are the conformance streams of
// testdata/conformance and the two of testdata/fuzzseed; findings go to
// testdata/fuzz/FuzzDecode.
//
//	go test -run='^$' -fuzz='^FuzzDecode$' -fuzztime=60s ./decoder
func FuzzDecode(f *testing.F) {
	var paths []string
	for _, pattern := range []string{"conformance/*.264", "fuzzseed/*"} {
		p, err := filepath.Glob(filepath.Join("..", "testdata", pattern))
		if err != nil {
			f.Fatal(err)
		}
		paths = append(paths, p...)
	}
	if len(paths) == 0 {
		f.Fatal("no seed")
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		if len(data) > 64<<10 {
			data = data[:64<<10]
		}
		f.Add(data, uint16(len(data)/2))
	}
	f.Fuzz(func(t *testing.T, data []byte, cut uint16) {
		d := New(Limits{MaxFrameMBs: 1 << 12, MaxNALBytes: 1 << 20, EnforceLevel: true})
		i := min(int(cut), len(data))
		for _, piece := range [][]byte{data[:i], data[i:]} {
			frames, _ := d.Decode(piece)
			for _, fr := range frames {
				if fr.Width <= 0 || fr.Height <= 0 || len(fr.AppendI420(nil)) != fr.I420Size() {
					t.Fatalf("picture %dx%d", fr.Width, fr.Height)
				}
			}
		}
		d.Flush()
	})
}
