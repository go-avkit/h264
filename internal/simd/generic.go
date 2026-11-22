package simd

import "encoding/binary"

func spanFits(b []byte, stride, w, h int) bool {
	if h == 0 {
		return true
	}
	return len(b) >= (h-1)*stride+w
}

func addResidualFits(plane []byte, stride, offset int) bool {
	if offset < 0 {
		return false
	}
	return len(plane)-offset >= 3*stride+4
}

func forward4x4Generic(b *[16]int32) {
	for i := 0; i < 16; i += 4 {
		s0, s1, s2, s3 := b[i], b[i+1], b[i+2], b[i+3]
		t0 := s0 + s3
		t1 := s1 + s2
		t2 := s1 - s2
		t3 := s0 - s3
		b[i] = t0 + t1
		b[i+1] = t3*2 + t2
		b[i+2] = t0 - t1
		b[i+3] = t3 - t2*2
	}
	for i := 0; i < 4; i++ {
		s0, s1, s2, s3 := b[i], b[i+4], b[i+8], b[i+12]
		t0 := s0 + s3
		t1 := s1 + s2
		t2 := s1 - s2
		t3 := s0 - s3
		b[i] = t0 + t1
		b[i+4] = t3*2 + t2
		b[i+8] = t0 - t1
		b[i+12] = t3 - t2*2
	}
}

func inverse4x4Generic(b *[16]int32) {
	for i := 0; i < 16; i += 4 {
		s0, s1, s2, s3 := b[i], b[i+1], b[i+2], b[i+3]
		t0 := s0 + s2
		t1 := s0 - s2
		t2 := s1>>1 - s3
		t3 := s1 + s3>>1
		b[i] = t0 + t3
		b[i+1] = t1 + t2
		b[i+2] = t1 - t2
		b[i+3] = t0 - t3
	}
	for i := 0; i < 4; i++ {
		s0, s1, s2, s3 := b[i], b[i+4], b[i+8], b[i+12]
		t0 := s0 + s2
		t1 := s0 - s2
		t2 := s1>>1 - s3
		t3 := s1 + s3>>1
		b[i] = (t0 + t3 + 32) >> 6
		b[i+4] = (t1 + t2 + 32) >> 6
		b[i+8] = (t1 - t2 + 32) >> 6
		b[i+12] = (t0 - t3 + 32) >> 6
	}
}

func quant4x4Generic(b *[16]int32, mf *[16]int32, f int32, qbits uint32) {
	qb := uint(qbits)
	for i := range b {
		v := b[i]
		if v >= 0 {
			b[i] = (v*mf[i] + f) >> qb
		} else {
			b[i] = -((-v*mf[i] + f) >> qb)
		}
	}
}

func dequant4x4Generic(b *[16]int32, scale *[16]int32, shift uint32) {
	if shift >= 4 {
		s := uint(shift - 4)
		for i := range b {
			b[i] = b[i] * scale[i] << s
		}
		return
	}
	s := uint(4 - shift)
	round := int32(1) << (s - 1)
	for i := range b {
		b[i] = (b[i]*scale[i] + round) >> s
	}
}

func clip1(v int32) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func addResidual4x4Generic(plane []byte, stride, offset int, b *[16]int32) {
	for y := 0; y < 4; y++ {
		row := plane[offset+y*stride:]
		row[0] = clip1(int32(row[0]) + b[y*4])
		row[1] = clip1(int32(row[1]) + b[y*4+1])
		row[2] = clip1(int32(row[2]) + b[y*4+2])
		row[3] = clip1(int32(row[3]) + b[y*4+3])
	}
}

func spanFitsMargin(b []byte, stride, off, w, h, left, right, top, bottom int) bool {
	minOff := off - left - top*stride
	maxOff := off + w - 1 + right + (h-1+bottom)*stride
	return minOff >= 0 && maxOff < len(b)
}

func hFilter1(src []byte, off int) int {
	return int(src[off-2]) -
		5*int(src[off-1]) +
		20*int(src[off]) +
		20*int(src[off+1]) -
		5*int(src[off+2]) +
		int(src[off+3])
}

func vFilter1(src []byte, stride, off int) int {
	return int(src[off-2*stride]) -
		5*int(src[off-stride]) +
		20*int(src[off]) +
		20*int(src[off+stride]) -
		5*int(src[off+2*stride]) +
		int(src[off+3*stride])
}

