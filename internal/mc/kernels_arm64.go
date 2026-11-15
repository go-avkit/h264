//go:build arm64 && !purego

package mc

// kernelsLuma routes luma prediction through go-videodsp/kernels, whose arm64
// implementation is NEON.
const kernelsLuma = true
