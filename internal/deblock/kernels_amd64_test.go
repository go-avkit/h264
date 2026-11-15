//go:build amd64 && !purego

package deblock

import (
	"testing"

	"github.com/go-videodsp/kernels/cpu"
)

// See internal/mc: the route is asserted both ways.
func TestKernelsRoutedByAVX2(t *testing.T) {
	if kernelsLF != cpu.HasAVX2 {
		t.Fatalf("kernelsLF=%v, want %v (cpu.HasAVX2)", kernelsLF, cpu.HasAVX2)
	}
}
