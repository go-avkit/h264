package cabac

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The extractor that produced spec_tables_test.go, so that the independence of
// that transcription can be re-run: it reads Tables 9-44 and 9-45 from the TEXT
// of ITU-T H.264 (V16) (06/2026) (pdftotext of the PDF), checks the structure of
// what it reads, and TestExtractorReproducesTheTranscription compares the result
// with the committed transcription and with the SHA-256 of its canonical form.
// The text of the standard is not in the repository (copyright): the test runs
// only when H264_SPEC_PDF (the PDF; pdftotext is run on it) or H264_SPEC_TXT (its
// text) names it, and says so in its log when it does not.

type specTables struct {
	lps    [64][4]uint8
	toLPS  [64]uint8
	toMPS  [64]uint8
	values string // the canonical text the digest is taken over
}

func nonEmptyLines(lines []string) []string {
	var out []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

// extractTables reads the two tables from the text. Table 9-44 comes out of
// pdftotext as one token per line: a page number and the column labels
// (qCodIRangeIdx 0..3 twice), then 32 rows of ten numbers (pStateIdx, four
// ranges, pStateIdx + 32, four ranges). Table 9-45 is four groups of three rows
// (pStateIdx, transIdxLPS, transIdxMPS) of sixteen numbers.
func extractTables(text string) (specTables, error) {
	var t specTables
	lines := strings.Split(text, "\n")
	// The heading line of a table, not its entry in the list of tables (dotted
	// leader) nor a sentence that mentions it.
	find := func(prefix, title string) int {
		for i, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, prefix) && strings.Contains(l, title) && !strings.Contains(l, "....") {
				return i
			}
		}
		return -1
	}
	a, b := find("Table 9-44", "Specification"), find("Table 9-45", "State transition table")
	if a < 0 || b < 0 || b <= a {
		return t, fmt.Errorf("Table 9-44 / 9-45 headings not found (%d, %d)", a, b)
	}

	var nums []int
	for _, s := range nonEmptyLines(lines[a+1 : b]) {
		if v, err := strconv.Atoi(s); err == nil {
			nums = append(nums, v)
		}
	}
	if len(nums) != 329 || fmt.Sprint(nums[1:9]) != "[0 1 2 3 0 1 2 3]" {
		return t, fmt.Errorf("Table 9-44: %d numbers, preamble %v; want 329 and the labels 0 1 2 3 0 1 2 3 after the page number", len(nums), nums[:min(9, len(nums))])
	}
	rows := nums[9:]
	for r := 0; r < 32; r++ {
		row := rows[r*10 : r*10+10]
		if row[0] != r || row[5] != r+32 {
			return t, fmt.Errorf("Table 9-44 row %d: pStateIdx labels %d and %d", r, row[0], row[5])
		}
		for q := 0; q < 4; q++ {
			if row[1+q] < 0 || row[1+q] > 255 || row[6+q] < 0 || row[6+q] > 255 {
				return t, fmt.Errorf("Table 9-44 row %d: a range out of 0..255", r)
			}
			t.lps[r][q], t.lps[r+32][q] = uint8(row[1+q]), uint8(row[6+q])
		}
	}

	toks := nonEmptyLines(lines[b+1:])
	pos := 0
	for g := 0; g < 4; g++ {
		for _, name := range []string{"pStateIdx", "transIdxLPS", "transIdxMPS"} {
			if pos >= len(toks) || toks[pos] != name {
				return t, fmt.Errorf("Table 9-45 group %d: expected %q", g, name)
			}
			pos++
			for k := 0; k < 16; k++ {
				if pos >= len(toks) {
					return t, fmt.Errorf("Table 9-45 ends early")
				}
				v, err := strconv.Atoi(toks[pos])
				if err != nil || v < 0 || v > 63 {
					return t, fmt.Errorf("Table 9-45 group %d %s: %q is not a state", g, name, toks[pos])
				}
				pos++
				switch name {
				case "pStateIdx":
					if v != g*16+k {
						return t, fmt.Errorf("Table 9-45: pStateIdx %d where %d belongs", v, g*16+k)
					}
				case "transIdxLPS":
					t.toLPS[g*16+k] = uint8(v)
				default:
					t.toMPS[g*16+k] = uint8(v)
				}
			}
		}
	}

	// Sanity of the content itself, independent of the engine's tables: a range
	// grows with qCodIRangeIdx along a row and shrinks with pStateIdx down a
	// column; transIdxMPS climbs by one and sticks at 62 and 63; transIdxLPS
	// never exceeds its own state.
	for s := 0; s < 64; s++ {
		for q := 0; q < 3; q++ {
			if t.lps[s][q] > t.lps[s][q+1] {
				return t, fmt.Errorf("Table 9-44 state %d: ranges not increasing along the row", s)
			}
		}
		if s < 63 {
			for q := 0; q < 4; q++ {
				if t.lps[s][q] < t.lps[s+1][q] {
					return t, fmt.Errorf("Table 9-44 column %d: ranges increase at state %d", q, s)
				}
			}
		}
		if int(t.toLPS[s]) > s {
			return t, fmt.Errorf("Table 9-45: transIdxLPS[%d] = %d exceeds its state", s, t.toLPS[s])
		}
		want := s + 1
		if s >= 62 {
			want = s
		}
		if int(t.toMPS[s]) != want {
			return t, fmt.Errorf("Table 9-45: transIdxMPS[%d] = %d, want %d", s, t.toMPS[s], want)
		}
	}

	var sb strings.Builder
	for s := 0; s < 64; s++ {
		fmt.Fprintf(&sb, "%d %d %d %d %d %d %d\n", s, t.lps[s][0], t.lps[s][1], t.lps[s][2], t.lps[s][3], t.toLPS[s], t.toMPS[s])
	}
	t.values = sb.String()
	return t, nil
}

