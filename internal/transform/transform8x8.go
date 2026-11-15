package transform

type Block8x8 [64]int32

func dct8x1D(src [8]int32) [8]int32 {
	s07 := src[0] + src[7]
	s16 := src[1] + src[6]
	s25 := src[2] + src[5]
	s34 := src[3] + src[4]
	a0 := s07 + s34
	a1 := s16 + s25
	a2 := s07 - s34
	a3 := s16 - s25
	d07 := src[0] - src[7]
	d16 := src[1] - src[6]
	d25 := src[2] - src[5]
	d34 := src[3] - src[4]
	a4 := d16 + d25 + (d07 + (d07 >> 1))
	a5 := d07 - d34 - (d25 + (d25 >> 1))
	a6 := d07 + d34 - (d16 + (d16 >> 1))
	a7 := d16 - d25 + (d34 + (d34 >> 1))
	var out [8]int32
	out[0] = a0 + a1
	out[1] = a4 + (a7 >> 2)
	out[2] = a2 + (a3 >> 1)
	out[3] = a5 + (a6 >> 2)
	out[4] = a0 - a1
	out[5] = a6 - (a5 >> 2)
	out[6] = (a2 >> 1) - a3
	out[7] = (a4 >> 2) - a7
	return out
}

// idct8 is the one-dimensional inverse transform of 8.5.13 on eight values (the
// same equations for the rows and for the columns), taking and returning the
// values in registers: passing them as [8]int32 copied the array in and out of
// every call and was the larger part of its cost.
func idct8(s0, s1, s2, s3, s4, s5, s6, s7 int32) (o0, o1, o2, o3, o4, o5, o6, o7 int32) {
	a0 := s0 + s4
	a2 := s0 - s4
	a4 := (s2 >> 1) - s6
	a6 := (s6 >> 1) + s2
	b0 := a0 + a6
	b2 := a2 + a4
	b4 := a2 - a4
	b6 := a0 - a6
	a1 := -s3 + s5 - s7 - (s7 >> 1)
	a3 := s1 + s7 - s3 - (s3 >> 1)
	a5 := -s1 + s7 + s5 + (s5 >> 1)
	a7 := s3 + s5 + s1 + (s1 >> 1)
	b1 := (a7 >> 2) + a1
	b3 := a3 + (a5 >> 2)
	b5 := (a3 >> 2) - a5
	b7 := a7 - (a1 >> 2)
	return b0 + b7, b2 + b5, b4 + b3, b6 + b1, b6 - b1, b4 - b3, b2 - b5, b0 - b7
}

func Forward8x8(b *Block8x8) {
	var tmp Block8x8
	for row := 0; row < 8; row++ {
		var src [8]int32
		for col := 0; col < 8; col++ {
			src[col] = b[row*8+col]
		}
		out := dct8x1D(src)
		for col := 0; col < 8; col++ {
			tmp[row*8+col] = out[col]
		}
	}
	for col := 0; col < 8; col++ {
		var src [8]int32
		for row := 0; row < 8; row++ {
			src[row] = tmp[row*8+col]
		}
		out := dct8x1D(src)
		for row := 0; row < 8; row++ {
			b[row*8+col] = out[row]
		}
	}
}

// Inverse8x8 is the inverse 8x8 transform of 8.5.13 with the final
// (x + 32) >> 6 (Equation 8-388). Two exact shortcuts for the sparse blocks that
// dominate real streams:
//
//   - only the DC coefficient is non-zero: every butterfly of the row pass
//     returns d00 and every one of the column pass again, so each of the 64
//     samples is (d00 + 32) >> 6;
//   - a row that is all zero transforms to a zero row (the transform is linear
//     and idct8 of zeros is zeros), so the row pass skips it.
func Inverse8x8(b *Block8x8) {
	var rest int32
	for _, v := range b[1:] {
		rest |= v
	}
	if rest == 0 {
		v := (b[0] + 32) >> 6
		for i := range b {
			b[i] = v
		}
		return
	}
	b[0] += 32
	var tmp Block8x8
	for r := 0; r < 64; r += 8 {
		row := b[r : r+8 : r+8]
		if row[0]|row[1]|row[2]|row[3]|row[4]|row[5]|row[6]|row[7] == 0 {
			continue // tmp is already zero
		}
		tmp[r], tmp[r+1], tmp[r+2], tmp[r+3], tmp[r+4], tmp[r+5], tmp[r+6], tmp[r+7] =
			idct8(row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7])
	}
	for c := 0; c < 8; c++ {
		o0, o1, o2, o3, o4, o5, o6, o7 := idct8(tmp[c], tmp[8+c], tmp[16+c], tmp[24+c], tmp[32+c], tmp[40+c], tmp[48+c], tmp[56+c])
		b[c], b[8+c], b[16+c], b[24+c] = o0>>6, o1>>6, o2>>6, o3>>6
		b[32+c], b[40+c], b[48+c], b[56+c] = o4>>6, o5>>6, o6>>6, o7>>6
	}
}

func AddResidual8x8(plane []byte, stride, offset int, b *Block8x8) {
	for y := 0; y < 8; y++ {
		row := plane[offset+y*stride:]
		for x := 0; x < 8; x++ {
			v := int32(row[x]) + b[y*8+x]
			if v < 0 {
				v = 0
			} else if v > 255 {
				v = 255
			}
			row[x] = byte(v)
		}
	}
}
