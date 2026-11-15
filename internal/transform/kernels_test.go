package transform

import (
	"bytes"
	"math/rand"
	"testing"
)

// The entry point equals the three scalar stages it replaces, for blocks of
// every density and every qp of the standard's range.
func TestDequantInverse8x8AddMatchesScalar(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	var ls LevelScale8x8
	for m := range ls {
		for i := range ls[m] {
			ls[m][i] = int32(16 + r.Intn(1500))
		}
	}
	for qp := 0; qp <= 51; qp++ {
		for n := 0; n < 40; n++ {
			var b, c Block8x8
			switch n % 4 {
			case 0:
				b[0] = int32(r.Intn(601) - 300)
			case 1:
				for k := 0; k < 4; k++ {
					b[r.Intn(64)] = int32(r.Intn(201) - 100)
				}
			case 2:
				for i := range b {
					b[i] = int32(r.Intn(41) - 20)
				}
			}
			c = b
			got := make([]byte, 8*24)
			for i := range got {
				got[i] = byte(r.Intn(256))
			}
			want := append([]byte(nil), got...)
			DequantInverse8x8Add(got, 24, 5, &b, qp, &ls)
			Dequant8x8(&c, qp, &ls)
			Inverse8x8(&c)
			AddResidual8x8(want, 24, 5, &c)
			if !bytes.Equal(got, want) {
				t.Fatalf("qp %d case %d: planes differ", qp, n)
			}
		}
	}
}

// A negative qp has no kernel form and takes the scalar path.
func TestDequantInverse8x8AddNegativeQP(t *testing.T) {
	var ls LevelScale8x8
	var b Block8x8
	defer func() {
		if recover() == nil {
			t.Fatal("a negative qp must reach the scalar path and its modulo panic or wrap; nothing happened")
		}
	}()
	DequantInverse8x8Add(make([]byte, 64), 8, 0, &b, -1, &ls)
}
