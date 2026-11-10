// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

import "testing"

// Below 30 the table is the identity and above it flattens (Table 8-15); the sum
// is clipped at both ends, so an offset cannot index outside it.
func TestChromaQPFollowsTable815(t *testing.T) {
	for _, c := range []struct{ qp, off, want int }{
		{0, 0, 0}, {29, 0, 29}, {30, 0, 29}, {33, 0, 32}, {34, 0, 32}, {51, 0, 39},
		{51, 12, 39}, {0, -12, 0}, {40, -12, 28}, {26, 2, 28}, {-5, 0, 0}, {100, 0, 39},
	} {
		if got := ChromaQP(c.qp, c.off); got != c.want {
			t.Errorf("ChromaQP(%d, %d) = %d, want %d", c.qp, c.off, got, c.want)
		}
	}
}
