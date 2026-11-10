package testutil

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
)

const (
	NALTypeSliceNonIDR = 1
	NALTypeSliceIDR    = 5
	NALTypeSEI         = 6
	NALTypeSPS         = 7
	NALTypePPS         = 8
)

func AllClips() []Clip {
	out := make([]Clip, 0, len(Corpus)+len(MainCorpus)+len(HighCorpus))
	out = append(out, Corpus...)
	out = append(out, MainCorpus...)
	out = append(out, HighCorpus...)
	return out
}

var (
	streamsOnce sync.Once
	streamsVal  [][]byte
)

func Streams() [][]byte {
	streamsOnce.Do(func() {
		for _, c := range AllClips() {
			data, err := os.ReadFile(filepath.Join(CorpusDir(), c.Name+".264"))
			if err != nil {
				continue
			}
			streamsVal = append(streamsVal, data)
		}
	})
	return streamsVal
}

func splitAnnexB(data []byte) [][]byte {
	prefix := []byte{0x00, 0x00, 0x01}
	var out [][]byte
	i := bytes.Index(data, prefix)
	if i < 0 {
		return nil
	}
	i += 3
	for {
		j := bytes.Index(data[i:], prefix)
		var unit []byte
		if j < 0 {
			unit = data[i:]
		} else {
			unit = data[i : i+j]
		}
		for len(unit) > 0 && unit[len(unit)-1] == 0 {
			unit = unit[:len(unit)-1]
		}
		if len(unit) > 0 {
			out = append(out, unit)
		}
		if j < 0 {
			return out
		}
		i += j + 3
	}
}

func unescape(src []byte) []byte {
	dst := make([]byte, 0, len(src))
	zeros := 0
	for _, b := range src {
		if zeros >= 2 && b == 0x03 {
			zeros = 0
			continue
		}
		dst = append(dst, b)
		if b == 0 {
			zeros++
		} else {
			zeros = 0
		}
	}
	return dst
}

func EBSPUnits() [][]byte {
	var out [][]byte
	for _, s := range Streams() {
		out = append(out, splitAnnexB(s)...)
	}
	return out
}

func RBSPOfType(types ...uint8) [][]byte {
	want := map[uint8]bool{}
	for _, t := range types {
		want[t] = true
	}
	var out [][]byte
	seen := map[string]bool{}
	for _, u := range EBSPUnits() {
		if len(u) == 0 || u[0]&0x80 != 0 {
			continue
		}
		if !want[u[0]&0x1F] {
			continue
		}
		rbsp := unescape(u[1:])
		if len(rbsp) == 0 {
			continue
		}
		key := string(rbsp)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, rbsp)
	}
	return out
}
