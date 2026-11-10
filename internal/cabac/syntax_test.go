package cabac

import (
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

func decodeOver(t *testing.T, ops []op, f func(*Decoder)) {
	t.Helper()
	const qp = 26
	enc := newRefEncoder(qp, true, 0)
	for _, o := range ops {
		if o.kind == 'y' {
			enc.encodeBypass(o.bin)
		} else {
			enc.encodeDecision(o.ctx, o.bin)
		}
	}
	enc.encodeTerminate(1)
	if err := enc.w.Err(); err != nil {
		t.Fatalf("encoder: %v", err)
	}
	var dec Decoder
	if err := dec.Init(bits.NewReader(enc.w.Bytes()), qp, true, 0); err != nil {
		t.Fatalf("Init: %v", err)
	}
	f(&dec)
	if dec.DecodeTerminate() != 1 {
		t.Fatal("the syntax element consumed the wrong number of bins")
	}
}

func decision(ctx int, bin uint32) op { return op{kind: 'd', ctx: ctx, bin: bin} }

func bypass(bin uint32) op { return op{kind: 'y', bin: bin} }

func unary(base int, ctxOf func(i int) int, n int) []op {
	var ops []op
	for i := 0; i < n; i++ {
		ops = append(ops, decision(base+ctxOf(i), 1))
	}
	return append(ops, decision(base+ctxOf(n), 0))
}

func TestMBQPDeltaRoundTrip(t *testing.T) {
	ctxOf := func(i int) int {
		switch {
		case i == 0:
			return 0
		case i == 1:
			return 2
		default:
			return 3
		}
	}
	for _, prevNonZero := range []bool{false, true} {
		for delta := -26; delta <= 25; delta++ {
			mapped := -2 * delta
			if delta > 0 {
				mapped = 2*delta - 1
			}
			ops := unary(offMBQPDelta, ctxOf, mapped)
			if prevNonZero {
				ops[0].ctx = offMBQPDelta + 1
			}
			want, prevNonZero := delta, prevNonZero
			decodeOver(t, ops, func(d *Decoder) {
				if got := d.MBQPDelta(prevNonZero); got != want {
					t.Fatalf("MBQPDelta(%v) = %d, want %d", prevNonZero, got, want)
				}
			})
		}
	}
}

func TestSubMBTypePRoundTrip(t *testing.T) {
	cases := [][]op{
		{decision(offSubMBTypeP, 1)},
		{decision(offSubMBTypeP, 0), decision(offSubMBTypeP+1, 0)},
		{decision(offSubMBTypeP, 0), decision(offSubMBTypeP+1, 1), decision(offSubMBTypeP+2, 1)},
		{decision(offSubMBTypeP, 0), decision(offSubMBTypeP+1, 1), decision(offSubMBTypeP+2, 0)},
	}
	for want, ops := range cases {
		want := want
		decodeOver(t, ops, func(d *Decoder) {
			if got := d.SubMBTypeP(); got != want {
				t.Fatalf("SubMBTypeP() = %d, want %d", got, want)
			}
		})
	}
}

func TestSubMBTypeBRoundTrip(t *testing.T) {
	b := offSubMBTypeB
	cases := map[int][]op{
		0:  {decision(b, 0)},
		1:  {decision(b, 1), decision(b+1, 0), decision(b+3, 0)},
		2:  {decision(b, 1), decision(b+1, 0), decision(b+3, 1)},
		3:  {decision(b, 1), decision(b+1, 1), decision(b+2, 0), decision(b+3, 0), decision(b+3, 0)},
		4:  {decision(b, 1), decision(b+1, 1), decision(b+2, 0), decision(b+3, 0), decision(b+3, 1)},
		5:  {decision(b, 1), decision(b+1, 1), decision(b+2, 0), decision(b+3, 1), decision(b+3, 0)},
		6:  {decision(b, 1), decision(b+1, 1), decision(b+2, 0), decision(b+3, 1), decision(b+3, 1)},
		7:  {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 0), decision(b+3, 0), decision(b+3, 0)},
		8:  {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 0), decision(b+3, 0), decision(b+3, 1)},
		9:  {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 0), decision(b+3, 1), decision(b+3, 0)},
		10: {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 0), decision(b+3, 1), decision(b+3, 1)},
		11: {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 1), decision(b+3, 0)},
		12: {decision(b, 1), decision(b+1, 1), decision(b+2, 1), decision(b+3, 1), decision(b+3, 1)},
	}
	for want, ops := range cases {
		want, ops := want, ops
		decodeOver(t, ops, func(d *Decoder) {
			if got := d.SubMBTypeB(); got != want {
				t.Fatalf("SubMBTypeB() = %d, want %d", got, want)
			}
		})
	}
}

func TestRefIdxRoundTrip(t *testing.T) {
	ctxOf := func(i int) int {
		switch i {
		case 0:
			return 0
		case 1:
			return 4
		}
		return 5
	}
	for inc := 0; inc < 4; inc++ {
		for want := 0; want < 6; want++ {
			ops := unary(offRefIdx, ctxOf, want)
			ops[0].ctx = offRefIdx + inc
			inc, want := inc, want
			decodeOver(t, ops, func(d *Decoder) {
				if got := d.RefIdx(inc); got != want {
					t.Fatalf("RefIdx(%d) = %d, want %d", inc, got, want)
				}
			})
		}
	}
}

