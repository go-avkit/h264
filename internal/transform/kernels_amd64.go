//go:build amd64 && !purego

package transform

import "github.com/go-videodsp/kernels/cpu"

// kernelsITX routes the 8x8 dequantise, inverse transform and add through
// go-videodsp/kernels when the processor has AVX2 (the module's cpu probe).
var kernelsITX = cpu.HasAVX2
