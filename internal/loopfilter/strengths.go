package loopfilter

// mbStrengths holds the boundary strengths of one macroblock (8.7.2.1): for
// each direction (0 vertical edges, 1 horizontal) and each edge e, the strength
// of its four 4-sample groups, and in any[dir] a bit e set when some group of
// edge e is not zero. It is derived in one pass before any sample is filtered:
// strengths read the macroblock metadata only, never the samples.
type mbStrengths struct {
	bs  [2][4][4]uint8
	any [2]uint8
}

// skipV[e][u] and skipH[e][u] are, for the odd internal edge e of a macroblock
// whose 8x8 quadrants u (bit q = quadrant q carries one prediction) are
// uniform, the groups whose two blocks lie in one such quadrant: their motion
// cannot differ, so motionDiffers is not asked.
var skipV, skipH [4][16]uint8

func init() {
	for e := 1; e < 4; e += 2 {
		for u := 0; u < 16; u++ {
			for g := 0; g < 4; g++ {
				skipV[e][u] |= uint8(u>>rasterQuad[g*4+e]&1) << uint(g)
				skipH[e][u] |= uint8(u>>rasterQuad[e*4+g]&1) << uint(g)
			}
		}
	}
}

// column4 gathers bits 0, 4, 8 and 12 of x into bits 0 to 3.
func column4(x uint16) uint {
	return uint(x&1 | x>>3&2 | x>>6&4 | x>>9&8)
}

// group fills the four strengths of one non-intra edge between p and q: nz
// has bit g set when a block of group g carries coefficients, skip when its
// motion needs no comparison. pr and qr are the raster positions of group 0's
// blocks on each side and step moves to the next group.
func group(bs *[4]uint8, p, q *MB, nz, skip uint, pr, qr, step int) bool {
	var any uint
	for g := 0; g < 4; g++ {
		bit := uint(1) << uint(g)
		switch {
		case nz&bit != 0:
			bs[g] = 2
			any = 1
		case skip&bit != 0:
		case motionDiffers(p, q, rasterBlk[pr+g*step], rasterBlk[qr+g*step]):
			bs[g] = 1
			any = 1
		}
	}
	return any != 0
}

// compute derives every strength of cur. left and top are the neighbours whose
// edges are filtered (nil: none), cnz, lnz and tnz the nzMask of cur, left and
// top, uni the quadUniform of cur (zero when it is not known to help), and
// the edges visited are 0, step, 2*step, ... below last.
func (s *mbStrengths) compute(cur, left, top *MB, cnz, lnz, tnz uint16, uni uint8, last, step int) {
	if cur.Intra {
		for e := 0; e < last; e += step {
			v := uint8(3)
			if e == 0 {
				v = 4
			}
			for dir := 0; dir < 2; dir++ {
				if e == 0 && (dir == 0 && left == nil || dir == 1 && top == nil) {
					continue
				}
				s.bs[dir][e] = [4]uint8{v, v, v, v}
				s.any[dir] |= 1 << uint(e)
			}
		}
		return
	}
	for e := 0; e < last; e += step {
		var vnz, hnz uint
		var vskip, hskip uint
		vp, hp := cur, cur
		vpr, hpr := e-1, (e-1)*4
		switch e {
		case 0:
			vp, hp = left, top
			vpr, hpr = 3, 12
			if left != nil {
				vnz = column4(lnz>>3 | cnz)
			}
			if top != nil {
				hnz = uint((tnz>>12 | cnz) & 0xf)
			}
		default:
			vnz = column4(cnz>>uint(e-1) | cnz>>uint(e))
			hnz = uint((cnz>>uint(e*4-4) | cnz>>uint(e*4)) & 0xf)
			vskip, hskip = uint(skipV[e][uni]), uint(skipH[e][uni])
		}
		if e != 0 || left != nil {
			if vp.Intra {
				s.bs[0][e] = [4]uint8{4, 4, 4, 4}
				s.any[0] |= 1
			} else if group(&s.bs[0][e], vp, cur, vnz, vskip, vpr, e, 4) {
				s.any[0] |= 1 << uint(e)
			}
		}
		if e != 0 || top != nil {
			if hp.Intra {
				s.bs[1][e] = [4]uint8{4, 4, 4, 4}
				s.any[1] |= 1
			} else if group(&s.bs[1][e], hp, cur, hnz, hskip, hpr, e*4, 1) {
				s.any[1] |= 1 << uint(e)
			}
		}
	}
}
