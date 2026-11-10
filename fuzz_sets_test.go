// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "testing"

// FuzzParameterSets feeds arbitrary bytes to the sequence and picture parameter
// set readers, which hold scaling lists, a hypothetical reference decoder with a
// stated count, a picture order cycle with a stated count and the VUI. None of it
// may panic or allocate past its bound, and what a set says about the buffer must
// stay within what a level allows. The seeds are the parameter sets of every
// stream in testdata (read through the same code the differential test uses).
//
//	go test -run='^$' -fuzz='^FuzzParameterSets$' -fuzztime=60s
func FuzzParameterSets(f *testing.F) {
	spss, ppss := payloadsOf(f, UnitSPS), payloadsOf(f, UnitPPS)
	for i, s := range spss {
		f.Add(s, ppss[i%len(ppss)])
	}
	f.Fuzz(func(t *testing.T, sps, pps []byte) {
		s, err := ParseSPS(Unit{Type: UnitSPS, Payload: sps})
		if err == nil || s.Width != 0 {
			if n := s.MaxDpbFrames(); n < 1 || n > 16 {
				t.Fatalf("MaxDpbFrames %d", n)
			}
			if n := s.MaxNumReorder(); n < 0 || n > 16 {
				t.Fatalf("MaxNumReorder %d", n)
			}
			if len(s.OffsetForRefFrame) > 255 {
				t.Fatalf("%d offsets", len(s.OffsetForRefFrame))
			}
		}
		_, _ = ParsePPS(Unit{Type: UnitPPS, Payload: pps}, s)
	})
}
