package frame

import "testing"

func lumaVal(x, y int) byte { return byte(x*7 + y*11 + 3) }

func cbVal(x, y int) byte { return byte(x*13 + y*17 + 5) }

func crVal(x, y int) byte { return byte(x*19 + y*23 + 9) }

func fillPattern(p *Picture) {
	buf := make([]byte, p.Size())
	n := 0
	for y := 0; y < p.Height; y++ {
		for x := 0; x < p.Width; x++ {
			buf[n] = lumaVal(x, y)
			n++
		}
	}
	cw, ch := p.Width/2, p.Height/2
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			buf[n] = cbVal(x, y)
			n++
		}
	}
	for y := 0; y < ch; y++ {
		for x := 0; x < cw; x++ {
			buf[n] = crVal(x, y)
			n++
		}
	}
	p.CopyIn(buf)
}

func patternByte(i int) byte {
	v := uint32(i)*2654435761 + 12345
	return byte(v)
}

var mbDims = [][2]int{{1, 1}, {2, 3}, {11, 9}, {120, 68}}

func TestNewPictureDimensions(t *testing.T) {
	for _, d := range mbDims {
		wMBs, hMBs := d[0], d[1]
		p := NewPicture(wMBs, hMBs)

		wantW := wMBs * 16
		wantH := hMBs * 16
		if p.Width != wantW {
			t.Errorf("dims %v: Width = %d, want %d", d, p.Width, wantW)
		}
		if p.Height != wantH {
			t.Errorf("dims %v: Height = %d, want %d", d, p.Height, wantH)
		}
		if p.CropWidth != p.Width {
			t.Errorf("dims %v: CropWidth = %d, want %d", d, p.CropWidth, p.Width)
		}
		if p.CropHeight != p.Height {
			t.Errorf("dims %v: CropHeight = %d, want %d", d, p.CropHeight, p.Height)
		}

		wantStrideY := p.Width + 2*LumaMargin
		wantStrideC := p.Width/2 + 2*ChromaMargin
		if p.StrideY != wantStrideY {
			t.Errorf("dims %v: StrideY = %d, want %d", d, p.StrideY, wantStrideY)
		}
		if p.StrideC != wantStrideC {
			t.Errorf("dims %v: StrideC = %d, want %d", d, p.StrideC, wantStrideC)
		}

		lastY := p.LumaOffset(p.Width-1, p.Height-1) + LumaMargin*p.StrideY + LumaMargin
		if lastY != len(p.Y)-1 {
			t.Errorf("dims %v: last luma addressable index = %d, want %d (len=%d)", d, lastY, len(p.Y)-1, len(p.Y))
		}
		p.Y[lastY] = 1

		cw, ch := p.Width/2-1, p.Height/2-1
		lastCb := p.ChromaOffset(cw, ch) + ChromaMargin*p.StrideC + ChromaMargin
		if lastCb != len(p.Cb)-1 {
			t.Errorf("dims %v: last Cb addressable index = %d, want %d (len=%d)", d, lastCb, len(p.Cb)-1, len(p.Cb))
		}
		p.Cb[lastCb] = 1

		lastCr := p.ChromaOffset(cw, ch) + ChromaMargin*p.StrideC + ChromaMargin
		if lastCr != len(p.Cr)-1 {
			t.Errorf("dims %v: last Cr addressable index = %d, want %d (len=%d)", d, lastCr, len(p.Cr)-1, len(p.Cr))
		}
		p.Cr[lastCr] = 1
	}
}

func TestOffsets(t *testing.T) {
	p := NewPicture(3, 2)

	if got := p.LumaOffset(0, 0); got != p.OriginY() {
		t.Errorf("LumaOffset(0,0) = %d, want OriginY() = %d", got, p.OriginY())
	}
	if got := p.ChromaOffset(0, 0); got != p.OriginC() {
		t.Errorf("ChromaOffset(0,0) = %d, want OriginC() = %d", got, p.OriginC())
	}

	if d := p.LumaOffset(1, 0) - p.LumaOffset(0, 0); d != 1 {
		t.Errorf("LumaOffset x-step = %d, want 1", d)
	}
	if d := p.LumaOffset(0, 1) - p.LumaOffset(0, 0); d != p.StrideY {
		t.Errorf("LumaOffset y-step = %d, want StrideY = %d", d, p.StrideY)
	}

	if d := p.ChromaOffset(1, 0) - p.ChromaOffset(0, 0); d != 1 {
		t.Errorf("ChromaOffset x-step = %d, want 1", d)
	}
	if d := p.ChromaOffset(0, 1) - p.ChromaOffset(0, 0); d != p.StrideC {
		t.Errorf("ChromaOffset y-step = %d, want StrideC = %d", d, p.StrideC)
	}
}

