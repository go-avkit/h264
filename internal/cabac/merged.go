package cabac

// The decoder keeps a context as one byte, pStateIdx<<1 | valMPS, so a decision
// reads and writes one byte instead of two fields, and the three tables below are
// indexed by that byte directly: the least probable symbol's range
// (rangeTabLPS, Table 9-44, by qCodIRangeIdx), the next byte after the most
// probable symbol (transIdxMPS, Table 9-45) and after the least probable one
// (transIdxLPS, with valMPS flipped when pStateIdx is 0, 9.3.3.2.1.1). The
// flip that was a branch is a table entry. They are derived from the
// specification's tables at start-up, so they cannot disagree with them
// (TestMergedTablesAreTheSpecsTables checks every entry independently).
var (
	mergedLPS     [128][4]uint8
	mergedNextMPS [128]uint8
	mergedNextLPS [128]uint8
)

func init() {
	for s := 0; s < 128; s++ {
		state, mps := s>>1, s&1
		mergedLPS[s] = rangeTabLPS[state]
		mergedNextMPS[s] = transIdxMPS[state]<<1 | uint8(mps)
		flipped := mps
		if state == 0 {
			flipped = 1 - mps
		}
		mergedNextLPS[s] = transIdxLPS[state]<<1 | uint8(flipped)
	}
}
