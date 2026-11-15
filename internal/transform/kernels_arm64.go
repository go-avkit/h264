//go:build arm64 && !purego

package transform

// kernelsITX routes the 8x8 dequantise, inverse transform and add through
// go-videodsp/kernels, whose arm64 implementation is NEON.
const kernelsITX = true
