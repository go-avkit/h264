package nal

import (
	"bytes"
	"testing"
	"time"
)

func TestScannerCostIsLinearInTheInput(t *testing.T) {
	const n = 1 << 21
	data := make([]byte, 0, n)
	for len(data) < n {
		data = append(data, 0x00, 0x00, 0x01, 0x0C)
	}
	start := time.Now()
	s := NewScanner()
	s.Append(data)
	count := 0
	for {
		if _, ok := s.Next(); !ok {
			break
		}
		count++
	}
	s.Flush()
	if took := time.Since(start); took > time.Second {
		t.Fatalf("scanning %d bytes into %d units took %v; the scanner is compacting once per unit again",
			n, count, took)
	}
}

func TestScannerDiscardsDataThatCannotStartAUnit(t *testing.T) {
	s := NewScanner()
	for i := 0; i < 512; i++ {
		s.Append(make([]byte, 4096))
		for {
			if _, ok := s.Next(); !ok {
				break
			}
		}
	}
	if n := s.Buffered(); n > 64 {
		t.Fatalf("2 MiB with no start code left %d bytes buffered", n)
	}
}

func TestScannerReleasesConsumedUnits(t *testing.T) {
	unit := append([]byte{0x00, 0x00, 0x01, 0x09}, bytes.Repeat([]byte{0x11}, 1000)...)
	s := NewScanner()
	for i := 0; i < 2000; i++ {
		s.Append(unit)
		for {
			if _, ok := s.Next(); !ok {
				break
			}
		}
		if n := s.Buffered(); n > 4*len(unit) {
			t.Fatalf("after %d units the scanner still holds %d bytes", i+1, n)
		}
	}
}

func TestScannerSplitsTheSameUnitsWhateverTheChunking(t *testing.T) {
	stream := buildAnnexBStream(annexBTestUnits(), []bool{true, false, true})
	want := SplitAnnexB(stream)
	for _, chunk := range []int{1, 2, 3, 5, 17, 64, 4096} {
		s := NewScanner()
		var got [][]byte
		for off := 0; off < len(stream); off += chunk {
			end := off + chunk
			if end > len(stream) {
				end = len(stream)
			}
			s.Append(stream[off:end])
			for {
				u, ok := s.Next()
				if !ok {
					break
				}
				got = append(got, u)
			}
		}
		if u, ok := s.Flush(); ok {
			got = append(got, u)
		}
		if len(got) != len(want) {
			t.Fatalf("chunk %d: %d units, want %d", chunk, len(got), len(want))
		}
		for i := range want {
			if !bytes.Equal(got[i], want[i]) {
				t.Fatalf("chunk %d unit %d: % x, want % x", chunk, i, got[i], want[i])
			}
		}
	}
}
