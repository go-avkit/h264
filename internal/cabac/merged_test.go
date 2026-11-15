package cabac

import "testing"

// splitContexts is the decoder's contexts as the two fields the encoder and the
// bit-serial oracles of these tests keep.
func splitContexts(d *Decoder) [NumContexts]context {
	var out [NumContexts]context
	for i, v := range d.ctx {
		out[i] = context{state: v >> 1, mps: v & 1}
	}
	return out
}

// Every entry of the merged tables against 9.3.3.2.1 and 9.3.3.2.1.1 stated
// independently of the way merged.go derives them: for every pStateIdx and
// valMPS, the range of the least probable symbol is Table 9-44's, the state
// after an MPS is Table 9-45's transIdxMPS with valMPS unchanged, and after an
// LPS it is transIdxLPS with valMPS inverted exactly when pStateIdx is 0.
func TestMergedTablesAreTheSpecsTables(t *testing.T) {
	for state := 0; state < 64; state++ {
		for mps := 0; mps < 2; mps++ {
			s := state<<1 | mps
			for q := 0; q < 4; q++ {
				if mergedLPS[s][q] != specRangeTabLPS[state][q] {
					t.Fatalf("state %d mps %d q %d: LPS range %d, table %d", state, mps, q, mergedLPS[s][q], specRangeTabLPS[state][q])
				}
			}
			wantMPS := int(specTransIdxMPS[state])<<1 | mps
			if int(mergedNextMPS[s]) != wantMPS {
				t.Fatalf("state %d mps %d: next after MPS %d, want %d", state, mps, mergedNextMPS[s], wantMPS)
			}
			nm := mps
			if state == 0 {
				nm = 1 - mps
			}
			wantLPS := int(specTransIdxLPS[state])<<1 | nm
			if int(mergedNextLPS[s]) != wantLPS {
				t.Fatalf("state %d mps %d: next after LPS %d, want %d", state, mps, mergedNextLPS[s], wantLPS)
			}
		}
	}
}