func TestSize(t *testing.T) {
	for _, d := range mbDims {
		p := NewPicture(d[0], d[1])
		want := p.Width * p.Height * 3 / 2
		if got := p.Size(); got != want {
			t.Errorf("dims %v: Size() = %d, want %d", d, got, want)
		}
	}
}

func TestCopyInCopyOutRoundTrip(t *testing.T) {
	dims := [][2]int{{1, 1}, {2, 3}, {5, 7}, {11, 9}, {4, 1}, {1, 4}}
	for _, d := range dims {
		p := NewPicture(d[0], d[1])
		src := make([]byte, p.Size())
		for i := range src {
			src[i] = patternByte(i)
		}
		p.CopyIn(src)
		dst := make([]byte, p.Size())
		p.CopyOut(dst)
		for i := range src {
			if dst[i] != src[i] {
				t.Fatalf("dims %v: byte %d mismatch: got %d, want %d", d, i, dst[i], src[i])
			}
		}
	}
}

func TestCopyInLeavesMarginsUntouched(t *testing.T) {
	dims := [][2]int{{1, 1}, {2, 3}, {5, 7}, {11, 9}}
	const poison = 0xAA
	for _, d := range dims {
		p := NewPicture(d[0], d[1])
		for i := range p.Y {
			p.Y[i] = poison
		}
		for i := range p.Cb {
			p.Cb[i] = poison
			p.Cr[i] = poison
		}

		src := make([]byte, p.Size())
		for i := range src {
			src[i] = patternByte(i)
		}
		p.CopyIn(src)

		checkMarginPoison(t, d, "Y", p.Y, p.Width, p.Height, func(x, y int) int { return p.LumaOffset(x, y) })
		checkMarginPoison(t, d, "Cb", p.Cb, p.Width/2, p.Height/2, func(x, y int) int { return p.ChromaOffset(x, y) })
		checkMarginPoison(t, d, "Cr", p.Cr, p.Width/2, p.Height/2, func(x, y int) int { return p.ChromaOffset(x, y) })
	}
}

func checkMarginPoison(t *testing.T, d [2]int, name string, plane []byte, width, height int, offset func(x, y int) int) {
	t.Helper()
	visible := make([]bool, len(plane))
	for y := 0; y < height; y++ {
		base := offset(0, y)
		for x := 0; x < width; x++ {
			visible[base+x] = true
		}
	}
	for i, v := range plane {
		if !visible[i] && v != 0xAA {
			t.Fatalf("dims %v plane %s: margin byte at index %d = %d, want untouched poison 0xAA", d, name, i, v)
		}
	}
}

func TestExtendBorders(t *testing.T) {
	dims := [][2]int{{1, 1}, {2, 3}, {5, 4}, {11, 9}}
	for _, d := range dims {
		p := NewPicture(d[0], d[1])
		fillPattern(p)
		p.ExtendBorders()

		checkExtended(t, d, "Y", p.Y, p.Width, p.Height, LumaMargin, func(x, y int) int { return p.LumaOffset(x, y) }, lumaVal)
		checkExtended(t, d, "Cb", p.Cb, p.Width/2, p.Height/2, ChromaMargin, func(x, y int) int { return p.ChromaOffset(x, y) }, cbVal)
		checkExtended(t, d, "Cr", p.Cr, p.Width/2, p.Height/2, ChromaMargin, func(x, y int) int { return p.ChromaOffset(x, y) }, crVal)
	}
}

func checkExtended(t *testing.T, d [2]int, name string, plane []byte, width, height, margin int, offset func(x, y int) int, val func(x, y int) byte) {
	t.Helper()
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	for y := -margin; y < height+margin; y++ {
		for x := -margin; x < width+margin; x++ {
			cx := clamp(x, 0, width-1)
			cy := clamp(y, 0, height-1)
			want := val(cx, cy)
			got := plane[offset(x, y)]
			if got != want {
				t.Fatalf("dims %v plane %s: sample(%d,%d) = %d, want %d (clamped from (%d,%d))", d, name, x, y, got, want, cx, cy)
			}
		}
	}
}
