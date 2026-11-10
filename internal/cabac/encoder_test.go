package cabac

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/bits"
)

func encodeAndDecode(t *testing.T, qp int, intra bool, initIDC uint32, encode func(*Encoder), decode func(*Decoder)) {
	t.Helper()
	w := bits.NewWriterSize(512)
	var enc Encoder
	if err := enc.Init(w, qp, intra, initIDC); err != nil {
		t.Fatalf("Encoder.Init: %v", err)
	}
	encode(&enc)
	enc.EncodeTerminate(1)
	enc.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("writer error: %v", err)
	}
	var dec Decoder
	r := bits.NewReader(w.Bytes())
	if err := dec.Init(r, qp, intra, initIDC); err != nil {
		t.Fatalf("Decoder.Init: %v", err)
	}
	decode(&dec)
	if dec.DecodeTerminate() != 1 {
		t.Fatal("decoding the encoded element left the bitstream out of sync with EndOfSlice")
	}
}

// encodeAndDecodeMBType is like encodeAndDecode but knows that mb_type ==
// 25 (I_PCM) resolves the terminate bin internally: per 9.3.4.5 that
// terminate bin, once it fires, must be followed by a flush (and, in a real
// bitstream, byte-aligned PCM samples plus a decoding-engine re-init)
// before any further bin can be coded, so the harness must not append its
// own closing EncodeTerminate(1)/DecodeTerminate() in that case.
func encodeAndDecodeMBType(t *testing.T, qp int, intra bool, initIDC uint32, terminal int, encode func(*Encoder), decode func(*Decoder)) {
	t.Helper()
	w := bits.NewWriterSize(512)
	var enc Encoder
	if err := enc.Init(w, qp, intra, initIDC); err != nil {
		t.Fatalf("Encoder.Init: %v", err)
	}
	encode(&enc)
	if terminal != 25 {
		enc.EncodeTerminate(1)
	}
	enc.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("writer error: %v", err)
	}
	var dec Decoder
	r := bits.NewReader(w.Bytes())
	if err := dec.Init(r, qp, intra, initIDC); err != nil {
		t.Fatalf("Decoder.Init: %v", err)
	}
	decode(&dec)
	if terminal != 25 {
		if dec.DecodeTerminate() != 1 {
			t.Fatal("decoding the encoded element left the bitstream out of sync with EndOfSlice")
		}
	}
}

func TestIntraMBTypeRoundTrip(t *testing.T) {
	for _, qp := range []int{0, 26, 51} {
		for inc := 0; inc < 3; inc++ {
			for mbType := 0; mbType <= 25; mbType++ {
				qp, inc, mbType := qp, inc, mbType
				encodeAndDecodeMBType(t, qp, true, 0, mbType, func(e *Encoder) {
					e.IntraMBType(inc, mbType)
				}, func(d *Decoder) {
					if got := d.IntraMBType(inc); got != mbType {
						t.Fatalf("IntraMBType(%d) = %d, want %d (qp=%d)", inc, got, mbType, qp)
					}
				})
			}
		}
	}
}

func TestEncoderMBTypePRoundTrip(t *testing.T) {
	for _, mbType := range []int{0, 1, 2, 3} {
		mbType := mbType
		encodeAndDecode(t, 26, false, 0, func(e *Encoder) {
			e.MBTypeP(mbType, false)
		}, func(d *Decoder) {
			got, intra := d.MBTypeP()
			if intra || got != mbType {
				t.Fatalf("MBTypeP() = %d intra %v, want %d not intra", got, intra, mbType)
			}
		})
	}
	for sub := 0; sub <= 25; sub++ {
		sub := sub
		encodeAndDecodeMBType(t, 26, false, 0, sub, func(e *Encoder) {
			e.MBTypeP(sub, true)
		}, func(d *Decoder) {
			got, intra := d.MBTypeP()
			if !intra || got != sub {
				t.Fatalf("MBTypeP() intra = %d intra %v, want %d intra", got, intra, sub)
			}
		})
	}
}

