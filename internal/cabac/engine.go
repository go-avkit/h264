package cabac

import (
	"errors"

	"github.com/go-avkit/h264/internal/bits"
)

var (
	ErrNotAligned = errors.New("go264/cabac: slice data does not start byte aligned")
	ErrAlignment  = errors.New("go264/cabac: cabac_alignment_one_bit is not one")
	ErrInitIDC    = errors.New("go264/cabac: cabac_init_idc out of range")
)

const NumContexts = 1024

type context struct {
	state uint8
	mps   uint8
}

// Decoder is the arithmetic decoding engine of 9.3.1.2 and 9.3.3.2.
//
// The specification reads codIOffset one bit at a time. This keeps the same
// arithmetic with the offset scaled by 2^7 and seven bits of lookahead below it
// (value = codIOffset<<7 | next seven bits), refilled a byte at a time, so a
// decision compares against range<<7 and a renormalisation is a shift. FFmpeg
// does the same (libavcodec/cabac_functions.h keeps its offset scaled and refills
// 16 bits at once). The bit position the specification would be at is
// recoverable (consumed), and the reader is put back there when a terminate
// bin of 1 ends the arithmetic code, which is the only time anything else reads
// the stream (I_PCM samples, the end of the slice).
type Decoder struct {
	r          *bits.Reader
	data       []byte
	ptr        int    // the next byte to fetch, counting those past the end
	value      uint32 // codIOffset<<7 | lookahead
	rng        uint32 // codIRange
	bitsNeeded int    // -8..-1: shifts left before the next byte is due, minus eight
	overflow   bool
	ctx        [NumContexts]context
}

// lpsShift is the renormalisation shift after a least probable symbol, by
// rangeTabLPS >> 3: the number of doublings that bring the new range to at least
// 256 (6 for 6 and 7, 5 for 8..15, ... 1 for 128..255).
var lpsShift = [32]uint8{
	6, 5, 4, 4, 3, 3, 3, 3,
	2, 2, 2, 2, 2, 2, 2, 2,
	1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
}

func clip3(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (d *Decoder) initContexts(table *[1024][2]int8, sliceQPY int) {
	qp := clip3(0, 51, sliceQPY)
	for i := 0; i < NumContexts; i++ {
		m := int(table[i][0])
		n := int(table[i][1])
		pre := clip3(1, 126, ((m*qp)>>4)+n)
		if pre <= 63 {
			d.ctx[i].state = uint8(63 - pre)
			d.ctx[i].mps = 0
		} else {
			d.ctx[i].state = uint8(pre - 64)
			d.ctx[i].mps = 1
		}
	}
}

// consumed is the position, in bits from the start of the data, that the
// specification's bit-by-bit reader would be at: everything fetched, less the
// lookahead that was not yet shifted into codIOffset.
func (d *Decoder) consumed() int { return d.ptr*8 + d.bitsNeeded + 1 }

// Overrun is how many bits the specification's reader would have had to read
// past the end of the data (each read past the end yields a zero bit).
func (d *Decoder) Overrun() int { return max(0, d.consumed()-len(d.data)*8) }

// fetch ORs the next byte (zero past the end) into the low byte of the value.
func (d *Decoder) fetch(shift int) {
	if d.ptr < len(d.data) {
		d.value |= uint32(d.data[d.ptr]) << uint(shift)
	}
	d.ptr++
}

// sync puts the reader where the specification's would be.
func (d *Decoder) sync() {
	_ = d.r.Seek(min(d.consumed(), len(d.data)*8))
}

func (d *Decoder) LevelOverflow() bool { return d.overflow }

func (d *Decoder) Init(r *bits.Reader, sliceQPY int, intra bool, initIDC uint32) error {
	d.overflow = false
	if !r.ByteAligned() {
		if err := d.consumeAlignmentBits(r); err != nil {
			return err
		}
	}
	if intra {
		d.initContexts(&contextInitI, sliceQPY)
	} else {
		if initIDC > 2 {
			return ErrInitIDC
		}
		d.initContexts(contextInitPB[initIDC], sliceQPY)
	}
	return d.Restart(r)
}

func (d *Decoder) consumeAlignmentBits(r *bits.Reader) error {
	for !r.ByteAligned() {
		b, err := r.ReadBit()
		if err != nil {
			return err
		}
		if b != 1 {
			return ErrAlignment
		}
	}
	return nil
}

func (d *Decoder) State(ctxIdx int) (state, mps uint8) {
	return d.ctx[ctxIdx].state, d.ctx[ctxIdx].mps
}

// Restart starts the arithmetic code afresh at the reader's position, which must be
// on a byte boundary (9.3.1.2): at the start of the slice data, and after the
// samples of an I_PCM macroblock.
func (d *Decoder) Restart(r *bits.Reader) error {
	if !r.ByteAligned() {
		return ErrNotAligned
	}
	if r.BitsLeft() < 9 {
		return bits.ErrOverrun
	}
	d.r = r
	d.data = r.Data()
	d.ptr = r.BitPos() / 8
	d.rng = 510
	// codIOffset is the first nine bits; the seven below them are the lookahead.
	d.value = uint32(d.data[d.ptr])<<8 | uint32(d.data[d.ptr+1])
	d.ptr += 2
	d.bitsNeeded = -8
	return nil
}

// DecodeDecision is DecodeDecision of 9.3.3.2.1 and its RenormD.
func (d *Decoder) DecodeDecision(ctxIdx int) uint32 {
	c := &d.ctx[ctxIdx]
	lps := uint32(rangeTabLPS[c.state][d.rng>>6&3])
	d.rng -= lps
	scaled := d.rng << 7
	if d.value < scaled {
		bin := uint32(c.mps)
		c.state = transIdxMPS[c.state]
		if scaled < 256<<7 { // the range fell below 256: one doubling
			d.rng = scaled >> 6
			d.value <<= 1
			d.bitsNeeded++
			if d.bitsNeeded >= 0 {
				d.fetch(0)
				d.bitsNeeded = -8
			}
		}
		return bin
	}
	n := int(lpsShift[lps>>3])
	d.value = (d.value - scaled) << uint(n)
	d.rng = lps << uint(n)
	bin := uint32(1 - c.mps)
	if c.state == 0 {
		c.mps = 1 - c.mps
	}
	c.state = transIdxLPS[c.state]
	d.bitsNeeded += n
	if d.bitsNeeded >= 0 {
		d.fetch(d.bitsNeeded)
		d.bitsNeeded -= 8
	}
	return bin
}

// DecodeBypass is DecodeBypass of 9.3.3.2.3.
func (d *Decoder) DecodeBypass() uint32 {
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

// DecodeTerminate is DecodeTerminate of 9.3.3.2.2.3. A bin of 1 ends the code with
// no renormalisation, and the reader is left at the bit the specification's would
// be at, where the I_PCM samples or the end of the slice follow.
func (d *Decoder) DecodeTerminate() uint32 {
	d.rng -= 2
	scaled := d.rng << 7
	if d.value >= scaled {
		d.sync()
		return 1
	}
	if scaled < 256<<7 {
		d.rng = scaled >> 6
		d.value <<= 1
		d.bitsNeeded++
		if d.bitsNeeded >= 0 {
			d.fetch(0)
			d.bitsNeeded = -8
		}
	}
	return 0
}
