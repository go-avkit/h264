// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzStream drives the whole reader over a fuzzed elementary stream, the way a
// demuxer of a downloaded file does: split it into units, keep the latest
// parameter sets, and for every slice read its header and references, derive
// its order count, reference set and lists, and the weighting of every pair of
// references. It holds the package to one thing: no stream, however malformed,
// panics. Every refusal along the way is an acceptable answer.
//
// The seeds are two short x264 encodes (testdata/fuzzseed): progressive with
// B-pyramid, four references and weighted prediction, and interlaced (PAFF/MBAFF).
// The first byte of an input picks the framing, so both the Annex B and the
// length-prefixed reader are reached.
//
//	go test -run='^$' -fuzz='^FuzzStream$' -fuzztime=60s
func FuzzStream(f *testing.F) {
	paths, err := filepath.Glob("testdata/fuzzseed/*")
	if err != nil || len(paths) == 0 {
		f.Fatalf("no seed: %v", err)
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			f.Fatalf("seed: %v", err)
		}
		f.Add(append([]byte{0}, data...))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		var units []Unit
		if data[0]%4 == 0 {
			units, err = SplitAnnexB(data[1:])
		} else {
			units, err = SplitLengthPrefixed(data[1:], int(data[0]%4))
		}
		if err != nil {
			return
		}
		var (
			sps     SPS
			pps     PPS
			haveSPS bool
			havePPS bool
			pred    = NewPredictor()
		)
		for _, u := range units {
			_ = u.Unescape()
			switch u.Type {
			case UnitSPS:
				if s, err := ParseSPS(u); err == nil {
					sps, haveSPS = s, true
				}
			case UnitPPS:
				if !haveSPS {
					continue
				}
				if p, err := ParsePPS(u, sps); err == nil {
					pps, havePPS = p, true
				}
			case UnitIDR, UnitNonIDR:
				if !havePPS {
					continue
				}
				h, ref, err := ParseSliceReferences(u, sps, pps)
				if err != nil {
					continue
				}
				_ = h.BeginsPicture()
				_ = h.Type.String()
				p, err := pred.Picture(u, h, ref, sps, pps)
				if err != nil {
					continue
				}
				_ = pred.Held()
				for i, r0 := range p.L0 {
					_ = WeightingFor(pps, h, ref, p.POC.Value, r0.POC, r0.POC, i, -1, r0.LongTerm)
					for j, r1 := range p.L1 {
						w := WeightingFor(pps, h, ref, p.POC.Value, r0.POC, r1.POC, i, j, r0.LongTerm || r1.LongTerm)
						_ = w.Trivial()
					}
				}
			}
		}
		if havePPS {
			_, _ = SplitPictures(units, sps, pps)
		}
	})
}