func TestEncoderMBTypeBRoundTrip(t *testing.T) {
	for inc := 0; inc < 3; inc++ {
		for mbType := 0; mbType <= 22; mbType++ {
			inc, mbType := inc, mbType
			encodeAndDecode(t, 26, false, 1, func(e *Encoder) {
				e.MBTypeB(inc, mbType, false)
			}, func(d *Decoder) {
				got, intra := d.MBTypeB(inc)
				if intra || got != mbType {
					t.Fatalf("MBTypeB(%d) = %d intra %v, want %d not intra", inc, got, intra, mbType)
				}
			})
		}
	}
	for sub := 0; sub <= 25; sub++ {
		sub := sub
		encodeAndDecodeMBType(t, 26, false, 1, sub, func(e *Encoder) {
			e.MBTypeB(0, sub, true)
		}, func(d *Decoder) {
			got, intra := d.MBTypeB(0)
			if !intra || got != sub {
				t.Fatalf("MBTypeB(0) intra = %d intra %v, want %d intra", got, intra, sub)
			}
		})
	}
}

func TestEncoderSubMBTypePRoundTrip(t *testing.T) {
	for sub := 0; sub <= 3; sub++ {
		sub := sub
		encodeAndDecode(t, 26, false, 0, func(e *Encoder) {
			e.SubMBTypeP(sub)
		}, func(d *Decoder) {
			if got := d.SubMBTypeP(); got != sub {
				t.Fatalf("SubMBTypeP() = %d, want %d", got, sub)
			}
		})
	}
}

func TestEncoderSubMBTypeBRoundTrip(t *testing.T) {
	for sub := 0; sub <= 12; sub++ {
		sub := sub
		encodeAndDecode(t, 26, false, 1, func(e *Encoder) {
			e.SubMBTypeB(sub)
		}, func(d *Decoder) {
			if got := d.SubMBTypeB(); got != sub {
				t.Fatalf("SubMBTypeB() = %d, want %d", got, sub)
			}
		})
	}
}

func TestEncoderRefIdxRoundTrip(t *testing.T) {
	for inc := 0; inc < 4; inc++ {
		for ref := 0; ref <= 31; ref++ {
			inc, ref := inc, ref
			encodeAndDecode(t, 26, false, 0, func(e *Encoder) {
				e.RefIdx(inc, ref)
			}, func(d *Decoder) {
				if got := d.RefIdx(inc); got != ref {
					t.Fatalf("RefIdx(%d) = %d, want %d", inc, got, ref)
				}
			})
		}
	}
}

func TestEncoderMVDRoundTrip(t *testing.T) {
	values := []int{
		0, 1, -1, 2, -2, 7, -7, 8, -8, 9, -9, 10, -10,
		16, -16, 17, -17, 40, -40, 100, -100, 1000, -1000, 8191, -8191, 20000, -20000,
	}
	for _, base := range []int{MVDHorizontal, MVDVertical} {
		for _, absSum := range []int{0, 1, 2, 3, 32, 33, 1000} {
			for _, v := range values {
				base, absSum, v := base, absSum, v
				encodeAndDecode(t, 26, false, 0, func(e *Encoder) {
					e.MVD(base, absSum, v)
				}, func(d *Decoder) {
					if got := d.MVD(base, absSum); got != v {
						t.Fatalf("MVD(base=%d,absSum=%d) = %d, want %d", base, absSum, got, v)
					}
				})
			}
		}
	}
}

func TestEncoderMVDRoundTripRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	for i := 0; i < 300; i++ {
		v := rng.Intn(40001) - 20000
		absSum := rng.Intn(200)
		base := MVDHorizontal
		if rng.Intn(2) == 0 {
			base = MVDVertical
		}
		encodeAndDecode(t, rng.Intn(52), rng.Intn(2) == 0, uint32(rng.Intn(3)), func(e *Encoder) {
			e.MVD(base, absSum, v)
		}, func(d *Decoder) {
			if got := d.MVD(base, absSum); got != v {
				t.Fatalf("MVD(base=%d,absSum=%d) = %d, want %d", base, absSum, got, v)
			}
		})
	}
}

