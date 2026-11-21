package cabac

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

// oldDecoder is the residual decoding of main at f2b000e, frozen: the engine
// with its own fields and the bin-by-bin ResidualBlock and ResidualBlock8x8.
// The kernel path of Decoder must give the same coefficients, the same engine
// state and the same contexts after every block, on any stream.
type oldDecoder struct {
	data       []byte
	ptr        int
	value      uint32
	rng        uint32
	bitsNeeded int
	overflow   bool
	ctx        [NumContexts]uint8
}

// restart is Restart of f2b000e on a byte-aligned reader at bit 0; ok is false
// where it refused (fewer than nine bits).
func (d *oldDecoder) restart(data []byte) (ok bool) {
	if len(data)*8 < 9 {
		return false
	}
	d.data = data
	d.ptr = 0
	d.rng = 510
	d.value = uint32(d.data[d.ptr])<<8 | uint32(d.data[d.ptr+1])
	d.ptr += 2
	d.bitsNeeded = -8
	return true
}

func (d *oldDecoder) fetch(shift int) {
	if d.ptr < len(d.data) {
		d.value |= uint32(d.data[d.ptr]) << uint(shift)
	}
	d.ptr++
}

func (d *oldDecoder) DecodeDecision(ctxIdx int) uint32 {
	s := d.ctx[ctxIdx]
	lps := uint32(mergedLPS[s][d.rng>>6&3])
	d.rng -= lps
	scaled := d.rng << 7
	if d.value < scaled {
		d.ctx[ctxIdx] = mergedNextMPS[s]
		sh := uint(^d.rng>>8) & 1
		d.rng <<= sh
		d.value <<= sh
		d.bitsNeeded += int(sh)
		if d.bitsNeeded >= 0 {
			d.fetch(0)
			d.bitsNeeded = -8
		}
		return uint32(s & 1)
	}
	n := uint(lpsShift[lps>>3])
	d.value = (d.value - scaled) << n
	d.rng = lps << n
	d.ctx[ctxIdx] = mergedNextLPS[s]
	d.bitsNeeded += int(n)
	if d.bitsNeeded >= 0 {
		d.fetch(d.bitsNeeded)
		d.bitsNeeded -= 8
	}
	return uint32(s&1) ^ 1
}

func (d *oldDecoder) DecodeBypass() uint32 {
	d.value <<= 1
	d.bitsNeeded++
	if d.bitsNeeded >= 0 {
		d.fetch(0)
		d.bitsNeeded = -8
	}
	scaled := d.rng << 7
	if d.value >= scaled {
		d.value -= scaled
		return 1
	}
	return 0
}

// boundedLevel clamps a level above 32767 to 32767 and records it: the old
// outcome on a level out of range is a CLAMP with the overflow flag, no error.
func (d *oldDecoder) boundedLevel(v int) int {
	if v > maxCoeffLevel {
		d.overflow = true
		return maxCoeffLevel
	}
	return v
}

const maxCoeffLevel = 32767

func (d *oldDecoder) absLevelMinus1At(base, limit, numGt1, numEq1 int) int {
	prefix := 0
	for prefix < 14 {
		if d.DecodeDecision(absLevelIncAt(base, limit, prefix, numGt1, numEq1)) == 0 {
			break
		}
		prefix++
	}
	if prefix < 14 {
		return prefix
	}
	value := 14
	k := 0
	for d.DecodeBypass() == 1 {
		value += 1 << uint(k)
		k++
		if k > 24 {
			break
		}
	}
	for k--; k >= 0; k-- {
		value += int(d.DecodeBypass()) << uint(k)
	}
	return value
}

