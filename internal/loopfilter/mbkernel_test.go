package loopfilter

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

// One call of the kernels per macroblock gives the same picture as the
// per-edge path, on grids of every kind of macroblock (intra, IPCM, 8x8
// transform, bi-predicted, slices, disabled filters, QP and offset ranges).
func TestMacroblockKernelMatchesPerEdgePath(t *testing.T) {
	saved := mbKernel
	defer func() { mbKernel = saved }()
	for _, size := range [][2]int{{20, 12}, {33, 9}, {7, 7}} {
		rng := rand.New(rand.NewSource(int64(size[0]*100 + size[1])))
		refs := []*frame.Picture{{POC: 1}, {POC: 2}, {POC: 3}}
		grid := randomGrid(rng, size[0], size[1], refs)
		at := func(mbx, mby int) *MB {
			if mbx < 0 || mby < 0 || mbx >= size[0] || mby >= size[1] {
				return nil
			}
			return &grid[mby*size[0]+mbx]
		}
		pic := smoothPicture(rng, size[0], size[1])
		want, got := clonePicture(pic), clonePicture(pic)
		for _, run := range []struct {
			pic    *frame.Picture
			kernel bool
		}{{want, false}, {got, true}} {
			mbKernel = run.kernel
			ctx := edgeContext{at: at, pic: run.pic}
			for mby := 0; mby < size[1]; mby++ {
				for mbx := 0; mbx < size[0]; mbx++ {
					ctx.filterMB(mbx, mby)
				}
			}
		}
		if bytes.Equal(want.Y, pic.Y) || bytes.Equal(want.Cb, pic.Cb) {
			t.Fatalf("%dx%d: the per-edge pass filtered nothing, so the comparison proves nothing", size[0], size[1])
		}
		if !bytes.Equal(got.Y, want.Y) || !bytes.Equal(got.Cb, want.Cb) || !bytes.Equal(got.Cr, want.Cr) {
			t.Fatalf("%dx%d: the kernel path differs from the per-edge path", size[0], size[1])
		}
	}
}