func TestIntraChromaPredModeRoundTrip(t *testing.T) {
	for inc := 0; inc < 3; inc++ {
		for mode := 0; mode <= 3; mode++ {
			inc, mode := inc, mode
			encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
				e.IntraChromaPredMode(inc, mode)
			}, func(d *Decoder) {
				if got := d.IntraChromaPredMode(inc); got != mode {
					t.Fatalf("IntraChromaPredMode(%d) = %d, want %d", inc, got, mode)
				}
			})
		}
	}
}

func TestIntra4x4PredModeRoundTrip(t *testing.T) {
	for predMode := 0; predMode <= 8; predMode++ {
		for mode := 0; mode <= 8; mode++ {
			predMode, mode := predMode, mode
			encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
				e.Intra4x4PredMode(predMode, mode)
			}, func(d *Decoder) {
				if got := d.Intra4x4PredMode(predMode); got != mode {
					t.Fatalf("Intra4x4PredMode(pred=%d) = %d, want %d", predMode, got, mode)
				}
			})
		}
	}
}

func TestCodedBlockPatternLumaRoundTrip(t *testing.T) {
	cbps := []int{0, 1, 2, 3, 4, 5, 8, 9, 15, 0x0F}
	neighbours := []int{0, 0x02, 0x04, 0x08, 0x0F, CBPUnavailable, CBPPCM}
	for _, cbp := range cbps {
		for _, left := range neighbours {
			for _, top := range neighbours {
				cbp, left, top := cbp, left, top
				encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
					e.CodedBlockPatternLuma(left, top, cbp)
				}, func(d *Decoder) {
					if got := d.CodedBlockPatternLuma(left, top); got != cbp {
						t.Fatalf("CodedBlockPatternLuma(%d,%d) = %d, want %d", left, top, got, cbp)
					}
				})
			}
		}
	}
}

func TestCodedBlockPatternChromaRoundTrip(t *testing.T) {
	neighbours := []int{0, 0x10, 0x20, CBPUnavailable, CBPPCM}
	for v := 0; v <= 2; v++ {
		for _, left := range neighbours {
			for _, top := range neighbours {
				v, left, top := v, left, top
				encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
					e.CodedBlockPatternChroma(left, top, v)
				}, func(d *Decoder) {
					if got := d.CodedBlockPatternChroma(left, top); got != v {
						t.Fatalf("CodedBlockPatternChroma(%d,%d) = %d, want %d", left, top, got, v)
					}
				})
			}
		}
	}
}

func TestEncoderMBQPDeltaRoundTrip(t *testing.T) {
	for _, qp := range []int{0, 1, 15, 26, 37, 50, 51} {
		for _, initIDC := range []uint32{0, 1, 2} {
			for _, prevNonZero := range []bool{false, true} {
				for delta := -26; delta <= 25; delta++ {
					qp, initIDC, prevNonZero, delta := qp, initIDC, prevNonZero, delta
					encodeAndDecode(t, qp, false, initIDC, func(e *Encoder) {
						e.MBQPDelta(delta, prevNonZero)
					}, func(d *Decoder) {
						if got := d.MBQPDelta(prevNonZero); got != delta {
							t.Fatalf("MBQPDelta(%v) = %d, want %d (qp=%d initIDC=%d)", prevNonZero, got, delta, qp, initIDC)
						}
					})
				}
			}
		}
	}
}

func TestEndOfSliceRoundTrip(t *testing.T) {
	for _, end := range []bool{false, true} {
		end := end
		w := bits.NewWriterSize(64)
		var enc Encoder
		if err := enc.Init(w, 26, true, 0); err != nil {
			t.Fatalf("Init: %v", err)
		}
		enc.EndOfSlice(end)
		if !end {
			enc.EncodeTerminate(1)
		}
		enc.Finish()
		var dec Decoder
		if err := dec.Init(bits.NewReader(w.Bytes()), 26, true, 0); err != nil {
			t.Fatalf("Decoder.Init: %v", err)
		}
		if got := dec.EndOfSlice(); got != end {
			t.Fatalf("EndOfSlice() = %v, want %v", got, end)
		}
		if !end {
			if dec.DecodeTerminate() != 1 {
				t.Fatal("closing terminate did not fire")
			}
		}
	}
}

