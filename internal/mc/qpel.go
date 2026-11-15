package mc

// Fused quarter-sample luma prediction (8.4.2.2.1, equations 8-250 to 8-261).
//
// Every quarter position is the rounding average (x + y + 1) >> 1 of two
// full or half samples. The two-pass way is to write each term as a block and
// average the blocks; here the two terms of a sample are computed together and
// averaged before the one store, so no intermediate block is written or read
// back. The arithmetic is the equations', term by term.

func tap6(a, b, c, d, e, f int) int {
	return a - 5*b + 20*c + 20*d - 5*e + f
}

func halfClip(v int) int { return clip1((v + 16) >> 5) }

// qpelStack is the room for the intermediates of the largest block that H.264
// predicts: (16 + 5) x 16 values.
const qpelStack = 21 * 16

func qpelScratch(stack *[qpelStack]int32, n int) []int32 {
	if n > len(stack) {
		return make([]int32, n)
	}
	return stack[:n]
}

// predictQuarter writes the quarter-sample prediction for (xFrac, yFrac), at
// least one of which is odd.
func predictQuarter(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac, yFrac int) {
	switch {
	case yFrac == 0:
		quarterH(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, xFrac)
	case xFrac == 0:
		quarterV(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, yFrac)
	case xFrac == 2:
		quarterHJ(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, yFrac)
	case yFrac == 2:
		quarterVJ(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, xFrac)
	default:
		quarterDiagonal(dst, dstStride, dstOff, src, srcStride, srcOff, w, h, xFrac, yFrac)
	}
}

// quarterH: a = (G + b + 1) >> 1 and c = (H + b + 1) >> 1 (8-250, 8-251).
func quarterH(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac int) {
	full := 2
	if xFrac == 3 {
		full = 3
	}
	for y := 0; y < h; y++ {
		s := src[srcOff+y*srcStride-2 : srcOff+y*srcStride+w+3]
		out := dst[dstOff+y*dstStride : dstOff+y*dstStride+w]
		for x := range out {
			b := halfClip(tap6(int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4]), int(s[x+5])))
			out[x] = byte((int(s[x+full]) + b + 1) >> 1)
		}
	}
}

// quarterV: d = (G + h + 1) >> 1 and n = (M + h + 1) >> 1 (8-252, 8-253).
func quarterV(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, yFrac int) {
	fullRow := 2
	if yFrac == 3 {
		fullRow = 3
	}
	for y := 0; y < h; y++ {
		o := srcOff + y*srcStride - 2*srcStride
		r0 := src[o : o+w]
		r1 := src[o+srcStride : o+srcStride+w]
		r2 := src[o+2*srcStride : o+2*srcStride+w]
		r3 := src[o+3*srcStride : o+3*srcStride+w]
		r4 := src[o+4*srcStride : o+4*srcStride+w]
		r5 := src[o+5*srcStride : o+5*srcStride+w]
		full := src[o+fullRow*srcStride : o+fullRow*srcStride+w]
		out := dst[dstOff+y*dstStride : dstOff+y*dstStride+w]
		for x := range out {
			hv := halfClip(tap6(int(r0[x]), int(r1[x]), int(r2[x]), int(r3[x]), int(r4[x]), int(r5[x])))
			out[x] = byte((int(full[x]) + hv + 1) >> 1)
		}
	}
}

// quarterHJ: f = (b + j + 1) >> 1 and q = (j + s + 1) >> 1 (8-254, 8-257). The
// b1 / s1 intermediates (8-241) of the h+5 rows are filtered once; j1 is the
// vertical filter over them (8-245) and b or s is read from the same rows.
func quarterHJ(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, yFrac int) {
	var stack [qpelStack]int32
	tmp := qpelScratch(&stack, w*(h+5))
	for r := 0; r < h+5; r++ {
		s := src[srcOff+(r-2)*srcStride-2 : srcOff+(r-2)*srcStride+w+3]
		row := tmp[r*w : r*w+w]
		for x := range row {
			row[x] = int32(tap6(int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4]), int(s[x+5])))
		}
	}
	half := 2 // row of b1 for the sample's own row
	if yFrac == 3 {
		half = 3 // s1 is one row down
	}
	for y := 0; y < h; y++ {
		r0 := tmp[y*w : y*w+w]
		r1 := tmp[(y+1)*w : (y+1)*w+w]
		r2 := tmp[(y+2)*w : (y+2)*w+w]
		r3 := tmp[(y+3)*w : (y+3)*w+w]
		r4 := tmp[(y+4)*w : (y+4)*w+w]
		r5 := tmp[(y+5)*w : (y+5)*w+w]
		hr := tmp[(y+half)*w : (y+half)*w+w]
		out := dst[dstOff+y*dstStride : dstOff+y*dstStride+w]
		for x := range out {
			j := clip1((tap6(int(r0[x]), int(r1[x]), int(r2[x]), int(r3[x]), int(r4[x]), int(r5[x])) + 512) >> 10)
			b := halfClip(int(hr[x]))
			out[x] = byte((b + j + 1) >> 1)
		}
	}
}

