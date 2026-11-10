package cabac

import (
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

type refEncoder struct {
	w             *bits.Writer
	low           uint32
	rng           uint32
	firstBitFlag  bool
	bitsOutstand  int
	ctx           [NumContexts]context
	terminateDone bool
}

func newRefEncoder(sliceQP int, intra bool, initIDC int) *refEncoder {
	e := &refEncoder{w: bits.NewWriterSize(4096), rng: 510, firstBitFlag: true}
	var d Decoder
	if intra {
		d.initContexts(&contextInitI, sliceQP)
	} else {
		d.initContexts(contextInitPB[initIDC], sliceQP)
	}
	e.ctx = d.ctx
	return e
}

func (e *refEncoder) putBit(b uint32) {
	if e.firstBitFlag {
		e.firstBitFlag = false
	} else {
		e.w.WriteBit(b)
	}
	for e.bitsOutstand > 0 {
		e.w.WriteBit(1 - b)
		e.bitsOutstand--
	}
}

func (e *refEncoder) renorm() {
	for e.rng < 256 {
		switch {
		case e.low < 256:
			e.putBit(0)
		case e.low >= 512:
			e.low -= 512
			e.putBit(1)
		default:
			e.low -= 256
			e.bitsOutstand++
		}
		e.rng <<= 1
		e.low <<= 1
	}
}

func (e *refEncoder) encodeDecision(ctxIdx int, bin uint32) {
	c := &e.ctx[ctxIdx]
	q := e.rng >> 6 & 3
	lps := uint32(rangeTabLPS[c.state][q])
	e.rng -= lps
	if bin != uint32(c.mps) {
		e.low += e.rng
		e.rng = lps
		if c.state == 0 {
			c.mps = 1 - c.mps
		}
		c.state = transIdxLPS[c.state]
	} else {
		c.state = transIdxMPS[c.state]
	}
	e.renorm()
}

func (e *refEncoder) encodeBypass(bin uint32) {
	e.low <<= 1
	if bin != 0 {
		e.low += e.rng
	}
	if e.low >= 1024 {
		e.putBit(1)
		e.low -= 1024
	} else if e.low < 512 {
		e.putBit(0)
	} else {
		e.low -= 512
		e.bitsOutstand++
	}
}

func (e *refEncoder) encodeTerminate(bin uint32) {
	e.rng -= 2
	if bin != 0 {
		e.low += e.rng
		e.flush()
		e.terminateDone = true
		return
	}
	e.renorm()
}

func (e *refEncoder) flush() {
	e.rng = 2
	e.renorm()
	e.putBit(e.low >> 9 & 1)
	e.w.WriteBits(e.low>>7&3|1, 2)
	e.w.AlignZero()
}

type op struct {
	kind byte
	ctx  int
	bin  uint32
}

func TestEngineRoundTripsAgainstReferenceEncoder(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	for iter := 0; iter < 300; iter++ {
		qp := rng.Intn(52)
		intra := rng.Intn(2) == 0
		initIDC := rng.Intn(3)

		n := 1 + rng.Intn(400)
		ops := make([]op, 0, n)
		for i := 0; i < n; i++ {
			switch rng.Intn(4) {
			case 0:
				ops = append(ops, op{kind: 'y', bin: uint32(rng.Intn(2))})
			default:
				ops = append(ops, op{kind: 'd', ctx: rng.Intn(NumContexts), bin: uint32(rng.Intn(2))})
			}
		}

		enc := newRefEncoder(qp, intra, initIDC)
		for _, o := range ops {
			if o.kind == 'd' {
				enc.encodeDecision(o.ctx, o.bin)
			} else {
				enc.encodeBypass(o.bin)
			}
		}
		enc.encodeTerminate(1)
		if err := enc.w.Err(); err != nil {
			t.Fatalf("iter %d: encoder: %v", iter, err)
		}

		var dec Decoder
		r := bits.NewReader(enc.w.Bytes())
		if err := dec.Init(r, qp, intra, uint32(initIDC)); err != nil {
			t.Fatalf("iter %d: Init: %v", iter, err)
		}
		for j, o := range ops {
			var got uint32
			if o.kind == 'd' {
				got = dec.DecodeDecision(o.ctx)
			} else {
				got = dec.DecodeBypass()
			}
			if got != o.bin {
				t.Fatalf("iter %d op %d (%c ctx %d): decoded %d, want %d",
					iter, j, o.kind, o.ctx, got, o.bin)
			}
		}
		if dec.DecodeTerminate() != 1 {
			t.Fatalf("iter %d: terminate did not fire", iter)
		}
	}
}

func TestContextInitMatchesSpecFormula(t *testing.T) {
	var d Decoder
	d.initContexts(&contextInitI, 26)
	m, n := int(contextInitI[0][0]), int(contextInitI[0][1])
	if m != 20 || n != -15 {
		t.Fatalf("contextInitI[0] = {%d,%d}, want {20,-15}", m, n)
	}
	pre := clip3(1, 126, ((m*26)>>4)+n)
	wantState, wantMPS := uint8(63-pre), uint8(0)
	if pre > 63 {
		wantState, wantMPS = uint8(pre-64), 1
	}
	gotState, gotMPS := d.State(0)
	if gotState != wantState || gotMPS != wantMPS {
		t.Fatalf("context 0 = state %d mps %d, want state %d mps %d", gotState, gotMPS, wantState, wantMPS)
	}
}

func TestRangeTabStructure(t *testing.T) {
	if rangeTabLPS[0] != [4]uint8{128, 176, 208, 240} {
		t.Fatalf("rangeTabLPS[0] = %v", rangeTabLPS[0])
	}
	if rangeTabLPS[63] != [4]uint8{2, 2, 2, 2} {
		t.Fatalf("rangeTabLPS[63] = %v", rangeTabLPS[63])
	}
	for q := 0; q < 4; q++ {
		for s := 0; s < 63; s++ {
			if rangeTabLPS[s][q] < rangeTabLPS[s+1][q] {
				t.Fatalf("rangeTabLPS column %d increases at state %d", q, s)
			}
		}
	}
	for i := 0; i < 64; i++ {
		if int(transIdxLPS[i]) > i {
			t.Fatalf("transIdxLPS[%d] = %d exceeds its state", i, transIdxLPS[i])
		}
	}
	for i := 0; i < 62; i++ {
		if int(transIdxMPS[i]) != i+1 {
			t.Fatalf("transIdxMPS[%d] = %d, want %d", i, transIdxMPS[i], i+1)
		}
	}
	if transIdxMPS[62] != 62 || transIdxMPS[63] != 63 {
		t.Fatalf("transIdxMPS tail = %d %d", transIdxMPS[62], transIdxMPS[63])
	}
}

func (e *refEncoder) restart() {
	e.low = 0
	e.rng = 510
	e.firstBitFlag = true
	e.bitsOutstand = 0
}

func TestEngineResumesAfterAPayloadInTheMiddle(t *testing.T) {
	rng := rand.New(rand.NewSource(20260901))
	for iter := 0; iter < 50; iter++ {
		qp := rng.Intn(52)
		before := make([]op, 1+rng.Intn(40))
		for i := range before {
			before[i] = op{kind: 'd', ctx: rng.Intn(NumContexts), bin: uint32(rng.Intn(2))}
		}
		payload := make([]byte, 1+rng.Intn(400))
		for i := range payload {
			payload[i] = byte(rng.Intn(256))
		}
		after := make([]op, 1+rng.Intn(40))
		for i := range after {
			after[i] = op{kind: 'd', ctx: rng.Intn(NumContexts), bin: uint32(rng.Intn(2))}
		}

		enc := newRefEncoder(qp, true, 0)
		for _, o := range before {
			enc.encodeDecision(o.ctx, o.bin)
		}
		enc.encodeTerminate(1)
		for _, v := range payload {
			enc.w.WriteBits(uint32(v), 8)
		}
		enc.restart()
		for _, o := range after {
			enc.encodeDecision(o.ctx, o.bin)
		}
		enc.encodeTerminate(1)
		if err := enc.w.Err(); err != nil {
			t.Fatalf("iter %d: encoder: %v", iter, err)
		}

		var dec Decoder
		r := bits.NewReader(enc.w.Bytes())
		if err := dec.Init(r, qp, true, 0); err != nil {
			t.Fatalf("iter %d: Init: %v", iter, err)
		}
		for j, o := range before {
			if got := dec.DecodeDecision(o.ctx); got != o.bin {
				t.Fatalf("iter %d: bin %d before the payload decoded %d, want %d", iter, j, got, o.bin)
			}
		}
		if dec.DecodeTerminate() != 1 {
			t.Fatalf("iter %d: the payload was not announced", iter)
		}
		for !r.ByteAligned() {
			if _, err := r.ReadBit(); err != nil {
				t.Fatalf("iter %d: alignment: %v", iter, err)
			}
		}
		for j := range payload {
			v, err := r.ReadBits(8)
			if err != nil {
				t.Fatalf("iter %d: payload byte %d: %v", iter, j, err)
			}
			if byte(v) != payload[j] {
				t.Fatalf("iter %d: payload byte %d = %d, want %d", iter, j, v, payload[j])
			}
		}
		if err := dec.Restart(r); err != nil {
			t.Fatalf("iter %d: Restart: %v", iter, err)
		}
		for j, o := range after {
			if got := dec.DecodeDecision(o.ctx); got != o.bin {
				t.Fatalf("iter %d: bin %d after the payload decoded %d, want %d", iter, j, got, o.bin)
			}
		}
		if dec.DecodeTerminate() != 1 {
			t.Fatalf("iter %d: the closing terminate did not fire", iter)
		}
	}
}
