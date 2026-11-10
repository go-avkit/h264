// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

// chromaQPTable is QPc as a function of qPI, Table 8-15, for qPI 0..51.
var chromaQPTable = [52]uint8{
	0, 1, 2, 3, 4, 5, 6, 7, 8, 9,
	10, 11, 12, 13, 14, 15, 16, 17, 18, 19,
	20, 21, 22, 23, 24, 25, 26, 27, 28, 29,
	29, 30, 31, 32, 32, 33, 34, 34, 35, 35,
	36, 36, 37, 37, 37, 38, 38, 38, 39, 39,
	39, 39,
}

// ChromaQP is QPc for a luma quantiser and a chroma_qp_index_offset (8.5.8,
// equations 8-312 and 8-313 for 8-bit video): the sum is clipped to 0..51 and then
// looked up in Table 8-15.
func ChromaQP(qpY, offset int) int {
	return int(chromaQPTable[min(max(qpY+offset, 0), 51)])
}
