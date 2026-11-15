//go:build !arm64 || purego

package deblock

// kernelsLF is false where the kernels module has no assembly: amd64 keeps the
// avo kernels of internal/simd until the AVX2 versions of the kernels are
// compared with them.
const kernelsLF = false
