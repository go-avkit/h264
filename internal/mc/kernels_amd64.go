//go:build amd64 && !purego

package mc

import "github.com/go-videodsp/kernels/cpu"

// kernelsLuma routes luma prediction, and kernelsWeight explicit weighted
// prediction, through go-videodsp/kernels when the processor has AVX2 (cpu
// probe of the kernels module). Measured on a Ryzen 9 7950X with the
// 1080p clips of the bench (one core, leave-one-stage-out against the avo
// kernels of internal/simd): each stage is faster with the kernels.
var (
	kernelsLuma   = cpu.HasAVX2
	kernelsWeight = cpu.HasAVX2
	kernelsChroma = cpu.HasAVX2
)