func TestMBSkipFlagRoundTrip(t *testing.T) {
	for inc := 0; inc < 3; inc++ {
		for _, skip := range []bool{false, true} {
			inc, skip := inc, skip
			encodeAndDecode(t, 26, false, 0, func(e *Encoder) {
				e.MBSkipFlagP(inc, skip)
			}, func(d *Decoder) {
				if got := d.MBSkipFlagP(inc); got != skip {
					t.Fatalf("MBSkipFlagP(%d) = %v, want %v", inc, got, skip)
				}
			})
			encodeAndDecode(t, 26, false, 1, func(e *Encoder) {
				e.MBSkipFlagB(inc, skip)
			}, func(d *Decoder) {
				if got := d.MBSkipFlagB(inc); got != skip {
					t.Fatalf("MBSkipFlagB(%d) = %v, want %v", inc, got, skip)
				}
			})
		}
	}
}

func TestCodedBlockFlagRoundTrip(t *testing.T) {
	cats := []int{CatIntra16x16DC, CatIntra16x16AC, CatLuma4x4, CatChromaDC, CatChromaAC}
	for _, cat := range cats {
		for condA := 0; condA < 2; condA++ {
			for condB := 0; condB < 2; condB++ {
				for _, flag := range []bool{false, true} {
					cat, condA, condB, flag := cat, condA, condB, flag
					encodeAndDecode(t, 26, true, 0, func(e *Encoder) {
						e.CodedBlockFlag(cat, condA, condB, flag)
					}, func(d *Decoder) {
						if got := d.CodedBlockFlag(cat, condA, condB); got != flag {
							t.Fatalf("CodedBlockFlag(cat=%d) = %v, want %v", cat, got, flag)
						}
					})
				}
			}
		}
	}
}

var residualBlockLength = map[int]int{
	CatIntra16x16DC: 16,
	CatIntra16x16AC: 15,
	CatLuma4x4:      16,
	CatChromaDC:     4,
	CatChromaAC:     15,
}

func checkResidualRoundTrip(t *testing.T, qp int, intra bool, initIDC uint32, coeffs []int32, cat, condA, condB, numC8x8 int) {
	t.Helper()
	want := append([]int32(nil), coeffs...)
	encodeAndDecode(t, qp, intra, initIDC, func(e *Encoder) {
		e.ResidualBlock(coeffs, cat, condA, condB, numC8x8)
	}, func(d *Decoder) {
		got := make([]int32, len(coeffs))
		d.ResidualBlock(got, cat, condA, condB, numC8x8)
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("ResidualBlock cat %d coeff[%d] = %d, want %d (all: got %v want %v)", cat, i, got[i], want[i], got, want)
			}
		}
	})
}

func TestResidualBlockEdgeCases(t *testing.T) {
	for cat, n := range residualBlockLength {
		cat, n := cat, n

		coeffs := make([]int32, n)
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 0, 0, 1)

		coeffs = make([]int32, n)
		coeffs[0] = 5
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 1, 0, 1)

		coeffs = make([]int32, n)
		coeffs[0] = -1
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 1, 1, 1)

		coeffs = make([]int32, n)
		coeffs[n-1] = -3
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 0, 1, 1)

		coeffs = make([]int32, n)
		coeffs[n-1] = 1
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 0, 0, 1)

		coeffs = make([]int32, n)
		for i := 0; i < n; i++ {
			coeffs[i] = 1
		}
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 1, 1, 1)

		coeffs = make([]int32, n)
		for i := 0; i < n; i++ {
			if i%2 == 0 {
				coeffs[i] = int32(20 + i)
			} else {
				coeffs[i] = int32(-(30 + i))
			}
		}
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 1, 1, 1)

		coeffs = make([]int32, n)
		coeffs[0] = 32767
		checkResidualRoundTrip(t, 26, true, 0, coeffs, cat, 0, 0, 1)
	}
}

func TestResidualBlockRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	cats := []int{CatIntra16x16DC, CatIntra16x16AC, CatLuma4x4, CatChromaDC, CatChromaAC}
	for i := 0; i < 400; i++ {
		cat := cats[rng.Intn(len(cats))]
		n := residualBlockLength[cat]
		coeffs := make([]int32, n)
		for j := range coeffs {
			switch rng.Intn(4) {
			case 0:
			case 1:
				v := int32(1 + rng.Intn(3))
				if rng.Intn(2) == 0 {
					v = -v
				}
				coeffs[j] = v
			default:
				v := int32(1 + rng.Intn(8000))
				if rng.Intn(2) == 0 {
					v = -v
				}
				coeffs[j] = v
			}
		}
		qp := rng.Intn(52)
		intra := rng.Intn(2) == 0
		initIDC := uint32(rng.Intn(3))
		condA, condB := rng.Intn(2), rng.Intn(2)
		checkResidualRoundTrip(t, qp, intra, initIDC, coeffs, cat, condA, condB, 1)
	}
}

func randomOps(rng *rand.Rand, n int) []op {
	ops := make([]op, n)
	for i := range ops {
		if rng.Intn(4) == 0 {
			ops[i] = bypass(uint32(rng.Intn(2)))
		} else {
			ops[i] = decision(rng.Intn(NumContexts), uint32(rng.Intn(2)))
		}
	}
	return ops
}

func applyOp(e *Encoder, o op) {
	if o.kind == 'd' {
		e.EncodeDecision(o.ctx, o.bin)
	} else {
		e.EncodeBypass(o.bin)
	}
}

type mixedAction struct {
	encode func(*Encoder)
	decode func(*Decoder, *testing.T)
}

