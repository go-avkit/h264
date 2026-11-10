package bits

import (
	"bytes"
	"math/rand"
	"testing"
)

func TestAppendBitsMatchesWritingEachBit(t *testing.T) {
	rng := rand.New(rand.NewSource(9))
	for trial := 0; trial < 2000; trial++ {
		var srcBits []uint32
		src := NewWriter()
		for n := rng.Intn(400); n > 0; n-- {
			width := 1 + rng.Intn(32)
			v := rng.Uint32()
			src.WriteBits(v, width)
			for b := width - 1; b >= 0; b-- {
				srcBits = append(srcBits, v>>uint(b)&1)
			}
		}
		prefix := rng.Intn(20)
		from := 0
		if len(srcBits) > 0 {
			from = rng.Intn(len(srcBits) + 2)
		}
		got := NewWriter()
		want := NewWriter()
		for i := 0; i < prefix; i++ {
			b := uint32(rng.Intn(2))
			got.WriteBit(b)
			want.WriteBit(b)
		}
		got.AppendBits(src, from)
		for i := from; i < len(srcBits); i++ {
			want.WriteBit(srcBits[i])
		}
		if got.BitsWritten() != want.BitsWritten() || !bytes.Equal(got.Bytes(), want.Bytes()) {
			t.Fatalf("trial %d: %d source bits from %d after %d: got %d bits % x, want %d bits % x",
				trial, len(srcBits), from, prefix, got.BitsWritten(), got.Bytes(), want.BitsWritten(), want.Bytes())
		}
	}
}

func TestAppendBitsCarriesTheSourceError(t *testing.T) {
	src := NewWriter()
	src.WriteBits(1, 40)
	dst := NewWriter()
	dst.AppendBits(src, 0)
	if dst.Err() == nil {
		t.Fatal("a failed source appended without its error")
	}
}
