//go:build amd64 && !purego

package deblock

import (
	"os"
	"testing"

	"github.com/go-videodsp/kernels/cpu"
)

// See internal/mc: the route is asserted both ways, and a processor without
// AVX2 on CI is a failure, not a pass of the equality.
func TestKernelsRoutedByAVX2(t *testing.T) {
	if (os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("H264_REQUIRE_AVX2") == "1") && !cpu.HasAVX2 {
		t.Fatal("AVX2 is required on this runner and the kernels module's probe says no: the kernels route is not tested")
	}
	if kernelsLF != cpu.HasAVX2 {
		t.Fatalf("kernelsLF=%v, want %v (cpu.HasAVX2)", kernelsLF, cpu.HasAVX2)
	}
}
