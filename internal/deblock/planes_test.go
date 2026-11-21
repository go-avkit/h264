package deblock

import (
	"bytes"
	"math/rand"
	"testing"
)

// Planes must filter exactly as the plain entry points do, on the route its
// proof selects.
func TestPlanesMatchPlain(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	const stride, off = 64, 64*8 + 8
	fill := func() []byte {
		p := make([]byte, stride*32)
		base := byte(rng.Intn(200))
		for i := range p {
			p[i] = base + byte(rng.Intn(24))
		}
		return p
	}
	for n := 0; n < 2000; n++ {
		strong := n&1 == 0
		var bS [4]uint8
		for g := range bS {
			if strong {
				bS[g] = 4
			} else {
				bS[g] = uint8(rng.Intn(4))
			}
		}
		ia, ib := rng.Intn(52), rng.Intn(52)
		ia2, ib2 := rng.Intn(52), rng.Intn(52)
		y, cb, cr := fill(), fill(), fill()
		type pic struct{ y, cb, cr []byte }
		clone := func() pic {
			return pic{append([]byte(nil), y...), append([]byte(nil), cb...), append([]byte(nil), cr...)}
		}
		check := func(name string, plain func(p pic), proved func(p *Planes)) {
			want, got := clone(), clone()
			plain(want)
			proved(NewPlanes(got.y, got.cb, got.cr, stride, stride))
			if !bytes.Equal(want.y, got.y) || !bytes.Equal(want.cb, got.cb) || !bytes.Equal(want.cr, got.cr) {
				t.Fatalf("%s: strong=%v bS=%v ia=%d ib=%d differ", name, strong, bS, ia, ib)
			}
		}
		check("lumaV",
			func(p pic) { FilterLumaEdgeVertical(p.y, stride, off, bS, ia, ib) },
			func(p *Planes) { p.LumaVertical(off, bS, strong, ia, ib) })
		check("lumaH",
			func(p pic) { FilterLumaEdgeHorizontal(p.y, stride, off, bS, ia, ib) },
			func(p *Planes) { p.LumaHorizontal(off, bS, strong, ia, ib) })
		check("chromaV",
			func(p pic) { FilterChromaPairVertical(p.cb, p.cr, stride, off, bS, ia, ib, ia2, ib2) },
			func(p *Planes) { p.ChromaVertical(off, bS, strong, ia, ib, ia2, ib2) })
		check("chromaH",
			func(p pic) { FilterChromaPairHorizontal(p.cb, p.cr, stride, off, bS, ia, ib, ia2, ib2) },
			func(p *Planes) { p.ChromaHorizontal(off, bS, strong, ia, ib, ia2, ib2) })
	}
}

