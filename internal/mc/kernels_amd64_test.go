//go:build amd64 && !purego

package mc

import (
	"testing"

	"github.com/go-videodsp/kernels/cpu"
)

// The routing is a decision of the build and the processor: with AVX2 the
// kernels module runs, without it the avo kernels of internal/simd. Asserted
// both ways, so a route that silently stayed on the other path fails here
// instead of passing every sample test.
func TestKernelsRoutedByAVX2(t *testing.T) {
	if kernelsLuma != cpu.HasAVX2 || kernelsWeight != cpu.HasAVX2 || FusedAverage != cpu.HasAVX2 {
		t.Fatalf("kernelsLuma=%v kernelsWeight=%v FusedAverage=%v, want all %v (cpu.HasAVX2)", kernelsLuma, kernelsWeight, FusedAverage, cpu.HasAVX2)
	}
}