func randomMixedAction(rng *rand.Rand) mixedAction {
	switch rng.Intn(15) {
	case 0:
		// mbType stays below 25 (I_PCM): that value resolves the terminate
		// bin internally and would require a flush plus a decoding-engine
		// re-init before any further bin could legally follow, which this
		// harness does not perform mid-sequence.
		inc, mbType := rng.Intn(3), rng.Intn(25)
		return mixedAction{
			func(e *Encoder) { e.IntraMBType(inc, mbType) },
			func(d *Decoder, t *testing.T) {
				if got := d.IntraMBType(inc); got != mbType {
					t.Fatalf("IntraMBType(%d) = %d, want %d", inc, got, mbType)
				}
			},
		}
	case 1:
		mbType, intra := rng.Intn(4), false
		if rng.Intn(2) == 0 {
			mbType, intra = rng.Intn(25), true
		}
		return mixedAction{
			func(e *Encoder) { e.MBTypeP(mbType, intra) },
			func(d *Decoder, t *testing.T) {
				got, gotIntra := d.MBTypeP()
				if got != mbType || gotIntra != intra {
					t.Fatalf("MBTypeP() = %d intra %v, want %d intra %v", got, gotIntra, mbType, intra)
				}
			},
		}
	case 2:
		inc := rng.Intn(3)
		mbType, intra := rng.Intn(23), false
		if rng.Intn(2) == 0 {
			mbType, intra = rng.Intn(25), true
		}
		return mixedAction{
			func(e *Encoder) { e.MBTypeB(inc, mbType, intra) },
			func(d *Decoder, t *testing.T) {
				got, gotIntra := d.MBTypeB(inc)
				if got != mbType || gotIntra != intra {
					t.Fatalf("MBTypeB(%d) = %d intra %v, want %d intra %v", inc, got, gotIntra, mbType, intra)
				}
			},
		}
	case 3:
		sub := rng.Intn(4)
		return mixedAction{
			func(e *Encoder) { e.SubMBTypeP(sub) },
			func(d *Decoder, t *testing.T) {
				if got := d.SubMBTypeP(); got != sub {
					t.Fatalf("SubMBTypeP() = %d, want %d", got, sub)
				}
			},
		}
	case 4:
		sub := rng.Intn(13)
		return mixedAction{
			func(e *Encoder) { e.SubMBTypeB(sub) },
			func(d *Decoder, t *testing.T) {
				if got := d.SubMBTypeB(); got != sub {
					t.Fatalf("SubMBTypeB() = %d, want %d", got, sub)
				}
			},
		}
	case 5:
		inc, ref := rng.Intn(4), rng.Intn(32)
		return mixedAction{
			func(e *Encoder) { e.RefIdx(inc, ref) },
			func(d *Decoder, t *testing.T) {
				if got := d.RefIdx(inc); got != ref {
					t.Fatalf("RefIdx(%d) = %d, want %d", inc, got, ref)
				}
			},
		}
	case 6:
		base := MVDHorizontal
		if rng.Intn(2) == 0 {
			base = MVDVertical
		}
		absSum := rng.Intn(200)
		v := rng.Intn(4001) - 2000
		return mixedAction{
			func(e *Encoder) { e.MVD(base, absSum, v) },
			func(d *Decoder, t *testing.T) {
				if got := d.MVD(base, absSum); got != v {
					t.Fatalf("MVD(%d,%d) = %d, want %d", base, absSum, got, v)
				}
			},
		}
	case 7:
		inc, mode := rng.Intn(3), rng.Intn(4)
		return mixedAction{
			func(e *Encoder) { e.IntraChromaPredMode(inc, mode) },
			func(d *Decoder, t *testing.T) {
				if got := d.IntraChromaPredMode(inc); got != mode {
					t.Fatalf("IntraChromaPredMode(%d) = %d, want %d", inc, got, mode)
				}
			},
		}
	case 8:
		predMode, mode := rng.Intn(9), rng.Intn(9)
		return mixedAction{
			func(e *Encoder) { e.Intra4x4PredMode(predMode, mode) },
			func(d *Decoder, t *testing.T) {
				if got := d.Intra4x4PredMode(predMode); got != mode {
					t.Fatalf("Intra4x4PredMode(%d) = %d, want %d", predMode, got, mode)
				}
			},
		}
	case 9:
		left, top, cbp := rng.Intn(16), rng.Intn(16), rng.Intn(16)
		return mixedAction{
			func(e *Encoder) { e.CodedBlockPatternLuma(left, top, cbp) },
			func(d *Decoder, t *testing.T) {
				if got := d.CodedBlockPatternLuma(left, top); got != cbp {
					t.Fatalf("CodedBlockPatternLuma(%d,%d) = %d, want %d", left, top, got, cbp)
				}
			},
		}
	case 10:
		left, top, v := rng.Intn(48), rng.Intn(48), rng.Intn(3)
		return mixedAction{
			func(e *Encoder) { e.CodedBlockPatternChroma(left, top, v) },
			func(d *Decoder, t *testing.T) {
				if got := d.CodedBlockPatternChroma(left, top); got != v {
					t.Fatalf("CodedBlockPatternChroma(%d,%d) = %d, want %d", left, top, got, v)
				}
			},
		}
	case 11:
		delta, prevNonZero := rng.Intn(52)-26, rng.Intn(2) == 0
		return mixedAction{
			func(e *Encoder) { e.MBQPDelta(delta, prevNonZero) },
			func(d *Decoder, t *testing.T) {
				if got := d.MBQPDelta(prevNonZero); got != delta {
					t.Fatalf("MBQPDelta(%v) = %d, want %d", prevNonZero, got, delta)
				}
			},
		}
	case 12:
		inc, skip := rng.Intn(3), rng.Intn(2) == 0
		return mixedAction{
			func(e *Encoder) { e.MBSkipFlagP(inc, skip) },
			func(d *Decoder, t *testing.T) {
				if got := d.MBSkipFlagP(inc); got != skip {
					t.Fatalf("MBSkipFlagP(%d) = %v, want %v", inc, got, skip)
				}
			},
		}
	case 13:
		inc, skip := rng.Intn(3), rng.Intn(2) == 0
		return mixedAction{
			func(e *Encoder) { e.MBSkipFlagB(inc, skip) },
			func(d *Decoder, t *testing.T) {
				if got := d.MBSkipFlagB(inc); got != skip {
					t.Fatalf("MBSkipFlagB(%d) = %v, want %v", inc, got, skip)
				}
			},
		}
	default:
		cats := []int{CatIntra16x16DC, CatIntra16x16AC, CatLuma4x4, CatChromaDC, CatChromaAC}
		cat := cats[rng.Intn(len(cats))]
		n := residualBlockLength[cat]
		coeffs := make([]int32, n)
		for j := range coeffs {
			if rng.Intn(2) == 0 {
				continue
			}
			v := int32(1 + rng.Intn(2000))
			if rng.Intn(2) == 0 {
				v = -v
			}
			coeffs[j] = v
		}
		condA, condB := rng.Intn(2), rng.Intn(2)
		want := append([]int32(nil), coeffs...)
		return mixedAction{
			func(e *Encoder) { e.ResidualBlock(coeffs, cat, condA, condB, 1) },
			func(d *Decoder, t *testing.T) {
				got := make([]int32, n)
				d.ResidualBlock(got, cat, condA, condB, 1)
				for i := range got {
					if got[i] != want[i] {
						t.Fatalf("mixed ResidualBlock cat %d coeff[%d] = %d, want %d", cat, i, got[i], want[i])
					}
				}
			},
		}
	}
}

func TestMixedSequenceRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	for trial := 0; trial < 6; trial++ {
		qp := rng.Intn(52)
		intra := rng.Intn(2) == 0
		initIDC := uint32(rng.Intn(3))

		n := 250 + rng.Intn(250)
		actions := make([]mixedAction, n)
		for i := range actions {
			actions[i] = randomMixedAction(rng)
		}

		w := bits.NewWriterSize(1 << 16)
		var enc Encoder
		if err := enc.Init(w, qp, intra, initIDC); err != nil {
			t.Fatalf("trial %d: Encoder.Init: %v", trial, err)
		}
		for _, a := range actions {
			a.encode(&enc)
		}
		enc.EncodeTerminate(1)
		enc.Finish()
		if err := w.Err(); err != nil {
			t.Fatalf("trial %d: writer error: %v", trial, err)
		}

		var dec Decoder
		if err := dec.Init(bits.NewReader(w.Bytes()), qp, intra, initIDC); err != nil {
			t.Fatalf("trial %d: Decoder.Init: %v", trial, err)
		}
		for i, a := range actions {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("trial %d action %d: %v", trial, i, r)
					}
				}()
				a.decode(&dec, t)
			}()
		}
		if dec.DecodeTerminate() != 1 {
			t.Fatalf("trial %d: %d interleaved syntax elements desynchronised the context state", trial, n)
		}
	}
}

func TestEncoderInitWritesAlignmentOneBits(t *testing.T) {
	w := bits.NewWriterSize(64)
	w.WriteBits(0b101, 3)
	var enc Encoder
	if err := enc.Init(w, 26, true, 0); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !w.ByteAligned() {
		t.Fatal("Encoder.Init left the writer non-byte-aligned")
	}
	if w.BitsWritten() != 8 {
		t.Fatalf("BitsWritten() = %d, want 8 (3 header bits + 5 alignment one bits)", w.BitsWritten())
	}
	enc.EncodeDecision(100, 1)
	enc.EncodeTerminate(1)
	enc.Finish()

	r := bits.NewReader(w.Bytes())
	if _, err := r.ReadBits(3); err != nil {
		t.Fatalf("reading header bits: %v", err)
	}
	var dec Decoder
	if err := dec.Init(r, 26, true, 0); err != nil {
		t.Fatalf("Decoder.Init: %v (the alignment bits the encoder wrote were not accepted as cabac_alignment_one_bit)", err)
	}
	if got := dec.DecodeDecision(100); got != 1 {
		t.Fatalf("DecodeDecision(100) = %d, want 1", got)
	}
	if dec.DecodeTerminate() != 1 {
		t.Fatal("terminate did not decode back to 1")
	}
}

