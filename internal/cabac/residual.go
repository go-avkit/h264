package cabac

import kcabac "github.com/go-videodsp/kernels/cabac"

const (
	CatIntra16x16DC = 0
	CatIntra16x16AC = 1
	CatLuma4x4      = 2
	CatChromaDC     = 3
	CatChromaAC     = 4
)

const (
	offCodedBlockFlag  = 85
	offSignificant     = 105
	offLastSignificant = 166
	offAbsLevel        = 227
)

var (
	catOffsetCodedBlockFlag = [5]int{0, 4, 8, 12, 16}
	catOffsetSignificant    = [5]int{0, 15, 29, 44, 47}
	catOffsetAbsLevel       = [5]int{0, 10, 20, 30, 39}
)

func (d *Decoder) CodedBlockFlag(cat, condTermA, condTermB int) bool {
	inc := condTermA + 2*condTermB
	return d.DecodeDecision(offCodedBlockFlag+catOffsetCodedBlockFlag[cat]+inc) == 1
}

func significanceInc(cat, idx, numC8x8 int) int {
	if cat == CatChromaDC {
		v := idx / numC8x8
		if v > 2 {
			v = 2
		}
		return v
	}
	return idx
}

func absLevelIncAt(base, limit, binIdx, numGt1, numEq1 int) int {
	if binIdx == 0 {
		if numGt1 != 0 {
			return base
		}
		v := 1 + numEq1
		if v > 4 {
			v = 4
		}
		return base + v
	}
	v := numGt1
	if v > limit {
		v = limit
	}
	return base + 5 + v
}

func absLevelBase(cat int) (base, limit int) {
	if cat == CatLuma8x8 {
		return offAbsLevel8x8, 4
	}
	if cat == CatChromaDC {
		return offAbsLevel + catOffsetAbsLevel[cat], 3
	}
	return offAbsLevel + catOffsetAbsLevel[cat], 4
}

// maxBlockCoeffs is the longest coefficient list of the categories handled here
// (the 4x4 blocks); the 8x8 block has its own table.
const maxBlockCoeffs = 16

// residualScans[cat][numC8x8-1][len(coeffs)-2] is the kernel's description of a
// block: the context of the significance and last flag of every coded position
// (Table 9-34 offsets, with the chroma DC increment min(i/numC8x8, 2)) and the
// base and limit of the level contexts. Built once, validated by NewScan.
var residualScans = buildResidualScans()

func buildResidualScans() (t [CatChromaAC + 1][2][maxBlockCoeffs - 1]*kcabac.Scan) {
	for cat := CatIntra16x16DC; cat <= CatChromaAC; cat++ {
		for n8 := 1; n8 <= 2; n8++ {
			for length := 2; length <= maxBlockCoeffs; length++ {
				sig := make([]uint16, length-1)
				last := make([]uint16, length-1)
				for i := range sig {
					inc := i
					if cat == CatChromaDC {
						inc = significanceInc(cat, i, n8)
					}
					sig[i] = uint16(offSignificant + catOffsetSignificant[cat] + inc)
					last[i] = uint16(offLastSignificant + catOffsetSignificant[cat] + inc)
				}
				base, limit := absLevelBase(cat)
				s, err := kcabac.NewScan(sig, last, base, limit)
				if err != nil {
					panic(err)
				}
				t[cat][n8-1][length-2] = s
			}
		}
	}
	return t
}

func (d *Decoder) ResidualBlock(coeffs []int32, cat, condTermA, condTermB, numC8x8 int) int {
	for i := range coeffs {
		coeffs[i] = 0
	}
	if !d.CodedBlockFlag(cat, condTermA, condTermB) {
		return 0
	}
	return kcabac.Decode(&d.st, (*kcabac.Contexts)(&d.ctx), residualScans[cat][numC8x8-1][len(coeffs)-2], coeffs)
}
