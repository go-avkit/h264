//go:build arm64 && !purego

package deblock

// kernelsLF routes the 16-line luma edges and the 8-line chroma edges through
// go-videodsp/kernels, whose arm64 implementation is NEON.
const kernelsLF = true
