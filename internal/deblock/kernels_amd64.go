//go:build amd64 && !purego

package deblock

import "github.com/go-videodsp/kernels/cpu"

// kernelsLF routes the 16-line luma edges and the 8-line chroma edges through
// go-videodsp/kernels when the processor has AVX2 (cpu probe of the kernels
// module); faster than the avo kernels in the whole-decode measurement.
var kernelsLF = cpu.HasAVX2