func (d *oldDecoder) residualBlock(coeffs []int32, cat, condTermA, condTermB, numC8x8 int) int {
	for i := range coeffs {
		coeffs[i] = 0
	}
	inc := condTermA + 2*condTermB
	if d.DecodeDecision(offCodedBlockFlag+catOffsetCodedBlockFlag[cat]+inc) != 1 {
		return 0
	}
	maxNumCoeff := len(coeffs)
	levelBase, levelLimit := absLevelBase(cat)
	var pos [16]uint8
	n := 0
	last := false
	sigBase := offSignificant + catOffsetSignificant[cat]
	lastBase := offLastSignificant + catOffsetSignificant[cat]
	for i := 0; i < maxNumCoeff-1; i++ {
		sigInc := i
		if cat == CatChromaDC {
			sigInc = significanceInc(cat, i, numC8x8)
		}
		if d.DecodeDecision(sigBase+sigInc) == 1 {
			pos[n] = uint8(i)
			n++
			if d.DecodeDecision(lastBase+sigInc) == 1 {
				last = true
				break
			}
		}
	}
	if !last {
		pos[n] = uint8(maxNumCoeff - 1)
		n++
	}
	numGt1, numEq1 := 0, 0
	total := 0
	for k := n - 1; k >= 0; k-- {
		i := pos[k]
		level := d.boundedLevel(d.absLevelMinus1At(levelBase, levelLimit, numGt1, numEq1) + 1)
		if level > 1 {
			numGt1++
		} else {
			numEq1++
		}
		if d.DecodeBypass() == 1 {
			coeffs[i] = int32(-level)
		} else {
			coeffs[i] = int32(level)
		}
		total++
	}
	return total
}

func (d *oldDecoder) residualBlock8x8(coeffs *[64]int32) int {
	for i := range coeffs {
		coeffs[i] = 0
	}
	var pos [64]uint8
	n := 0
	last := false
	for i := 0; i < 63; i++ {
		if d.DecodeDecision(offSignificant8x8+int(significantCoeffFlagOffset8x8[i])) == 1 {
			pos[n] = uint8(i)
			n++
			if d.DecodeDecision(offLastSignificant8x8+int(lastCoeffFlagOffset8x8[i])) == 1 {
				last = true
				break
			}
		}
	}
	if !last {
		pos[n] = 63
		n++
	}
	numGt1, numEq1 := 0, 0
	total := 0
	for k := n - 1; k >= 0; k-- {
		i := pos[k]
		level := d.boundedLevel(d.absLevelMinus1At(offAbsLevel8x8, 4, numGt1, numEq1) + 1)
		if level > 1 {
			numGt1++
		} else {
			numEq1++
		}
		if d.DecodeBypass() == 1 {
			coeffs[i] = int32(-level)
		} else {
			coeffs[i] = int32(level)
		}
		total++
	}
	return total
}

var diffLengths = [CatLuma8x8]int{16, 15, 16, 4, 15}

// differentialRun decodes the same blocks from the same stream and contexts
// with the frozen decoder and the kernel path, and reports the first
// difference. in is: seed, context mode, then the stream.
func differentialRun(t testing.TB, seed byte, wide bool, stream []byte) {
	t.Helper()
	rng := rand.New(rand.NewSource(int64(seed)))
	var o oldDecoder
	var d Decoder
	for i := range o.ctx {
		if wide {
			o.ctx[i] = uint8(rng.Intn(128)) // the merged tables hold 128 entries: pStateIdx 0..63
		} else {
			o.ctx[i] = uint8(rng.Intn(126))
		}
	}
	d.ctx = o.ctx
	okOld := o.restart(stream)
	errNew := d.Restart(bits.NewReader(stream))
	if okOld != (errNew == nil) {
		t.Fatalf("refusal differs: old ok=%v, new err=%v (stream %x)", okOld, errNew, stream)
	}
	if !okOld {
		return
	}
	for blk := 0; blk < 8; blk++ {
		cat := rng.Intn(CatLuma8x8 + 1)
		a, b := rng.Intn(2), rng.Intn(2)
		var co, cn []int32
		var no, nn int
		if cat == CatLuma8x8 {
			var x, y [64]int32
			no = o.residualBlock8x8(&x)
			// the new path starts with the same context flags: 8x8 has none
			nn = d.ResidualBlock8x8(&y)
			co, cn = x[:], y[:]
		} else {
			co = make([]int32, diffLengths[cat])
			cn = make([]int32, diffLengths[cat])
			no = o.residualBlock(co, cat, a, b, 1)
			nn = d.ResidualBlock(cn, cat, a, b, 1)
		}
		switch {
		case no != nn:
			t.Fatalf("block %d cat %d: count old %d new %d (stream %x)", blk, cat, no, nn, stream)
		case !reflect.DeepEqual(co, cn):
			t.Fatalf("block %d cat %d: coefficients\n old %v\n new %v\n stream %x", blk, cat, co, cn, stream)
		case o.ptr != d.st.Ptr || o.value != d.st.Value || o.rng != d.st.Rng || o.bitsNeeded != d.st.BitsNeeded:
			t.Fatalf("block %d cat %d: engine old (%d,%d,%d,%d) new (%d,%d,%d,%d) (stream %x)", blk, cat,
				o.ptr, o.value, o.rng, o.bitsNeeded, d.st.Ptr, d.st.Value, d.st.Rng, d.st.BitsNeeded, stream)
		case o.overflow != d.LevelOverflow():
			t.Fatalf("block %d: overflow old %v new %v (stream %x)", blk, o.overflow, d.LevelOverflow(), stream)
		case o.ctx != d.ctx:
			t.Fatalf("block %d cat %d: contexts differ (stream %x)", blk, cat, stream)
		}
	}
}

