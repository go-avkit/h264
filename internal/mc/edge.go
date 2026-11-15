package mc

// EmulateEdge fills the w x h block dst (row stride dstStride) with the samples
// of a plane whose top-left sample is plane[origin] and whose rows are stride
// apart, taken at (x0 + i, y0 + j) with both coordinates clamped into the
// picture [0, picW) x [0, picH): equations 8-239 and 8-240 (luma) and 8-262 to
// 8-269 (chroma) of ITU-T H.264. A prediction block whose footprint leaves the
// picture reads this block instead of a reference padded all around.
func EmulateEdge(dst []byte, dstStride int, plane []byte, stride, origin, picW, picH, x0, y0, w, h int) {
	// Columns of the block left of the picture, inside it, and right of it.
	left := min(max(-x0, 0), w)
	right := min(max(x0+w-picW, 0), w)
	mid := w - left - right
	for j := 0; j < h; j++ {
		y := min(max(y0+j, 0), picH-1)
		row := plane[origin+y*stride : origin+y*stride+picW]
		out := dst[j*dstStride : j*dstStride+w]
		if left > 0 {
			v := row[0]
			for i := 0; i < left; i++ {
				out[i] = v
			}
		}
		if mid > 0 {
			copy(out[left:left+mid], row[x0+left:x0+left+mid])
		}
		if right > 0 {
			v := row[picW-1]
			for i := w - right; i < w; i++ {
				out[i] = v
			}
		}
	}
}