func mcClip1(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func sixTapHorizGeneric(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	for y := 0; y < h; y++ {
		so := srcOff + y*srcStride
		do := dstOff + y*dstStride
		for x := 0; x < w; x++ {
			dst[do+x] = mcClip1((hFilter1(src, so+x) + 16) >> 5)
		}
	}
}

func sixTapVertGeneric(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	for y := 0; y < h; y++ {
		so := srcOff + y*srcStride
		do := dstOff + y*dstStride
		for x := 0; x < w; x++ {
			dst[do+x] = mcClip1((vFilter1(src, srcStride, so+x) + 16) >> 5)
		}
	}
}

// hvRows is the most rows of intermediates the centre position needs for a
// block of the largest height H.264 predicts (16 + 5).
const hvRows = 16 + 5

// sixTapHVGeneric computes the centre half sample j of 8.4.2.2.1: the
// horizontal six-tap filter is applied once to each of the h+5 rows the
// vertical filter reaches (rows -2 .. h+2, unclipped, the b1 values of
// equation 8-241), and the vertical six-tap filter runs over those
// intermediates (equation 8-246). Each intermediate is
// shared by the six outputs that need it, instead of being recomputed.
func sixTapHVGeneric(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	var stack [16 * hvRows]int32
	tmp := stack[:]
	n := w * (h + 5)
	if n > len(tmp) {
		tmp = make([]int32, n)
	}
	tmp = tmp[:n]
	for r := 0; r < h+5; r++ {
		so := srcOff + (r-2)*srcStride
		row := tmp[r*w : r*w+w]
		for x := range row {
			row[x] = int32(hFilter1(src, so+x))
		}
	}
	for y := 0; y < h; y++ {
		do := dstOff + y*dstStride
		out := dst[do : do+w]
		r0 := tmp[y*w : y*w+w]
		r1 := tmp[(y+1)*w : (y+1)*w+w]
		r2 := tmp[(y+2)*w : (y+2)*w+w]
		r3 := tmp[(y+3)*w : (y+3)*w+w]
		r4 := tmp[(y+4)*w : (y+4)*w+w]
		r5 := tmp[(y+5)*w : (y+5)*w+w]
		for x := range out {
			j1 := int(r0[x]) - 5*int(r1[x]) + 20*int(r2[x]) + 20*int(r3[x]) - 5*int(r4[x]) + int(r5[x])
			out[x] = mcClip1((j1 + 512) >> 10)
		}
	}
}

func bilinearChromaGeneric(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac, yFrac int) {
	for y := 0; y < h; y++ {
		so := srcOff + y*srcStride
		do := dstOff + y*dstStride
		for x := 0; x < w; x++ {
			p := so + x
			a := int(src[p])
			b := int(src[p+1])
			c := int(src[p+srcStride])
			d := int(src[p+srcStride+1])
			val := ((8-xFrac)*(8-yFrac)*a + xFrac*(8-yFrac)*b + (8-xFrac)*yFrac*c + xFrac*yFrac*d + 32) >> 6
			dst[do+x] = byte(val)
		}
	}
}

func SixTapHoriz(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	sixTapHorizDispatch(dst, dstStride, dstOff, src, srcStride, srcOff, w, h)
}

func SixTapVert(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	sixTapVertDispatch(dst, dstStride, dstOff, src, srcStride, srcOff, w, h)
}

func SixTapHV(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	sixTapHVDispatch(dst, dstStride, dstOff, src, srcStride, srcOff, w, h)
}

func BilinearChroma(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac, yFrac int) {
	bilinearChromaDispatch(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, xFrac, yFrac)
}

func deblockFits(plane []byte, offset, stride, before, after int) bool {
	if stride <= 0 || offset < before*stride {
		return false
	}
	return len(plane)-offset >= after*stride+16
}

func avgBytesGeneric(dst []byte, dstStride, dstOff int, a []byte, aStride, aOff int, b []byte, bStride, bOff, w, h int) {
	for y := 0; y < h; y++ {
		do := dstOff + y*dstStride
		ao := aOff + y*aStride
		bo := bOff + y*bStride
		out := dst[do : do+w]
		ra := a[ao : ao+w]
		rb := b[bo : bo+w]
		x := 0
		for ; x+8 <= w; x += 8 {
			binary.LittleEndian.PutUint64(out[x:], avg8(binary.LittleEndian.Uint64(ra[x:]), binary.LittleEndian.Uint64(rb[x:])))
		}
		for ; x < w; x++ {
			out[x] = byte((int(ra[x]) + int(rb[x]) + 1) >> 1)
		}
	}
}

// avg8 is (a + b + 1) >> 1 in each of the eight byte lanes of a and b:
// (a + b + 1) >> 1 = (a | b) - ((a ^ b) >> 1), and the lane-wise (a ^ b) >> 1
// is the whole-word shift with the bit that crossed into the lane masked off.
// A lane of a | b is never below the same lane of the term subtracted, so
// nothing borrows between lanes.
func avg8(a, b uint64) uint64 {
	return (a | b) - ((a^b)>>1)&0x7f7f7f7f7f7f7f7f
}

func AvgBytes(dst []byte, dstStride, dstOff int, a []byte, aStride, aOff int, b []byte, bStride, bOff, w, h int) {
	avgBytesDispatch(dst, dstStride, dstOff, a, aStride, aOff, b, bStride, bOff, w, h)
}

func copyBlockGeneric(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	for y := 0; y < h; y++ {
		do := dstOff + y*dstStride
		so := srcOff + y*srcStride
		// The widths H.264 predicts are 16, 8 and 4 (and 2 for chroma): a
		// fixed-size array move is a few wide loads and stores, where copy
		// is a call to memmove for every row.
		switch w {
		case 16:
			*(*[16]byte)(dst[do : do+16]) = [16]byte(src[so : so+16])
		case 8:
			*(*[8]byte)(dst[do : do+8]) = [8]byte(src[so : so+8])
		case 4:
			*(*[4]byte)(dst[do : do+4]) = [4]byte(src[so : so+4])
		default:
			copy(dst[do:do+w], src[so:so+w])
		}
	}
}

func CopyBlock(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h int) {
	copyBlockDispatch(dst, dstStride, dstOff, src, srcStride, srcOff, w, h)
}
