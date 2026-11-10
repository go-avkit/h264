package decoder

import (
	"os"
	"testing"

	"github.com/go-avkit/h264/internal/nal"
)

// BenchmarkDecodeMidGOP decodes the IDR picture and the 45 pictures that
// follow it from the Annex B file named by H264_MIDGOP_CLIP (for example a
// 1280x720 x264 clip, GOP 60), the work a poster frame 1.5 s into a GOP costs.
// The stream is fed one access unit per Decode call, as a container reader
// hands it over. Skipped when the variable is unset.
//
//	ffmpeg -f lavfi -i testsrc2=size=1280x720:rate=30 -t 10 -c:v libx264 -g 60 -pix_fmt yuv420p -f h264 g60.264
//	H264_MIDGOP_CLIP=g60.264 GOMAXPROCS=1 go test -run='^$' -bench MidGOP ./decoder
func BenchmarkDecodeMidGOP(b *testing.B) {
	path := os.Getenv("H264_MIDGOP_CLIP")
	if path == "" {
		b.Skip("H264_MIDGOP_CLIP not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	var units [][]byte
	var cur []byte
	slices := 0
	for _, n := range nal.SplitAnnexB(data) {
		if len(n) == 0 {
			continue
		}
		if typ := n[0] & 0x1f; typ == 1 || typ == 5 {
			if slices == 46 {
				break
			}
			slices++
			if slices > 1 {
				units = append(units, cur)
				cur = nil
			}
		}
		cur = append(cur, 0, 0, 0, 1)
		cur = append(cur, n...)
	}
	units = append(units, cur)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := New(Limits{})
		for _, u := range units {
			if _, err := d.Decode(u); err != nil {
				b.Fatalf("Decode: %v", err)
			}
		}
		if _, err := d.Flush(); err != nil {
			b.Fatalf("Flush: %v", err)
		}
	}
}
