package cavlc

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
)

func collectCoeffToken(t *testing.T, src *[4][17]string) []string {
	t.Helper()
	var out []string
	for t1 := 0; t1 < 4; t1++ {
		for tc := 0; tc < len(src[t1]); tc++ {
			p := src[t1][tc]
			if p == "" {
				continue
			}
			if t1 > tc && !(t1 == 0 && tc == 0) {
				t.Errorf("entry present for trailingOnes=%d totalCoeff=%d", t1, tc)
			}
			out = append(out, p)
		}
	}
	return out
}

func checkPrefixFree(t *testing.T, name string, codes []string) {
	t.Helper()
	seen := make(map[string]bool, len(codes))
	for _, c := range codes {
		if c == "" {
			t.Errorf("%s: empty code", name)
			continue
		}
		if strings.Trim(c, "01") != "" {
			t.Errorf("%s: code %q has non-binary digits", name, c)
		}
		if seen[c] {
			t.Errorf("%s: duplicate code %q", name, c)
		}
		seen[c] = true
	}
	for _, a := range codes {
		for _, b := range codes {
			if a == b {
				continue
			}
			if strings.HasPrefix(b, a) {
				t.Errorf("%s: code %q is a prefix of %q", name, a, b)
			}
		}
	}
}

func kraft(codes []string) *big.Rat {
	sum := new(big.Rat)
	for _, c := range codes {
		term := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(len(c))))
		sum.Add(sum, term)
	}
	return sum
}

func checkKraft(t *testing.T, name string, codes []string, maxLen int) {
	t.Helper()
	sum := kraft(codes)
	one := new(big.Rat).SetInt64(1)
	if sum.Cmp(one) > 0 {
		t.Errorf("%s: Kraft sum %s exceeds 1, the code cannot be prefix free", name, sum.RatString())
		return
	}
	unit := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Lsh(big.NewInt(1), uint(maxLen)))
	slack := new(big.Rat).Sub(one, sum)
	slackUnits := new(big.Rat).Quo(slack, unit)
	if !slackUnits.IsInt() {
		t.Fatalf("%s: slack %s is not a whole number of %d-bit codewords", name, slack.RatString(), maxLen)
	}
	n := slackUnits.Num().Int64()
	if n > 4 {
		t.Errorf("%s: Kraft sum %s leaves %d unused %d-bit codewords, the table is very likely wrong",
			name, sum.RatString(), n, maxLen)
	}
	t.Logf("%s: %d codes, Kraft sum %s, %d unused %d-bit codewords", name, len(codes), sum.RatString(), n, maxLen)
}

func maxLength(codes []string) int {
	m := 0
	for _, c := range codes {
		if len(c) > m {
			m = len(c)
		}
	}
	return m
}

func TestCoeffTokenTablesAreValidPrefixCodes(t *testing.T) {
	tables := []struct {
		name string
		src  *[4][17]string
		want int
	}{
		{"coeff_token nC<2", &coeffTokenNC0, 62},
		{"coeff_token 2<=nC<4", &coeffTokenNC2, 62},
		{"coeff_token 4<=nC<8", &coeffTokenNC4, 62},
	}
	for _, tc := range tables {
		codes := collectCoeffToken(t, tc.src)
		if len(codes) != tc.want {
			t.Errorf("%s: %d entries, want %d", tc.name, len(codes), tc.want)
		}
		checkPrefixFree(t, tc.name, codes)
		checkKraft(t, tc.name, codes, maxLength(codes))
	}
}

func TestChromaDCCoeffTokenTable(t *testing.T) {
	var codes []string
	for t1 := 0; t1 < 4; t1++ {
		for tc := 0; tc < 5; tc++ {
			if p := coeffTokenChromaDC420[t1][tc]; p != "" {
				codes = append(codes, p)
			}
		}
	}
	if len(codes) != 14 {
		t.Errorf("chroma DC coeff_token: %d entries, want 14", len(codes))
	}
	checkPrefixFree(t, "chroma DC coeff_token", codes)
	checkKraft(t, "chroma DC coeff_token", codes, maxLength(codes))
}

func TestTotalZerosTables(t *testing.T) {
	for i := 1; i < 16; i++ {
		codes := totalZeros4x4[i]
		name := fmt.Sprintf("total_zeros tzVlcIndex=%d", i)
		if want := 17 - i; len(codes) != want {
			t.Errorf("%s: %d entries, want %d", name, len(codes), want)
		}
		checkPrefixFree(t, name, codes)
		checkKraft(t, name, codes, maxLength(codes))
	}
	if len(totalZeros4x4[0]) != 0 {
		t.Error("total_zeros index 0 must be unused")
	}
}

func TestChromaDCTotalZerosTables(t *testing.T) {
	for i := 1; i < 4; i++ {
		codes := totalZerosChromaDC420[i]
		name := fmt.Sprintf("chroma DC total_zeros tzVlcIndex=%d", i)
		if want := 5 - i; len(codes) != want {
			t.Errorf("%s: %d entries, want %d", name, len(codes), want)
		}
		checkPrefixFree(t, name, codes)
		checkKraft(t, name, codes, maxLength(codes))
	}
}

func TestRunBeforeTables(t *testing.T) {
	for i := 1; i < 8; i++ {
		codes := runBefore[i]
		name := fmt.Sprintf("run_before zerosLeft=%d", i)
		want := i + 1
		if i == 7 {
			want = 15
		}
		if len(codes) != want {
			t.Errorf("%s: %d entries, want %d", name, len(codes), want)
		}
		checkPrefixFree(t, name, codes)
		checkKraft(t, name, codes, maxLength(codes))
	}
}
