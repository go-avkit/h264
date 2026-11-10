//go:build amd64 && !purego

package simd

import (
	"math/rand"
	"testing"
)

type featureState struct {
	name   string
	sse41  bool
	avx2   bool
	skipIf func() bool
}

func fallbackPlane(rng *rand.Rand, n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(rng.Intn(256))
	}
	return b
}

func withFeatures(t *testing.T, s featureState, fn func()) {
	t.Helper()
	oldSSE41, oldAVX2 := hasSSE41, hasAVX2
	defer func() { hasSSE41, hasAVX2 = oldSSE41, oldAVX2 }()
	hasSSE41, hasAVX2 = s.sse41, s.avx2
	fn()
}

func featureStates() []featureState {
	return []featureState{
		{name: "no vector instructions at all", sse41: false, avx2: false},
		{name: "sse4.1 only", sse41: true, avx2: false},
		{name: "sse4.1 and avx2", sse41: true, avx2: true,
			skipIf: func() bool { return !hasAVX2 }},
	}
}

func TestEveryPathAgreesWhateverTheProcessorOffers(t *testing.T) {
	rng := rand.New(rand.NewSource(20260902))
	const stride = 64
	const rows = 64
	src := fallbackPlane(rng, stride*rows)
	ref := fallbackPlane(rng, stride*rows)

	sizes := [][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4}}
	chromaSizes := [][2]int{{8, 8}, {8, 4}, {4, 8}, {4, 4}, {2, 4}, {2, 2}}

	for _, state := range featureStates() {
		if state.skipIf != nil && state.skipIf() {
			t.Logf("%s: this processor cannot run that state, skipping", state.name)
			continue
		}
		t.Run(state.name, func(t *testing.T) {
			withFeatures(t, state, func() {
				for _, s := range sizes {
					w, h := s[0], s[1]
					off := 3*stride + 5
					got := SAD(src, stride, off, ref, stride, off, w, h)
					want := sadGeneric(src[off:], stride, ref[off:], stride, w, h)
					if got != want {
						t.Fatalf("SAD %dx%d gave %d, want %d", w, h, got, want)
					}
				}

				off := 4*stride + 4
				if got, want := SATD4x4(src, stride, off, ref, stride, off),
					satd4x4Generic(src[off:], stride, ref[off:], stride); got != want {
					t.Fatalf("SATD4x4 gave %d, want %d", got, want)
				}
				if got, want := SATD8x8(src, stride, off, ref, stride, off),
					satd8x8Generic(src[off:], stride, ref[off:], stride); got != want {
					t.Fatalf("SATD8x8 gave %d, want %d", got, want)
				}

				for _, s := range sizes {
					w, h := s[0], s[1]
					dstA := make([]byte, stride*rows)
					dstB := make([]byte, stride*rows)
					srcOff := 8*stride + 8
					SixTapHoriz(dstA, stride, 0, src, stride, srcOff, w, h)
					sixTapHorizGeneric(dstB, stride, 0, src, stride, srcOff, w, h)
					compareBytes(t, "SixTapHoriz", w, h, dstA, dstB)

					clear(dstA)
					clear(dstB)
					SixTapVert(dstA, stride, 0, src, stride, srcOff, w, h)
					sixTapVertGeneric(dstB, stride, 0, src, stride, srcOff, w, h)
					compareBytes(t, "SixTapVert", w, h, dstA, dstB)

					clear(dstA)
					clear(dstB)
					SixTapHV(dstA, stride, 0, src, stride, srcOff, w, h)
					sixTapHVGeneric(dstB, stride, 0, src, stride, srcOff, w, h)
					compareBytes(t, "SixTapHV", w, h, dstA, dstB)
				}

				for _, s := range chromaSizes {
					w, h := s[0], s[1]
					for _, frac := range [][2]int{{0, 0}, {3, 5}, {7, 7}, {1, 0}, {0, 6}} {
						dstA := make([]byte, stride*rows)
						dstB := make([]byte, stride*rows)
						srcOff := 4*stride + 4
						BilinearChroma(dstA, stride, 0, src, stride, srcOff, w, h, frac[0], frac[1])
						bilinearChromaGeneric(dstB, stride, 0, src, stride, srcOff, w, h, frac[0], frac[1])
						compareBytes(t, "BilinearChroma", w, h, dstA, dstB)
					}
				}

				var mf, scale [16]int32
				for i := range mf {
					mf[i] = int32(1 + rng.Intn(20000))
					scale[i] = int32(1 + rng.Intn(64))
				}
				for trial := 0; trial < 64; trial++ {
					var a, b [16]int32
					for i := range a {
						v := int32(rng.Intn(8001) - 4000)
						a[i], b[i] = v, v
					}
					Forward4x4(&a)
					forward4x4Generic(&b)
					compareBlocks(t, "Forward4x4", a, b)

					Inverse4x4(&a)
					inverse4x4Generic(&b)
					compareBlocks(t, "Inverse4x4", a, b)

					qbits := uint32(15 + rng.Intn(10))
					f := int32(1) << qbits / 3
					Quant4x4(&a, &mf, f, qbits)
					quant4x4Generic(&b, &mf, f, qbits)
					compareBlocks(t, "Quant4x4", a, b)

					shift := uint32(rng.Intn(8))
					Dequant4x4(&a, &scale, shift)
					dequant4x4Generic(&b, &scale, shift)
					compareBlocks(t, "Dequant4x4", a, b)

					planeA := fallbackPlane(rng, stride*8)
					planeB := append([]byte(nil), planeA...)
					AddResidual4x4(planeA, stride, stride+4, &a)
					addResidual4x4Generic(planeB, stride, stride+4, &b)
					for i := range planeA {
						if planeA[i] != planeB[i] {
							t.Fatalf("AddResidual4x4 differs at %d: %d against %d",
								i, planeA[i], planeB[i])
						}
					}
				}
			})
		})
	}
}

func compareBytes(t *testing.T, what string, w, h int, a, b []byte) {
	t.Helper()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("%s %dx%d differs at byte %d: %d against %d", what, w, h, i, a[i], b[i])
		}
	}
}

func compareBlocks(t *testing.T, what string, a, b [16]int32) {
	t.Helper()
	if a != b {
		t.Fatalf("%s differs:\n got %v\nwant %v", what, a, b)
	}
}

func TestAcceleratedReportsWhatIsActuallyUsed(t *testing.T) {
	oldSSE41, oldAVX2 := hasSSE41, hasAVX2
	defer func() { hasSSE41, hasAVX2 = oldSSE41, oldAVX2 }()

	hasSSE41, hasAVX2 = false, false
	if Accelerated() {
		t.Fatal("a processor without SSE4.1 falls back to Go for almost everything, but Accelerated says otherwise")
	}
	hasSSE41, hasAVX2 = true, false
	if !Accelerated() {
		t.Fatal("SSE4.1 is present and used, but Accelerated denies it")
	}
}