func TestEncoderInitRejectsBadInitIDC(t *testing.T) {
	w := bits.NewWriterSize(64)
	var enc Encoder
	if err := enc.Init(w, 26, false, 3); err != ErrInitIDC {
		t.Fatalf("Init with initIDC=3 returned %v, want ErrInitIDC", err)
	}
}

func TestSnapshotRestoreMatchesFreshEncodeOfSecondAttempt(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	for iter := 0; iter < 40; iter++ {
		qp := rng.Intn(52)
		intra := rng.Intn(2) == 0
		initIDC := uint32(rng.Intn(3))

		w := bits.NewWriterSize(512)
		var enc Encoder
		if err := enc.Init(w, qp, intra, initIDC); err != nil {
			t.Fatalf("iter %d: Init: %v", iter, err)
		}
		preamble := randomOps(rng, 5+rng.Intn(20))
		for _, o := range preamble {
			applyOp(&enc, o)
		}
		snap := enc.Snapshot()

		discarded := randomOps(rng, 10+rng.Intn(20))
		for _, o := range discarded {
			applyOp(&enc, o)
		}

		enc.Restore(snap)
		wSecond := bits.NewWriterSize(512)
		enc.w = wSecond
		attemptB := randomOps(rng, 10+rng.Intn(20))
		for _, o := range attemptB {
			applyOp(&enc, o)
		}
		enc.EncodeTerminate(1)
		enc.Finish()

		var fresh Encoder
		wFresh := bits.NewWriterSize(512)
		fresh.w = wFresh
		fresh.Restore(snap)
		for _, o := range attemptB {
			applyOp(&fresh, o)
		}
		fresh.EncodeTerminate(1)
		fresh.Finish()

		if !bytes.Equal(wSecond.Bytes(), wFresh.Bytes()) {
			t.Fatalf("iter %d: restoring after a discarded attempt did not reproduce the bits of a fresh encode of the second attempt", iter)
		}
	}
}

func TestEstimateResidualBlockBitsMatchesRealEncodedLength(t *testing.T) {
	rng := rand.New(rand.NewSource(20260831))
	cats := []int{CatIntra16x16DC, CatIntra16x16AC, CatLuma4x4, CatChromaDC, CatChromaAC}

	w := bits.NewWriterSize(1 << 16)
	var enc Encoder
	if err := enc.Init(w, 26, true, 0); err != nil {
		t.Fatalf("Init: %v", err)
	}
	preamble := randomOps(rng, 30)
	for _, o := range preamble {
		applyOp(&enc, o)
	}

	var maxDiff int
	for i := 0; i < 400; i++ {
		cat := cats[rng.Intn(len(cats))]
		n := residualBlockLength[cat]
		coeffs := make([]int32, n)
		for j := range coeffs {
			switch rng.Intn(3) {
			case 0:
			case 1:
				v := int32(1 + rng.Intn(3))
				if rng.Intn(2) == 0 {
					v = -v
				}
				coeffs[j] = v
			default:
				v := int32(1 + rng.Intn(6000))
				if rng.Intn(2) == 0 {
					v = -v
				}
				coeffs[j] = v
			}
		}
		condA, condB := rng.Intn(2), rng.Intn(2)

		estimate := enc.EstimateResidualBlockBits(coeffs, cat, condA, condB, 1)

		before := w.BitsWritten()
		beforeOutstand := enc.bitsOutstand
		enc.ResidualBlock(coeffs, cat, condA, condB, 1)
		actual := (w.BitsWritten() - before) + (enc.bitsOutstand - beforeOutstand)

		diff := estimate - actual
		if diff < 0 {
			diff = -diff
		}
		if diff > maxDiff {
			maxDiff = diff
		}
		if diff > 1 {
			t.Fatalf("block %d (cat %d): estimated %d bits, actually cost %d (diff %d)", i, cat, estimate, actual, diff)
		}
	}
	if maxDiff > 1 {
		t.Fatalf("worst-case estimator error was %d bits, want <= 1", maxDiff)
	}
}
