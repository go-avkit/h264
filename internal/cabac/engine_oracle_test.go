package cabac

import (
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

// oracle is the arithmetic decoder exactly as 9.3.1.2 and 9.3.3.2 state it, one
// bit at a time, which is how this package decoded before the offset was kept
// scaled. It is slow and obviously right, and the engine is held to it.
type oracle struct {
	r          *bits.Reader
	rng, off   uint32
	overrun    int
	ctx        [NumContexts]context
	terminated bool
}

func (o *oracle) readBit() uint32 {
	b, err := o.r.ReadBit()
	if err != nil {
		o.overrun++
		return 0
	}
	return b
}

func (o *oracle) renorm() {
	for o.rng < 256 {
		o.rng <<= 1
		o.off = o.off<<1 | o.readBit()
	}
}

func (o *oracle) decision(i int) uint32 {
	c := &o.ctx[i]
	lps := uint32(rangeTabLPS[c.state][o.rng>>6&3])
	o.rng -= lps
	var bin uint32
	if o.off >= o.rng {
		bin = uint32(1 - c.mps)
		o.off -= o.rng
		o.rng = lps
		if c.state == 0 {
			c.mps = 1 - c.mps
		}
		c.state = transIdxLPS[c.state]
	} else {
		bin = uint32(c.mps)
		c.state = transIdxMPS[c.state]
	}
	o.renorm()
	return bin
}

func (o *oracle) bypass() uint32 {
	o.off = o.off<<1 | o.readBit()
	if o.off >= o.rng {
		o.off -= o.rng
		return 1
	}
	return 0
}

func (o *oracle) terminate() uint32 {
	o.rng -= 2
	if o.off >= o.rng {
		return 1
	}
	o.renorm()
	return 0
}

// TestTheEngineAgreesWithTheBitByBitDecoder feeds arbitrary bytes (so every state
// the arithmetic code can reach, and the end of the data) to both decoders and
// requires the same bin from every decision, bypass and terminate, the reader at
// the same bit after every terminate of 1, and the same overrun.
func TestTheEngineAgreesWithTheBitByBitDecoder(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	for iter := 0; iter < 400; iter++ {
		data := make([]byte, 2+rng.Intn(120))
		rng.Read(data)
		qp, intra, idc := rng.Intn(52), rng.Intn(2) == 0, uint32(rng.Intn(3))

		var dec Decoder
		r := bits.NewReader(data)
		if err := dec.Init(r, qp, intra, idc); err != nil {
			continue // fewer than nine bits after the alignment bits
		}
		// The data begins on a byte boundary, so Init starts the code at bit 0: the
		// oracle reads its nine bits from there, from the same contexts.
		o := &oracle{r: bits.NewReader(data), rng: 510}
		o.ctx = dec.ctx
		first, _ := o.r.ReadBits(9)
		o.off = first
		for n := 0; n < 3000; n++ {
			switch k := rng.Intn(10); {
			case k < 6:
				i := rng.Intn(NumContexts)
				if got, want := dec.DecodeDecision(i), o.decision(i); got != want {
					t.Fatalf("iter %d op %d: decision %d, oracle %d", iter, n, got, want)
				}
			case k < 9:
				if got, want := dec.DecodeBypass(), o.bypass(); got != want {
					t.Fatalf("iter %d op %d: bypass %d, oracle %d", iter, n, got, want)
				}
			default:
				got, want := dec.DecodeTerminate(), o.terminate()
				if got != want {
					t.Fatalf("iter %d op %d: terminate %d, oracle %d", iter, n, got, want)
				}
				if got == 1 {
					if r.BitPos() != o.r.BitPos() {
						t.Fatalf("iter %d op %d: reader at bit %d, oracle at %d", iter, n, r.BitPos(), o.r.BitPos())
					}
					// The code ended: start another from the next byte boundary,
					// as an I_PCM macroblock does.
					for !r.ByteAligned() {
						r.ReadBit()
					}
					for !o.r.ByteAligned() {
						o.r.ReadBit()
					}
					if r.BitsLeft() < 9 {
						goto next
					}
					if err := dec.Restart(r); err != nil {
						t.Fatal(err)
					}
					o.rng = 510
					v, _ := o.r.ReadBits(9)
					o.off = v
					o.overrun = 0
				}
			}
			if dec.Overrun() != o.overrun {
				t.Fatalf("iter %d op %d: overrun %d, oracle %d", iter, n, dec.Overrun(), o.overrun)
			}
		}
	next:
	}
}
