package deblock

import (
	"testing"

	"github.com/go-videodsp/kernels/lf"
)

// MBStrengthsOK takes what lf.DeblockMB takes and nothing else.
func TestMBStrengthsOK(t *testing.T) {
	mk := func(f func(*lf.MBEdges)) *lf.MBEdges {
		e := &lf.MBEdges{}
		e.BS[0][0] = [4]uint8{4, 4, 4, 4}
		e.BS[1][2] = [4]uint8{1, 0, 3, 2}
		f(e)
		return e
	}
	if !MBStrengthsOK(mk(func(*lf.MBEdges) {})) {
		t.Fatal("four 4s on edge 0 and lower values elsewhere must be accepted")
	}
	for name, e := range map[string]*lf.MBEdges{
		"4 beside others":    mk(func(e *lf.MBEdges) { e.BS[1][0] = [4]uint8{4, 3, 4, 4} }),
		"4 on an inner edge": mk(func(e *lf.MBEdges) { e.BS[0][3] = [4]uint8{4, 4, 4, 4} }),
		"one 4 alone":        mk(func(e *lf.MBEdges) { e.BS[1][1][2] = 4 }),
	} {
		if MBStrengthsOK(e) {
			t.Errorf("%s was accepted", name)
		}
	}
}
