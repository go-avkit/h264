//go:build arm64 && !purego

package mc

// kernelsLuma routes luma prediction, and kernelsWeight explicit weighted
// prediction, through go-videodsp/kernels, whose arm64 implementation is NEON.
const (
	kernelsLuma   = true
	kernelsWeight = true
	kernelsChroma = true
)
