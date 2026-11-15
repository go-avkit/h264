package mc

import (
	"bytes"
	"math/rand"
	"testing"
)

// PredictChromaPair and PredictChromaPairAvg against PredictChroma and
// Average on each plane, for every size and fraction, both through the kernels
// and, with the destination sharing memory with the source (which the kernels
// refuse), through the portable path.
func TestPredictChromaPairMatchesPerPlane(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	const stride = 20
	// 6 and 3 wide, 6 and 3 high: sizes a merged run of motion produces, which
	// the kernels do not take
	for _, sz := range [][2]int{{8, 8}, {8, 4}, {8, 2}, {2, 8}, {4, 8}, {4, 4}, {4, 2}, {2, 2}, {4, 6}, {6, 8}, {6, 6}, {8, 6}, {2, 6}, {3, 4}} {
		w, h := sz[0], sz[1]
		// vectors with whole-sample parts (0, 8, -8: fractions of zero) and every
		// other fraction among them
		for _, mvx := range []int{-9, -8, -3, 0, 5, 8} {
			for _, mvy := range []int{-9, -4, 0, 1, 8} {
				srcCb, srcCr := make([]byte, 400), make([]byte, 400)
				for i := range srcCb {
					srcCb[i], srcCr[i] = byte(r.Intn(256)), byte(r.Intn(256))
				}
				srcOff := 3*stride + 3
				for _, shared := range []bool{false, true} {
					for _, avg := range []bool{false, true} {
						gotCb, gotCr := make([]byte, 8*stride), make([]byte, 8*stride)
						for i := range gotCb {
							gotCb[i], gotCr[i] = byte(r.Intn(256)), byte(r.Intn(256))
						}
						wantCb, wantCr := append([]byte(nil), gotCb...), append([]byte(nil), gotCr...)
						sCb, sCr := srcCb, srcCr
						if shared {
							// the destination planes are the source planes' own storage
							copy(gotCb, srcCb[:len(gotCb)])
							copy(gotCr, srcCr[:len(gotCr)])
						}
						var tCb, tCr [64]byte
						PredictChroma(tCb[:w*h], w, 0, sCb, stride, srcOff, w, h, mvx, mvy)
						PredictChroma(tCr[:w*h], w, 0, sCr, stride, srcOff, w, h, mvx, mvy)
						if shared {
							copy(wantCb, gotCb)
							copy(wantCr, gotCr)
						}
						for y := 0; y < h; y++ {
							for x := 0; x < w; x++ {
								i := 2 + y*stride + x
								if avg {
									wantCb[i] = byte((int(wantCb[i]) + int(tCb[y*w+x]) + 1) >> 1)
									wantCr[i] = byte((int(wantCr[i]) + int(tCr[y*w+x]) + 1) >> 1)
								} else {
									wantCb[i], wantCr[i] = tCb[y*w+x], tCr[y*w+x]
								}
							}
						}
						if shared {
							// the destination is a window of the source slice itself
							d0, d1 := srcCb, srcCr
							saved0, saved1 := append([]byte(nil), srcCb...), append([]byte(nil), srcCr...)
							copy(d0, gotCb)
							copy(d1, gotCr)
							if avg {
								PredictChromaPairAvg(d0, d1, stride, 2, sCb, sCr, stride, srcOff, w, h, mvx, mvy)
							} else {
								PredictChromaPair(d0, d1, stride, 2, sCb, sCr, stride, srcOff, w, h, mvx, mvy)
							}
							for y := 0; y < h; y++ {
								for x := 0; x < w; x++ {
									i := 2 + y*stride + x
									if d0[i] != wantCb[i] || d1[i] != wantCr[i] {
										t.Fatalf("shared avg=%v %dx%d mv (%d,%d): sample (%d,%d) differs", avg, w, h, mvx, mvy, x, y)
									}
								}
							}
							copy(srcCb, saved0)
							copy(srcCr, saved1)
							continue
						}
						if avg {
							PredictChromaPairAvg(gotCb, gotCr, stride, 2, sCb, sCr, stride, srcOff, w, h, mvx, mvy)
						} else {
							PredictChromaPair(gotCb, gotCr, stride, 2, sCb, sCr, stride, srcOff, w, h, mvx, mvy)
						}
						if !bytes.Equal(gotCb, wantCb) || !bytes.Equal(gotCr, wantCr) {
							t.Fatalf("avg=%v %dx%d mv (%d,%d): result differs", avg, w, h, mvx, mvy)
						}
					}
				}
			}
		}
	}
}
