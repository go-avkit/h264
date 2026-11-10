package parallel

import (
	"runtime"
	"sync/atomic"
	"testing"
)

func TestForVisitsEveryIndexOnce(t *testing.T) {
	for _, c := range []struct{ n, chunk int }{{0, 4}, {1, 4}, {7, 3}, {100, 1}, {1000, 64}, {5, 0}, {3, 10}} {
		seen := make([]atomic.Int32, c.n)
		For(c.n, c.chunk, func(lo, hi int) {
			if lo < 0 || hi > c.n || lo >= hi {
				t.Errorf("n %d, chunk %d: range [%d, %d)", c.n, c.chunk, lo, hi)
				return
			}
			for i := lo; i < hi; i++ {
				seen[i].Add(1)
			}
		})
		for i := range seen {
			if got := seen[i].Load(); got != 1 {
				t.Fatalf("n %d, chunk %d: index %d visited %d times", c.n, c.chunk, i, got)
			}
		}
	}
}

func TestWavefrontKeepsEachCellBehindTheRowAbove(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("one processor runs the serial path only")
	}
	for _, c := range []struct{ rows, cols, lag int }{{40, 60, 1}, {40, 60, 2}, {90, 215, 2}, {3, 500, 2}, {200, 8, 1}} {
		visited := make([]atomic.Bool, c.rows*c.cols)
		var violations, calls atomic.Int32
		Wavefront(c.rows, c.cols, c.lag, func(x, y int) {
			calls.Add(1)
			if x > 0 && !visited[y*c.cols+x-1].Load() {
				violations.Add(1)
			}
			if y > 0 {
				for dx := 0; dx < c.lag && x+dx < c.cols; dx++ {
					if !visited[(y-1)*c.cols+x+dx].Load() {
						violations.Add(1)
					}
				}
			}
			visited[y*c.cols+x].Store(true)
		})
		if v := violations.Load(); v != 0 {
			t.Fatalf("%dx%d, lag %d: %d cells ran ahead of a cell they depend on", c.rows, c.cols, c.lag, v)
		}
		if n := calls.Load(); int(n) != c.rows*c.cols {
			t.Fatalf("%dx%d, lag %d: %d calls", c.rows, c.cols, c.lag, n)
		}
	}
}

func TestWavefrontRunsSmallJobsInRasterOrder(t *testing.T) {
	var order []int
	Wavefront(4, 5, 2, func(x, y int) { order = append(order, y*5+x) })
	for i, v := range order {
		if v != i {
			t.Fatalf("cell %d ran in position %d", v, i)
		}
	}
	if len(order) != 20 {
		t.Fatalf("%d cells ran, want 20", len(order))
	}
	Wavefront(0, 5, 2, func(x, y int) { t.Fatal("an empty wavefront ran a cell") })
}

func TestWorkersStaysWithinTheLimit(t *testing.T) {
	if w := Workers(1); w != 1 {
		t.Fatalf("Workers(1) = %d", w)
	}
	if w := Workers(0); w != 1 {
		t.Fatalf("Workers(0) = %d", w)
	}
	if w := Workers(1 << 20); w != runtime.GOMAXPROCS(0) {
		t.Fatalf("Workers(huge) = %d, GOMAXPROCS %d", w, runtime.GOMAXPROCS(0))
	}
}

func TestRowsFollowOnlyTheRowTheyTrail(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("one processor runs the serial path only")
	}
	const rows, cols, lag, lanes = 60, 120, 2, 8
	trail := func(y int) int {
		if y < 3 {
			return -1
		}
		return y - 3
	}
	visited := make([]atomic.Bool, rows*cols)
	var violations, badLane atomic.Int32
	Rows(lanes, rows, cols, lag, trail, func(lane, y int, wait, done func(int)) {
		if lane < 0 || lane >= lanes {
			badLane.Add(1)
		}
		for x := 0; x < cols; x++ {
			wait(x)
			if t := trail(y); t >= 0 {
				for dx := 0; dx < lag && x+dx < cols; dx++ {
					if !visited[t*cols+x+dx].Load() {
						violations.Add(1)
					}
				}
			}
			visited[y*cols+x].Store(true)
			done(x)
		}
	})
	if v := violations.Load(); v != 0 {
		t.Fatalf("%d cells ran ahead of the row they trail", v)
	}
	if badLane.Load() != 0 {
		t.Fatal("a row was handed a lane outside the range asked for")
	}
	for i := range visited {
		if !visited[i].Load() {
			t.Fatalf("cell %d never ran", i)
		}
	}
}

func TestRowsReleaseFollowersWhenARowStopsEarly(t *testing.T) {
	const rows, cols = 6, 50
	var ran atomic.Int32
	Rows(4, rows, cols, 2, func(y int) int { return y - 1 }, func(_, y int, wait, done func(int)) {
		for x := 0; x < cols; x++ {
			if y == 2 && x == 10 {
				return
			}
			wait(x)
			ran.Add(1)
			done(x)
		}
	})
	if got := ran.Load(); got != int32((rows-1)*cols+10) {
		t.Fatalf("%d cells ran, want %d", got, (rows-1)*cols+10)
	}
}
