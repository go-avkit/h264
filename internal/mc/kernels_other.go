//go:build (!arm64 && !amd64) || purego

package mc

// The kernels module has no assembly here: the portable code of this package
// runs.
const (
	kernelsLuma   = false
	kernelsWeight = false
)