// The two routes give the same bytes, so a test cannot tell them apart by
// output alone. It tells them apart by WHERE they write: the proved route
// writes the planes it proved, the checked route the slices the Planes holds
// for it. Both are replaced by decoys here so that the route taken shows.
func TestPlanesRouteWritesWhereTheProofSays(t *testing.T) {
	if !kernelsLF {
		t.Skip("no kernels: there is only the checked route")
	}
	const stride, off = 64, 64*8 + 8
	smooth := func(seed int) []byte {
		p := make([]byte, stride*32)
		for i := range p {
			p[i] = byte(100 + (i*7+seed)%5)
		}
		return p
	}
	bS := [4]uint8{3, 3, 3, 3}
	type route struct {
		name string
		call func(p *Planes, strong bool)
		luma bool
	}
	routes := []route{
		{"lumaV", func(p *Planes, s bool) { p.LumaVertical(off, bS, s, 51, 51) }, true},
		{"lumaH", func(p *Planes, s bool) { p.LumaHorizontal(off, bS, s, 51, 51) }, true},
		{"chromaV", func(p *Planes, s bool) { p.ChromaVertical(off, bS, s, 51, 51, 51, 51) }, false},
		{"chromaH", func(p *Planes, s bool) { p.ChromaHorizontal(off, bS, s, 51, 51, 51, 51) }, false},
	}
	for _, r := range routes {
		for _, strong := range []bool{false, true} {
			y, cb, cr := smooth(1), smooth(2), smooth(3)
			orig := [3][]byte{append([]byte(nil), y...), append([]byte(nil), cb...), append([]byte(nil), cr...)}
			// The reference result, from the plain entry points.
			ref := [3][]byte{append([]byte(nil), y...), append([]byte(nil), cb...), append([]byte(nil), cr...)}
			if strong {
				bS = [4]uint8{4, 4, 4, 4}
			} else {
				bS = [4]uint8{3, 3, 3, 3}
			}
			switch r.name {
			case "lumaV":
				FilterLumaEdgeVertical(ref[0], stride, off, bS, 51, 51)
			case "lumaH":
				FilterLumaEdgeHorizontal(ref[0], stride, off, bS, 51, 51)
			case "chromaV":
				FilterChromaPairVertical(ref[1], ref[2], stride, off, bS, 51, 51, 51, 51)
			case "chromaH":
				FilterChromaPairHorizontal(ref[1], ref[2], stride, off, bS, 51, 51, 51, 51)
			}
			touched := 0
			for i := range ref {
				if !bytes.Equal(ref[i], orig[i]) {
					touched++
				}
			}
			if touched == 0 {
				t.Fatalf("%s strong=%v: the plain filter changed nothing, the test would see no route", r.name, strong)
			}
			same := func(a, b []byte) bool { return bytes.Equal(a, b) }
			run := func(proved bool) (real, decoy [3][]byte) {
				real = [3][]byte{append([]byte(nil), orig[0]...), append([]byte(nil), orig[1]...), append([]byte(nil), orig[2]...)}
				decoy = [3][]byte{append([]byte(nil), orig[0]...), append([]byte(nil), orig[1]...), append([]byte(nil), orig[2]...)}
				p := NewPlanes(real[0], real[1], real[2], stride, stride)
				if l, c := p.Proved(); !l || !c {
					t.Fatalf("%s: planes of stride %d not proved (%v, %v)", r.name, stride, l, c)
				}
				p.y, p.cb, p.cr = decoy[0], decoy[1], decoy[2]
				if !proved {
					p.lumaOK, p.chromaOK = false, false
				}
				r.call(p, strong)
				return
			}
			real, decoy := run(true)
			for i := range ref {
				if !same(real[i], ref[i]) || !same(decoy[i], orig[i]) {
					t.Errorf("%s strong=%v: the proved route did not write the proved plane %d", r.name, strong, i)
				}
			}
			real, decoy = run(false)
			for i := range ref {
				if !same(decoy[i], ref[i]) || !same(real[i], orig[i]) {
					t.Errorf("%s strong=%v: the checked route did not write the held slice %d", r.name, strong, i)
				}
			}
		}
	}
}

// The proof is taken only where the kernels accept the planes: not at all
// without kernels, not for a stride below 16, not for planes that share bytes.
func TestPlanesProofSelectsTheRoute(t *testing.T) {
	buf := func(n int) []byte { return make([]byte, n) }
	cases := []struct {
		name          string
		y, cb, cr     []byte
		strideY, strC int
		luma, chroma  bool
	}{
		{"proved", buf(64 * 32), buf(64 * 32), buf(64 * 32), 64, 64, true, true},
		{"narrow luma", buf(8 * 32), buf(64 * 32), buf(64 * 32), 8, 64, false, true},
		{"narrow chroma", buf(64 * 32), buf(8 * 32), buf(8 * 32), 64, 8, true, false},
		{"unequal chroma", buf(64 * 32), buf(64 * 32), buf(64 * 16), 64, 64, true, false},
	}
	shared := buf(64 * 32)
	cases = append(cases, struct {
		name          string
		y, cb, cr     []byte
		strideY, strC int
		luma, chroma  bool
	}{"shared chroma", buf(64 * 32), shared, shared, 64, 64, true, false})
	for _, c := range cases {
		luma, chroma := NewPlanes(c.y, c.cb, c.cr, c.strideY, c.strC).Proved()
		if !kernelsLF {
			c.luma, c.chroma = false, false
		}
		if luma != c.luma || chroma != c.chroma {
			t.Errorf("%s: proved (%v, %v), want (%v, %v)", c.name, luma, chroma, c.luma, c.chroma)
		}
	}
}
