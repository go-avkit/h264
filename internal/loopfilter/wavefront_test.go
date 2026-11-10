package loopfilter

import (
	"bytes"
	"math/rand"
	"runtime"
	"testing"

	"github.com/go-avkit/h264/internal/frame"
)

func randomGrid(rng *rand.Rand, widthMBs, heightMBs int, refs []*frame.Picture) []MB {
	grid := make([]MB, widthMBs*heightMBs)
	for i := range grid {
		m := &grid[i]
		m.Decoded = rng.Intn(50) != 0
		m.Intra = rng.Intn(4) == 0
		m.IPCM = m.Intra && rng.Intn(20) == 0
		m.QPY = 10 + rng.Intn(42)
		m.SliceID = 1 + (i/widthMBs)/7
		m.Transform8x8 = rng.Intn(3) == 0
		m.Bipredictive = rng.Intn(5) == 0
		m.ChromaQPOffset = [2]int{rng.Intn(5) - 2, rng.Intn(5) - 2}
		switch rng.Intn(10) {
		case 0:
			m.DisableDeblock = 1
		case 1:
			m.DisableDeblock = 2
		}
		m.AlphaOffset = 2 * (rng.Intn(13) - 6)
		m.BetaOffset = 2 * (rng.Intn(13) - 6)
		for b := 0; b < 16; b++ {
			m.NzY[b] = uint8(rng.Intn(3))
			m.MvL0[b] = [2]int16{int16(rng.Intn(9) - 4), int16(rng.Intn(9) - 4)}
			m.RefPicL0[b] = refs[rng.Intn(len(refs))]
			m.MvL1[b] = [2]int16{int16(rng.Intn(9) - 4), int16(rng.Intn(9) - 4)}
			m.RefPicL1[b] = refs[rng.Intn(len(refs))]
		}
	}
	return grid
}

func smoothPicture(rng *rand.Rand, widthMBs, heightMBs int) *frame.Picture {
	p := frame.NewPicture(widthMBs, heightMBs)
	for y := 0; y < heightMBs*16; y++ {
		for x := 0; x < widthMBs*16; x++ {
			v := 60 + (x/16*37+y/16*23)%120 + rng.Intn(9)
			p.Y[p.LumaOffset(x, y)] = byte(v)
		}
	}
	for y := 0; y < heightMBs*8; y++ {
		for x := 0; x < widthMBs*8; x++ {
			p.Cb[p.ChromaOffset(x, y)] = byte(90 + (x/8*11+y/8*7)%60 + rng.Intn(5))
			p.Cr[p.ChromaOffset(x, y)] = byte(140 - (x/8*5+y/8*13)%60 + rng.Intn(5))
		}
	}
	return p
}

func clonePicture(p *frame.Picture) *frame.Picture {
	q := *p
	q.Y = append([]byte(nil), p.Y...)
	q.Cb = append([]byte(nil), p.Cb...)
	q.Cr = append([]byte(nil), p.Cr...)
	return &q
}

func TestApplyAcrossRowsMatchesRasterOrder(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("one processor filters in raster order anyway")
	}
	for _, size := range [][2]int{{120, 68}, {215, 90}, {64, 17}} {
		rng := rand.New(rand.NewSource(int64(size[0] * size[1])))
		refs := []*frame.Picture{{POC: 1}, {POC: 2}, {POC: 3}}
		grid := randomGrid(rng, size[0], size[1], refs)
		at := func(mbx, mby int) *MB {
			if mbx < 0 || mby < 0 || mbx >= size[0] || mby >= size[1] {
				return nil
			}
			return &grid[mby*size[0]+mbx]
		}
		pic := smoothPicture(rng, size[0], size[1])
		want := clonePicture(pic)
		ctx := edgeContext{at: at, pic: want}
		for mby := 0; mby < size[1]; mby++ {
			for mbx := 0; mbx < size[0]; mbx++ {
				ctx.filterMB(mbx, mby)
			}
		}
		if bytes.Equal(want.Y, pic.Y) {
			t.Fatalf("%dx%d: the reference pass filtered nothing, so the comparison proves nothing", size[0], size[1])
		}
		for run := 0; run < 3; run++ {
			got := clonePicture(pic)
			Apply(got, size[0], size[1], at)
			if !bytes.Equal(got.Y, want.Y) || !bytes.Equal(got.Cb, want.Cb) || !bytes.Equal(got.Cr, want.Cr) {
				t.Fatalf("%dx%d, run %d: filtering rows in parallel changed the picture", size[0], size[1], run)
			}
		}
	}
}
