//go:build (!arm64 && !amd64) || purego

package deblock

// kernelsLF is false where the kernels module has no assembly: the portable
// code runs.
const kernelsLF = false
