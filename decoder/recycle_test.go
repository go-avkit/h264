package decoder

import (
	"bytes"
	"path/filepath"
	"runtime"
	"testing"

	idec "github.com/go-avkit/h264/internal/decoder"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/nal"
)

// accessUnits cuts an Annex B stream before every slice that starts a picture
// (a slice NAL after another slice NAL), the way a container hands it over.
func accessUnits(stream []byte) [][]byte {
	var units [][]byte
	var cur []byte
	sawSlice := false
	for _, n := range nal.SplitAnnexB(stream) {
		if len(n) == 0 {
			continue
		}
		if typ := n[0] & 0x1f; typ == 1 || typ == 5 {
			if sawSlice {
				units = append(units, cur)
				cur, sawSlice = nil, false
			}
			sawSlice = true
		}
		cur = append(cur, 0, 0, 0, 1)
		cur = append(cur, n...)
	}
	return append(units, cur)
}

func i420(p *frame.Picture) []byte {
	out := make([]byte, 0, p.Size())
	buf := make([]byte, p.Size())
	p.CopyOut(buf)
	return append(out, buf...)
}

// Recycling the planes of a picture that left the reference set must change
// nothing a caller sees: every conformance clip, fed one access unit per call,
// decodes to the same bytes with the planes reused as with fresh ones.
func TestRecyclingChangesNoOutput(t *testing.T) {
	clips, err := filepath.Glob(filepath.Join("..", "testdata", "conformance", "*.264"))
	if err != nil || len(clips) == 0 {
		t.Fatalf("no clips: %v", err)
	}
	for _, path := range clips {
		stream, _ := conformance(t, filepath.Base(path[:len(path)-len(".264")]))
		fresh := idec.New()
		reuse := idec.New()
		reuse.SetRecycle(true)
		var want, got [][]byte
		step := func(d *idec.Decoder, flush bool, data []byte, into *[][]byte) {
			var pics []*frame.Picture
			var err error
			if flush {
				pics, err = d.Flush()
			} else {
				pics, err = d.Decode(data)
			}
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			for _, p := range pics {
				*into = append(*into, i420(p))
			}
		}
		for _, u := range accessUnits(stream) {
			step(fresh, false, u, &want)
			step(reuse, false, u, &got)
		}
		step(fresh, true, nil, &want)
		step(reuse, true, nil, &got)
		if len(got) != len(want) || len(want) == 0 {
			t.Fatalf("%s: %d pictures with reuse, %d without", path, len(got), len(want))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("%s: picture %d differs with the planes reused", path, i)
			}
		}
	}
}

// The point of reuse: a long stream decoded picture by picture allocates the
// planes of the references and the reorder window, not of every picture.
func TestRecyclingBoundsTheBytesAllocated(t *testing.T) {
	stream, _ := conformance(t, "main_ip_cabac")
	units := accessUnits(stream)
	measure := func(recycle bool) uint64 {
		d := idec.New()
		d.SetRecycle(recycle)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		for _, u := range units {
			if _, err := d.Decode(u); err != nil {
				t.Fatal(err)
			}
		}
		d.Flush()
		runtime.ReadMemStats(&after)
		return after.TotalAlloc - before.TotalAlloc
	}
	fresh, reuse := measure(false), measure(true)
	if len(units) < 8 || reuse*2 > fresh {
		t.Fatalf("%d pictures: %d bytes allocated with reuse, %d without; want under half", len(units), reuse, fresh)
	}
}

// Switching it off hands the pool back: nothing is kept for a decoder that
// does not recycle.
func TestRecyclingOffDropsThePool(t *testing.T) {
	stream, _ := conformance(t, "main_ip_cabac")
	d := idec.New()
	d.SetRecycle(true)
	for _, u := range accessUnits(stream) {
		d.Decode(u)
	}
	d.SetRecycle(false)
	if _, err := d.Decode(nil); err != nil {
		t.Fatal(err)
	}
}