func mvdOps(base, v int) []op {
	mag := v
	if mag < 0 {
		mag = -mag
	}
	if mag == 0 {
		return []op{decision(base, 0)}
	}
	ctxAt := func(i int) int {
		if i > 4 {
			i = 4
		}
		return base + 2 + i
	}
	ops := []op{decision(base, 1)}
	prefix := mag
	if prefix > 9 {
		prefix = 9
	}
	for i := 1; i < prefix; i++ {
		ops = append(ops, decision(ctxAt(i), 1))
	}
	if mag < 9 {
		ops = append(ops, decision(ctxAt(prefix), 0))
	} else {
		rest := mag - 9
		k := 3
		for rest >= 1<<uint(k) {
			ops = append(ops, bypass(1))
			rest -= 1 << uint(k)
			k++
		}
		ops = append(ops, bypass(0))
		for k--; k >= 0; k-- {
			ops = append(ops, bypass(uint32(rest>>uint(k)&1)))
		}
	}
	sign := uint32(0)
	if v < 0 {
		sign = 1
	}
	return append(ops, bypass(sign))
}

func TestMVDRoundTrip(t *testing.T) {
	values := []int{0, 1, -1, 2, -3, 7, -8, 9, -9, 10, 16, -17, 40, -41, 100, -1000, 8000}
	for _, base := range []int{MVDHorizontal, MVDVertical} {
		for _, absSum := range []int{0, 3, 40} {
			for _, v := range values {
				ops := mvdOps(base, v)
				inc := 0
				if absSum > 2 {
					inc++
				}
				if absSum > 32 {
					inc++
				}
				ops[0].ctx = base + inc
				v, absSum, base := v, absSum, base
				decodeOver(t, ops, func(d *Decoder) {
					if got := d.MVD(base, absSum); got != v {
						t.Fatalf("MVD(%d, %d) = %d, want %d", base, absSum, got, v)
					}
				})
			}
		}
	}
}

func TestMBTypePRoundTrip(t *testing.T) {
	p := offMBTypeP
	cases := map[int][]op{
		0: {decision(p, 0), decision(p+1, 0), decision(p+2, 0)},
		3: {decision(p, 0), decision(p+1, 0), decision(p+2, 1)},
		2: {decision(p, 0), decision(p+1, 1), decision(p+3, 0)},
		1: {decision(p, 0), decision(p+1, 1), decision(p+3, 1)},
	}
	for want, ops := range cases {
		want, ops := want, ops
		decodeOver(t, ops, func(d *Decoder) {
			got, intra := d.MBTypeP()
			if intra || got != want {
				t.Fatalf("MBTypeP() = %d intra %v, want %d", got, intra, want)
			}
		})
	}
}

func TestMBTypePIntraPrefix(t *testing.T) {
	ops := []op{decision(offMBTypeP, 1), decision(offMBTypeIinP, 0)}
	decodeOver(t, ops, func(d *Decoder) {
		got, intra := d.MBTypeP()
		if !intra || got != 0 {
			t.Fatalf("MBTypeP() = %d intra %v, want 0 intra", got, intra)
		}
	})
}

func TestMBTypeBRoundTrip(t *testing.T) {
	b := offMBTypeB
	head := []op{decision(b, 1), decision(b+3, 1)}
	bits4 := func(v int) []op {
		return []op{
			decision(b+4, uint32(v>>3&1)),
			decision(b+5, uint32(v>>2&1)),
			decision(b+5, uint32(v>>1&1)),
			decision(b+5, uint32(v&1)),
		}
	}
	with := func(extra ...[]op) []op {
		ops := append([]op(nil), head...)
		for _, e := range extra {
			ops = append(ops, e...)
		}
		return ops
	}

	for inc := 0; inc < 3; inc++ {
		inc := inc
		decodeOver(t, []op{decision(b+inc, 0)}, func(d *Decoder) {
			got, intra := d.MBTypeB(inc)
			if intra || got != 0 {
				t.Fatalf("MBTypeB direct = %d intra %v", got, intra)
			}
		})
		for i := 0; i < 2; i++ {
			want := 1 + i
			ops := []op{decision(b+inc, 1), decision(b+3, 0), decision(b+5, uint32(i))}
			decodeOver(t, ops, func(d *Decoder) {
				got, intra := d.MBTypeB(inc)
				if intra || got != want {
					t.Fatalf("MBTypeB() = %d intra %v, want %d", got, intra, want)
				}
			})
		}
	}

	for v := 0; v < 8; v++ {
		want := v + 3
		decodeOver(t, with(bits4(v)), func(d *Decoder) {
			got, intra := d.MBTypeB(0)
			if intra || got != want {
				t.Fatalf("MBTypeB() = %d intra %v, want %d", got, intra, want)
			}
		})
	}

	for _, c := range []struct{ bits, want int }{{14, 11}, {15, 22}} {
		c := c
		decodeOver(t, with(bits4(c.bits)), func(d *Decoder) {
			got, intra := d.MBTypeB(0)
			if intra || got != c.want {
				t.Fatalf("MBTypeB() = %d intra %v, want %d", got, intra, c.want)
			}
		})
	}

	for _, v := range []int{8, 9, 10, 11, 12} {
		for last := 0; last < 2; last++ {
			want := v<<1 + last - 4
			ops := with(bits4(v), []op{decision(b+5, uint32(last))})
			decodeOver(t, ops, func(d *Decoder) {
				got, intra := d.MBTypeB(0)
				if intra || got != want {
					t.Fatalf("MBTypeB() = %d intra %v, want %d", got, intra, want)
				}
			})
		}
	}

	ops := with(bits4(13), []op{decision(offMBTypeIinB, 0)})
	decodeOver(t, ops, func(d *Decoder) {
		got, intra := d.MBTypeB(0)
		if !intra || got != 0 {
			t.Fatalf("MBTypeB() = %d intra %v, want 0 intra", got, intra)
		}
	})
}
