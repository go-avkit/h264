//go:build amd64 && !purego

package mc

import (
	"os"
	"testing"

	"github.com/go-videodsp/kernels/cpu"
)

// The routing is a decision of the build and the processor: with AVX2 the
// kernels module runs, without it the avo kernels of internal/simd. It is
// asserted both ways, and on CI (GITHUB_ACTIONS, or H264_REQUIRE_AVX2=1 anywhere
// else) a processor without AVX2 is itself a failure: every hosted amd64 runner
// has AVX2, so a lane that silently ran the avo kernels, where the equality
// below holds trivially, would go green without testing the kernels route.
func TestKernelsRoutedByAVX2(t *testing.T) {
	if (os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("H264_REQUIRE_AVX2") == "1") && !cpu.HasAVX2 {
		t.Fatal("AVX2 is required on this runner and the kernels module's probe says no: the kernels route is not tested")
	}
	if kernelsLuma != cpu.HasAVX2 || kernelsWeight != cpu.HasAVX2 || FusedAverage != cpu.HasAVX2 {
		t.Fatalf("kernelsLuma=%v kernelsWeight=%v FusedAverage=%v, want all %v (cpu.HasAVX2)", kernelsLuma, kernelsWeight, FusedAverage, cpu.HasAVX2)
	}
}
