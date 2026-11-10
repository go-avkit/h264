package decoder

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-avkit/h264/internal/testutil"
)

func splitAnnexB(data []byte) [][]byte {
	var units [][]byte
	start := 0
	for i := 0; i+2 < len(data); {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if i > start {
				units = append(units, data[start:i])
			}
			start = i
			i += 3
			continue
		}
		i++
	}
	if start < len(data) {
		units = append(units, data[start:])
	}
	return units
}

type highStats struct {
	intra8x8Modes  map[int]bool
	intra8x8MBs    int
	inter8x8MBs    int
	transform4x4   int
	intra16x16MBs  int
	nonEmpty8x8Blk int
}

func newHighStats() *highStats {
	return &highStats{intra8x8Modes: make(map[int]bool)}
}

func (s *highStats) collect(g *mbGrid) {
	if g == nil {
		return
	}
	for i := range g.mbs {
		m := &g.mbs[i]
		if !m.Decoded {
			continue
		}
		switch {
		case m.Transform8x8 && m.Intra:
			s.intra8x8MBs++
			for i8 := 0; i8 < 4; i8++ {
				s.intra8x8Modes[int(m.intra4Modes[i8*4])] = true
				if m.cbpLuma&(1<<uint(i8)) != 0 {
					s.nonEmpty8x8Blk++
				}
			}
		case m.Transform8x8:
			s.inter8x8MBs++
			for i8 := 0; i8 < 4; i8++ {
				if m.cbpLuma&(1<<uint(i8)) != 0 {
					s.nonEmpty8x8Blk++
				}
			}
		case m.kind == mbTypeI16x16:
			s.intra16x16MBs++
		default:
			s.transform4x4++
		}
	}
}

func gatherHighStats(t *testing.T, clips []testutil.Clip) *highStats {
	t.Helper()
	stats := newHighStats()
	for _, clip := range clips {
		stream := testutil.LoadStream(t, clip)
		d := New()
		for _, unit := range splitAnnexB(stream) {
			if _, err := d.Decode(unit); err != nil {
				t.Fatalf("%s: Decode: %v", clip.Name, err)
			}
			stats.collect(d.grid)
		}
		if _, err := d.Flush(); err != nil {
			t.Fatalf("%s: Flush: %v", clip.Name, err)
		}
		stats.collect(d.grid)
	}
	return stats
}

func TestHighCorpusExercisesTheEightByEightTransform(t *testing.T) {
	stats := gatherHighStats(t, testutil.HighCorpus)
	if stats.intra8x8MBs == 0 {
		t.Error("no macroblock in the High corpus uses Intra_8x8 prediction")
	}
	if stats.inter8x8MBs == 0 {
		t.Error("no inter macroblock in the High corpus uses the 8x8 transform")
	}
	if stats.transform4x4 == 0 {
		t.Error("the High corpus never falls back to the 4x4 transform, so the flag is never zero")
	}
	if stats.nonEmpty8x8Blk == 0 {
		t.Error("no coded 8x8 luma block appears in the High corpus")
	}
}

func TestHighCorpusCoversEveryIntra8x8Mode(t *testing.T) {
	stats := gatherHighStats(t, testutil.HighCorpus)
	var missing []int
	for mode := 0; mode < 9; mode++ {
		if !stats.intra8x8Modes[mode] {
			missing = append(missing, mode)
		}
	}
	if len(missing) != 0 {
		t.Errorf("Intra_8x8 prediction modes %v never occur in the High corpus", missing)
	}
}

func TestHighCorpusStreamsCarryScalingMatrices(t *testing.T) {
	nonFlat := 0
	for _, clip := range testutil.HighCorpus {
		stream := testutil.LoadStream(t, clip)
		d := New()
		if _, err := d.Decode(stream); err != nil {
			t.Fatalf("%s: Decode: %v", clip.Name, err)
		}
		if _, err := d.Flush(); err != nil {
			t.Fatalf("%s: Flush: %v", clip.Name, err)
		}
		if d.scal == nil {
			t.Fatalf("%s: no scaling tables were built", clip.Name)
		}
		if !d.scal.flat {
			nonFlat++
		}
	}
	if nonFlat < 4 {
		t.Errorf("only %d High clips carry non-flat scaling matrices, want at least four", nonFlat)
	}
}

func TestSplitAnnexBKeepsEveryByte(t *testing.T) {
	stream := testutil.LoadStream(t, testutil.HighCorpus[0])
	var rebuilt []byte
	for _, u := range splitAnnexB(stream) {
		rebuilt = append(rebuilt, u...)
	}
	if !bytes.Equal(rebuilt, stream) {
		t.Fatal("splitting on start codes lost or reordered bytes")
	}
}

var x264RejectedSPS = []struct {
	name string
	nal  []byte
}{
	{"lossless transform bypass", []byte{
		0x67, 0xf4, 0x00, 0x0a, 0xae, 0xb2, 0x05, 0x89, 0xd0, 0x80, 0x00, 0x00,
		0x03, 0x00, 0x80, 0x00, 0x00, 0x0a, 0x07, 0x89, 0x13, 0x24,
	}},
	{"4:2:2 chroma", []byte{
		0x67, 0x7a, 0x00, 0x0a, 0xbc, 0xd9, 0x42, 0xc4, 0xe8, 0x40, 0x00, 0x00,
		0x03, 0x00, 0x40, 0x00, 0x00, 0x05, 0x03, 0xc4, 0x89, 0x65, 0x80,
	}},
	{"ten bit luma", []byte{
		0x67, 0x6e, 0x00, 0x0a, 0xa6, 0xcd, 0x94, 0x2c, 0x4e, 0x84, 0x00, 0x00,
		0x03, 0x00, 0x04, 0x00, 0x00, 0x03, 0x00, 0x50, 0x3c, 0x48, 0x96, 0x58,
	}},
}

func TestBeyondHighProfileStreamsAreRejected(t *testing.T) {
	for _, c := range x264RejectedSPS {
		t.Run(c.name, func(t *testing.T) {
			stream := append([]byte{0x00, 0x00, 0x00, 0x01}, c.nal...)
			_, err := decodeWithFlush(New(), stream)
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("Decode() = %v, want ErrUnsupported", err)
			}
		})
	}
}