// quarterVJ: i = (h + j + 1) >> 1 and k = (j + m + 1) >> 1 (8-255, 8-256). The
// h1 / m1 intermediates (8-242) of the w+5 columns are filtered once; j1 is the
// horizontal filter over them (8-246, equal to 8-245).
func quarterVJ(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac int) {
	var stack [qpelStack]int32
	cols := w + 5
	tmp := qpelScratch(&stack, cols*h)
	for y := 0; y < h; y++ {
		o := srcOff + y*srcStride - 2
		o0 := o - 2*srcStride
		r0 := src[o0 : o0+cols]
		r1 := src[o0+srcStride : o0+srcStride+cols]
		r2 := src[o0+2*srcStride : o0+2*srcStride+cols]
		r3 := src[o0+3*srcStride : o0+3*srcStride+cols]
		r4 := src[o0+4*srcStride : o0+4*srcStride+cols]
		r5 := src[o0+5*srcStride : o0+5*srcStride+cols]
		row := tmp[y*cols : y*cols+cols]
		for c := range row {
			row[c] = int32(tap6(int(r0[c]), int(r1[c]), int(r2[c]), int(r3[c]), int(r4[c]), int(r5[c])))
		}
	}
	half := 2
	if xFrac == 3 {
		half = 3 // m1 is one column right
	}
	for y := 0; y < h; y++ {
		t := tmp[y*cols : y*cols+cols]
		out := dst[dstOff+y*dstStride : dstOff+y*dstStride+w]
		for x := range out {
			j := clip1((tap6(int(t[x]), int(t[x+1]), int(t[x+2]), int(t[x+3]), int(t[x+4]), int(t[x+5])) + 512) >> 10)
			hv := halfClip(int(t[x+half]))
			out[x] = byte((hv + j + 1) >> 1)
		}
	}
}

// quarterDiagonal: e = (b + h + 1) >> 1, g = (b + m + 1) >> 1,
// p = (h + s + 1) >> 1, r = (m + s + 1) >> 1 (8-258 to 8-261). b or s is the
// horizontal half sample of the row (yFrac 1) or the row below (yFrac 3); h or m
// is the vertical half sample of the column (xFrac 1) or the column right of it
// (xFrac 3).
func quarterDiagonal(dst []byte, dstStride, dstOff int, src []byte, srcStride, srcOff, w, h, xFrac, yFrac int) {
	hOff := 0
	if yFrac == 3 {
		hOff = srcStride
	}
	vOff := 0
	if xFrac == 3 {
		vOff = 1
	}
	for y := 0; y < h; y++ {
		so := srcOff + y*srcStride
		s := src[so+hOff-2 : so+hOff+w+3]
		o := so + vOff - 2*srcStride
		r0 := src[o : o+w]
		r1 := src[o+srcStride : o+srcStride+w]
		r2 := src[o+2*srcStride : o+2*srcStride+w]
		r3 := src[o+3*srcStride : o+3*srcStride+w]
		r4 := src[o+4*srcStride : o+4*srcStride+w]
		r5 := src[o+5*srcStride : o+5*srcStride+w]
		out := dst[dstOff+y*dstStride : dstOff+y*dstStride+w]
		for x := range out {
			b := halfClip(tap6(int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4]), int(s[x+5])))
			hv := halfClip(tap6(int(r0[x]), int(r1[x]), int(r2[x]), int(r3[x]), int(r4[x]), int(r5[x])))
			out[x] = byte((b + hv + 1) >> 1)
		}
	}
}
