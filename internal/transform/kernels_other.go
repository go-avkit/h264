//go:build (!arm64 && !amd64) || purego

package transform

// kernelsITX is false where the kernels module has no assembly: the portable
// code runs.
const kernelsITX = false
