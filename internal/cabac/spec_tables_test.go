package cabac

import "testing"

// Code below is the output of a parser of the text of ITU-T H.264 (V16) (06/2026),
// Tables 9-44 and 9-45 (pdftotext), checked for the row/column structure: it shares
// nothing with tables.go.
var specRangeTabLPS = [64][4]uint8{
	{128, 176, 208, 240}, // pStateIdx 0
	{128, 167, 197, 227}, // pStateIdx 1
	{128, 158, 187, 216}, // pStateIdx 2
	{123, 150, 178, 205}, // pStateIdx 3
	{116, 142, 169, 195}, // pStateIdx 4
	{111, 135, 160, 185}, // pStateIdx 5
	{105, 128, 152, 175}, // pStateIdx 6
	{100, 122, 144, 166}, // pStateIdx 7
	{95, 116, 137, 158},  // pStateIdx 8
	{90, 110, 130, 150},  // pStateIdx 9
	{85, 104, 123, 142},  // pStateIdx 10
	{81, 99, 117, 135},   // pStateIdx 11
	{77, 94, 111, 128},   // pStateIdx 12
	{73, 89, 105, 122},   // pStateIdx 13
	{69, 85, 100, 116},   // pStateIdx 14
	{66, 80, 95, 110},    // pStateIdx 15
	{62, 76, 90, 104},    // pStateIdx 16
	{59, 72, 86, 99},     // pStateIdx 17
	{56, 69, 81, 94},     // pStateIdx 18
	{53, 65, 77, 89},     // pStateIdx 19
	{51, 62, 73, 85},     // pStateIdx 20
	{48, 59, 69, 80},     // pStateIdx 21
	{46, 56, 66, 76},     // pStateIdx 22
	{43, 53, 63, 72},     // pStateIdx 23
	{41, 50, 59, 69},     // pStateIdx 24
	{39, 48, 56, 65},     // pStateIdx 25
	{37, 45, 54, 62},     // pStateIdx 26
	{35, 43, 51, 59},     // pStateIdx 27
	{33, 41, 48, 56},     // pStateIdx 28
	{32, 39, 46, 53},     // pStateIdx 29
	{30, 37, 43, 50},     // pStateIdx 30
	{29, 35, 41, 48},     // pStateIdx 31
	{27, 33, 39, 45},     // pStateIdx 32
	{26, 31, 37, 43},     // pStateIdx 33
	{24, 30, 35, 41},     // pStateIdx 34
	{23, 28, 33, 39},     // pStateIdx 35
	{22, 27, 32, 37},     // pStateIdx 36
	{21, 26, 30, 35},     // pStateIdx 37
	{20, 24, 29, 33},     // pStateIdx 38
	{19, 23, 27, 31},     // pStateIdx 39
	{18, 22, 26, 30},     // pStateIdx 40
	{17, 21, 25, 28},     // pStateIdx 41
	{16, 20, 23, 27},     // pStateIdx 42
	{15, 19, 22, 25},     // pStateIdx 43
	{14, 18, 21, 24},     // pStateIdx 44
	{14, 17, 20, 23},     // pStateIdx 45
	{13, 16, 19, 22},     // pStateIdx 46
	{12, 15, 18, 21},     // pStateIdx 47
	{12, 14, 17, 20},     // pStateIdx 48
	{11, 14, 16, 19},     // pStateIdx 49
	{11, 13, 15, 18},     // pStateIdx 50
	{10, 12, 15, 17},     // pStateIdx 51
	{10, 12, 14, 16},     // pStateIdx 52
	{9, 11, 13, 15},      // pStateIdx 53
	{9, 11, 12, 14},      // pStateIdx 54
	{8, 10, 12, 14},      // pStateIdx 55
	{8, 9, 11, 13},       // pStateIdx 56
	{7, 9, 11, 12},       // pStateIdx 57
	{7, 9, 10, 12},       // pStateIdx 58
	{7, 8, 10, 11},       // pStateIdx 59
	{6, 8, 9, 11},        // pStateIdx 60
	{6, 7, 9, 10},        // pStateIdx 61
	{6, 7, 8, 9},         // pStateIdx 62
	{2, 2, 2, 2},         // pStateIdx 63
}

var specTransIdxLPS = [64]uint8{
	0, 0, 1, 2, 2, 4, 4, 5, 6, 7, 8, 9, 9, 11, 11, 12,
	13, 13, 15, 15, 16, 16, 18, 18, 19, 19, 21, 21, 22, 22, 23, 24,
	24, 25, 26, 26, 27, 27, 28, 29, 29, 30, 30, 30, 31, 32, 32, 33,
	33, 33, 34, 34, 35, 35, 35, 36, 36, 36, 37, 37, 37, 38, 38, 63,
}

var specTransIdxMPS = [64]uint8{
	1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16,
	17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32,
	33, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43, 44, 45, 46, 47, 48,
	49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 59, 60, 61, 62, 62, 63,
}

// The engine's tables (tables.go, used by the encoder and by merged.go) against
// the independent transcription above, entry by entry. The tests of the engine
// and the bit-serial oracles use the transcription, not tables.go: a typo in
// tables.go cannot make the engine and its oracle agree on a wrong table.
func TestSourceTablesEqualTheSpecTranscription(t *testing.T) {
	for s := 0; s < 64; s++ {
		if rangeTabLPS[s] != specRangeTabLPS[s] {
			t.Errorf("rangeTabLPS[%d] = %v, Table 9-44 says %v", s, rangeTabLPS[s], specRangeTabLPS[s])
		}
		if transIdxLPS[s] != specTransIdxLPS[s] {
			t.Errorf("transIdxLPS[%d] = %d, Table 9-45 says %d", s, transIdxLPS[s], specTransIdxLPS[s])
		}
		if transIdxMPS[s] != specTransIdxMPS[s] {
			t.Errorf("transIdxMPS[%d] = %d, Table 9-45 says %d", s, transIdxMPS[s], specTransIdxMPS[s])
		}
	}
}