func TestResidualDifferentialAgainstFrozenDecoder(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	for round := 0; round < 30000; round++ {
		n := rng.Intn(48) // many streams end inside a block
		if round%10 == 0 {
			n = rng.Intn(3)
		}
		stream := make([]byte, n)
		rng.Read(stream)
		if round%5 == 0 { // offsets at the top of the range: malformed
			for i := range stream {
				stream[i] |= 0xf0
			}
		}
		differentialRun(t, byte(round), round%7 == 0, stream)
	}
}

// A level beyond 32767 is clamped and flagged, never an error, in both.
func TestResidualDifferentialLevelOverflow(t *testing.T) {
	seen := 0
	for seed := 0; seed < 256 && seen == 0; seed++ {
		stream := make([]byte, 40)
		for i := range stream {
			stream[i] = 0xff
		}
		var o oldDecoder
		o.restart(stream)
		rng := rand.New(rand.NewSource(int64(seed)))
		for i := range o.ctx {
			o.ctx[i] = uint8(50+rng.Intn(13)) << 1
		}
		o.value = 65279 - uint32(seed%8)
		var x [16]int32
		o.residualBlock(x[:], CatLuma4x4, 0, 0, 1)
		if o.overflow {
			seen++
			var d Decoder
			d.Restart(bits.NewReader(stream))
			d.ctx = [NumContexts]uint8{}
			rng = rand.New(rand.NewSource(int64(seed)))
			for i := range d.ctx {
				d.ctx[i] = uint8(50+rng.Intn(13)) << 1
			}
			d.st.Value = 65279 - uint32(seed%8)
			var y [16]int32
			d.ResidualBlock(y[:], CatLuma4x4, 0, 0, 1)
			if !d.LevelOverflow() || x != y {
				t.Fatalf("overflow: old %v, new %v flag %v", x, y, d.LevelOverflow())
			}
			for _, v := range y {
				if v > maxCoeffLevel || v < -maxCoeffLevel {
					t.Fatalf("level %d beyond the clamp", v)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no overflow reached")
	}
}

func FuzzResidualDifferential(f *testing.F) {
	f.Add([]byte{1, 0, 0x12, 0x34, 0x56, 0x78, 0x9a, 0xbc, 0xde, 0xf0, 0x11, 0x22})
	f.Add([]byte{2, 1, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff})
	f.Add([]byte{3, 0, 0x00})
	f.Add([]byte{4, 1, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, in []byte) {
		if len(in) < 2 {
			return
		}
		differentialRun(t, in[0], in[1]&1 == 1, in[2:])
	})
}
