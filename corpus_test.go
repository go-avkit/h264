// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import (
	"os"
	"path/filepath"
	"testing"
)

// streamsOfTheCorpus are the paths of every stream the repository ships: the
// x264 conformance clips and the two fuzz seeds.
func streamsOfTheCorpus(t testing.TB) []string {
	t.Helper()
	var paths []string
	for _, pattern := range []string{"conformance/*.264", "fuzzseed/*"} {
		p, err := filepath.Glob(filepath.Join("testdata", pattern))
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p...)
	}
	return paths
}

// payloadsOf are the payloads of the units of one type in every stream the
// repository ships, as stored (escaped).
func payloadsOf(t testing.TB, typ UnitType) [][]byte {
	t.Helper()
	var out [][]byte
	for _, p := range streamsOfTheCorpus(t) {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		units, err := SplitAnnexB(data)
		if err != nil {
			t.Fatal(err)
		}
		for _, u := range units {
			if u.Type == typ {
				out = append(out, u.Payload)
			}
		}
	}
	if len(out) < 20 {
		t.Fatalf("only %d units of type %d found", len(out), typ)
	}
	return out
}

// TestEveryUnitOfTheCorpusIsRead walks the repository's streams the way a decoder
// does: each set is read, each slice header is read against the sets it names,
// and what comes out is within what the format allows. These streams were checked
// against the reader the decoder used before this one (internal/syntax, from
// oops1/go.264) field by field when it was replaced, and the decoder decodes them
// bit for bit.
func TestEveryUnitOfTheCorpusIsRead(t *testing.T) {
	slices, sets := 0, 0
	for _, path := range streamsOfTheCorpus(t) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		units, err := SplitAnnexB(data)
		if err != nil {
			t.Fatal(err)
		}
		seqs, pics := map[uint32]SPS{}, map[uint32]PPS{}
		for _, u := range units {
			switch u.Type {
			case UnitSPS:
				s, err := ParseSPS(u)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				seqs[s.ID] = s
				sets++
			case UnitPPS:
				_, spsID, err := PPSIDs(u)
				if err != nil {
					t.Fatal(err)
				}
				p, err := ParsePPS(u, seqs[spsID])
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				pics[p.ID] = p
				sets++
			case UnitIDR, UnitNonIDR:
				id, err := SlicePPSID(u)
				if err != nil {
					t.Fatal(err)
				}
				p := pics[id]
				h, ref, err := ParseSliceReferences(u, seqs[p.SPSID], p)
				if err != nil {
					t.Fatalf("%s: %v", path, err)
				}
				if ref.DataBit <= 0 || ref.DataBit > len(u.Unescape())*8 || ref.QP < 0 || ref.QP > 51 || h.PPSID != id {
					t.Fatalf("%s: slice %d reads as %+v %+v", path, slices, h, ref)
				}
				slices++
			}
		}
	}
	if slices < 200 || sets < 40 {
		t.Fatalf("read only %d slices and %d sets", slices, sets)
	}
}