// specTablesDigest is the SHA-256 of the canonical values (one line per
// pStateIdx: the state, the four ranges, transIdxLPS, transIdxMPS) the extractor
// read from the 06/2026 text. It is all that is kept of the standard's text.
const specTablesDigest = "b87135938c754e8274748e76ba6de83862aa3f074da92e1f5bb04a0be6d5d4f9"

func specText(t *testing.T) string {
	t.Helper()
	if txt := os.Getenv("H264_SPEC_TXT"); txt != "" {
		b, err := os.ReadFile(txt)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	pdf := os.Getenv("H264_SPEC_PDF")
	if pdf == "" {
		t.Skip("SKIPPED, nothing was checked: set H264_SPEC_PDF to ITU-T H.264 (V16) (06/2026) (pdftotext is run on it) or H264_SPEC_TXT to its text to re-run the extraction of Tables 9-44 and 9-45; the standard's text is not in the repository")
	}
	out, err := exec.Command("pdftotext", pdf, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext %s: %v", pdf, err)
	}
	return string(out)
}

func TestExtractorReproducesTheTranscription(t *testing.T) {
	got, err := extractTables(specText(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.lps != specRangeTabLPS || got.toLPS != specTransIdxLPS || got.toMPS != specTransIdxMPS {
		t.Fatal("the extractor's tables differ from spec_tables_test.go")
	}
	if sum := fmt.Sprintf("%x", sha256.Sum256([]byte(got.values))); sum != specTablesDigest {
		t.Fatalf("digest of the extracted values %s, committed %s", sum, specTablesDigest)
	}
}

// The extractor's refusals, on fabricated text: it must reject a text that is
// not the tables, a table with a wrong label, and content that breaks the
// monotonic structure, so that a PDF change cannot slide through as "no change".
func TestExtractorRefusesWhatIsNotTheTables(t *testing.T) {
	if _, err := extractTables("nothing here"); err == nil {
		t.Error("text without the tables accepted")
	}
	if _, err := extractTables("Table 9-44 – x\n1\nTable 9-45 – y\n"); err == nil {
		t.Error("an empty Table 9-44 accepted")
	}
	// A well-formed text built from the committed transcription is accepted, and
	// each corruption of it is refused.
	build := func(mutate func(lps *[64][4]uint8, tl, tm *[64]uint8, labelBad bool)) string {
		lps, tl, tm := specRangeTabLPS, specTransIdxLPS, specTransIdxMPS
		mutate(&lps, &tl, &tm, false)
		var sb strings.Builder
		sb.WriteString("Table 9-44 – Specification\n278\n0\n1\n2\n3\n0\n1\n2\n3\n")
		for r := 0; r < 32; r++ {
			fmt.Fprintf(&sb, "%d\n", r)
			for q := 0; q < 4; q++ {
				fmt.Fprintf(&sb, "%d\n", lps[r][q])
			}
			fmt.Fprintf(&sb, "%d\n", r+32)
			for q := 0; q < 4; q++ {
				fmt.Fprintf(&sb, "%d\n", lps[r+32][q])
			}
		}
		sb.WriteString("Table 9-45 – State transition table\n")
		for g := 0; g < 4; g++ {
			sb.WriteString("pStateIdx\n")
			for k := 0; k < 16; k++ {
				fmt.Fprintf(&sb, "%d\n", g*16+k)
			}
			sb.WriteString("transIdxLPS\n")
			for k := 0; k < 16; k++ {
				fmt.Fprintf(&sb, "%d\n", tl[g*16+k])
			}
			sb.WriteString("transIdxMPS\n")
			for k := 0; k < 16; k++ {
				fmt.Fprintf(&sb, "%d\n", tm[g*16+k])
			}
		}
		return sb.String()
	}
	if _, err := extractTables(build(func(*[64][4]uint8, *[64]uint8, *[64]uint8, bool) {})); err != nil {
		t.Fatalf("the committed transcription rendered as text was refused: %v", err)
	}
	for name, mutate := range map[string]func(lps *[64][4]uint8, tl, tm *[64]uint8, _ bool){
		"range not increasing along a row": func(l *[64][4]uint8, _, _ *[64]uint8, _ bool) { l[10][0], l[10][1] = l[10][1], l[10][0]-1 },
		"range increasing down a column":   func(l *[64][4]uint8, _, _ *[64]uint8, _ bool) { l[20][3] = l[19][3] + 1 },
		"transIdxLPS above its state":      func(_ *[64][4]uint8, tl, _ *[64]uint8, _ bool) { tl[5] = 9 },
		"transIdxMPS not climbing":         func(_ *[64][4]uint8, _, tm *[64]uint8, _ bool) { tm[7] = 9 },
		"a range above 255":                func(*[64][4]uint8, *[64]uint8, *[64]uint8, bool) {},
	} {
		text := build(mutate)
		if name == "a range above 255" {
			text = strings.Replace(text, "\n128\n176\n208\n240\n", "\n128\n176\n208\n256\n", 1) // state 0's row
		}
		if _, err := extractTables(text); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	bad := strings.Replace(build(func(*[64][4]uint8, *[64]uint8, *[64]uint8, bool) {}), "pStateIdx\n16\n", "pStateIdx\n17\n", 1)
	if _, err := extractTables(bad); err == nil {
		t.Error("a wrong pStateIdx label in Table 9-45 accepted")
	}
	short := build(func(*[64][4]uint8, *[64]uint8, *[64]uint8, bool) {})
	if _, err := extractTables(short[:len(short)-20]); err == nil {
		t.Error("a truncated Table 9-45 accepted")
	}
}
