package syntax

import "testing"

func isPermutation(t *testing.T, name string, scan []uint8, n int) {
	t.Helper()
	seen := make([]bool, n)
	for i, v := range scan {
		if int(v) >= n {
			t.Fatalf("%s[%d] = %d out of range [0,%d)", name, i, v, n)
		}
		if seen[v] {
			t.Fatalf("%s: value %d repeated", name, v)
		}
		seen[v] = true
	}
	for i, ok := range seen {
		if !ok {
			t.Fatalf("%s: value %d never produced", name, i)
		}
	}
}

func TestZigzagScansArePermutations(t *testing.T) {
	isPermutation(t, "zigzagScan4x4", zigzagScan4x4[:], 16)
	isPermutation(t, "zigzagScan8x8", zigzagScan8x8[:], 64)
}

func TestToScanOrderRoundTrip(t *testing.T) {
	for i, p := range zigzagScan4x4 {
		if rasterDefaultScalingList4x4Intra[p] != scanDefaultScalingList4x4Intra[i] {
			t.Fatalf("4x4 intra scan[%d]: got %d want %d", i, scanDefaultScalingList4x4Intra[i], rasterDefaultScalingList4x4Intra[p])
		}
		if rasterDefaultScalingList4x4Inter[p] != scanDefaultScalingList4x4Inter[i] {
			t.Fatalf("4x4 inter scan[%d]: got %d want %d", i, scanDefaultScalingList4x4Inter[i], rasterDefaultScalingList4x4Inter[p])
		}
	}
	for i, p := range zigzagScan8x8 {
		if rasterDefaultScalingList8x8Intra[p] != scanDefaultScalingList8x8Intra[i] {
			t.Fatalf("8x8 intra scan[%d]: got %d want %d", i, scanDefaultScalingList8x8Intra[i], rasterDefaultScalingList8x8Intra[p])
		}
		if rasterDefaultScalingList8x8Inter[p] != scanDefaultScalingList8x8Inter[i] {
			t.Fatalf("8x8 inter scan[%d]: got %d want %d", i, scanDefaultScalingList8x8Inter[i], rasterDefaultScalingList8x8Inter[p])
		}
	}
}

func TestDefaultScalingListValuesInRange(t *testing.T) {
	check := func(name string, vals []uint8) {
		t.Helper()
		for i, v := range vals {
			if v < 1 {
				t.Fatalf("%s[%d] = %d out of range [1,255]", name, i, v)
			}
		}
	}
	check("rasterDefaultScalingList4x4Intra", rasterDefaultScalingList4x4Intra[:])
	check("rasterDefaultScalingList4x4Inter", rasterDefaultScalingList4x4Inter[:])
	check("rasterDefaultScalingList8x8Intra", rasterDefaultScalingList8x8Intra[:])
	check("rasterDefaultScalingList8x8Inter", rasterDefaultScalingList8x8Inter[:])
}

func TestDefaultScalingList8x8TransposeSymmetric(t *testing.T) {
	check := func(name string, m [64]uint8) {
		t.Helper()
		for row := 0; row < 8; row++ {
			for col := 0; col < 8; col++ {
				a := m[row*8+col]
				b := m[col*8+row]
				if a != b {
					t.Fatalf("%s not symmetric at (%d,%d): %d vs %d", name, row, col, a, b)
				}
			}
		}
	}
	check("rasterDefaultScalingList8x8Intra", rasterDefaultScalingList8x8Intra)
	check("rasterDefaultScalingList8x8Inter", rasterDefaultScalingList8x8Inter)
}

func flat4x4SPS(id uint32) *SPS {
	return &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: false,
		ID:                      id,
	}
}

func TestResolvedScalingListsSPSNoMatrixIsFlat(t *testing.T) {
	s := flat4x4SPS(0)
	l4, l8 := s.ResolvedScalingLists()
	for i := 0; i < 6; i++ {
		if l4[i] != flatScalingList4x4 {
			t.Fatalf("list4x4[%d] = %v, want flat", i, l4[i])
		}
	}
	for i := 0; i < 2; i++ {
		if l8[i] != flatScalingList8x8 {
			t.Fatalf("list8x8[%d] = %v, want flat", i, l8[i])
		}
	}
}

func TestResolvedScalingListsBaselineProfileIsFlat(t *testing.T) {
	s := &SPS{ProfileIDC: 77, ChromaFormatIDC: Chroma420}
	l4, l8 := s.ResolvedScalingLists()
	if l4[0] != flatScalingList4x4 || l8[0] != flatScalingList8x8 {
		t.Fatalf("baseline profile must resolve to flat scaling")
	}
}

func TestResolvedScalingListsSPSAllAbsentUsesFallbackChain(t *testing.T) {
	s := &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: true,
	}
	l4, l8 := s.ResolvedScalingLists()
	if l4[0] != scanDefaultScalingList4x4Intra {
		t.Fatalf("list4x4[0] should fall back to Default_4x4_Intra")
	}
	if l4[1] != l4[0] || l4[2] != l4[1] {
		t.Fatalf("list4x4[1],[2] should chain from list4x4[0]")
	}
	if l4[3] != scanDefaultScalingList4x4Inter {
		t.Fatalf("list4x4[3] should fall back to Default_4x4_Inter")
	}
	if l4[4] != l4[3] || l4[5] != l4[4] {
		t.Fatalf("list4x4[4],[5] should chain from list4x4[3]")
	}
	if l8[0] != scanDefaultScalingList8x8Intra {
		t.Fatalf("list8x8[0] should fall back to Default_8x8_Intra")
	}
	if l8[1] != scanDefaultScalingList8x8Inter {
		t.Fatalf("list8x8[1] should fall back to Default_8x8_Inter")
	}
}

func TestResolvedScalingListsSPSExplicitValuesUsed(t *testing.T) {
	s := &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: true,
	}
	s.ScalingList4x4Present[0] = true
	s.ScalingList4x4[0] = flatList4x4(1)
	s.ScalingList8x8Present[0] = true
	s.ScalingList8x8[0] = flatList8x8(5)

	l4, l8 := s.ResolvedScalingLists()
	if l4[0] != flatList4x4(1) {
		t.Fatalf("list4x4[0] should be the explicitly coded list")
	}
	if l4[1] != l4[0] {
		t.Fatalf("list4x4[1] should chain from the explicit list4x4[0]")
	}
	if l8[0] != flatList8x8(5) {
		t.Fatalf("list8x8[0] should be the explicitly coded list")
	}
}

func TestResolvedScalingListsSPSUseDefaultFlagUsesRealDefault(t *testing.T) {
	s := &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: true,
	}
	s.ScalingList4x4Present[2] = true
	s.UseDefaultScaling4x4[2] = true
	s.ScalingList4x4[2] = defaultList4x4()

	s.ScalingList8x8Present[1] = true
	s.UseDefaultScaling8x8[1] = true
	s.ScalingList8x8[1] = defaultList8x8()

	l4, l8 := s.ResolvedScalingLists()
	if l4[2] != scanDefaultScalingList4x4Intra {
		t.Fatalf("useDefaultScalingMatrixFlag at index 2 should resolve to Default_4x4_Intra, got %v", l4[2])
	}
	if l8[1] != scanDefaultScalingList8x8Inter {
		t.Fatalf("useDefaultScalingMatrixFlag at index 1 should resolve to Default_8x8_Inter, got %v", l8[1])
	}
}

func TestResolvedScalingLists444UsesSixEightByEightLists(t *testing.T) {
	s := &SPS{
		ProfileIDC:              244,
		ChromaFormatIDC:         Chroma444,
		SeqScalingMatrixPresent: true,
	}
	_, l8 := s.ResolvedScalingLists()
	if l8[2] != l8[0] {
		t.Fatalf("list8x8[2] (intra Cb) should fall back to list8x8[0] (intra Y)")
	}
	if l8[3] != l8[1] {
		t.Fatalf("list8x8[3] (inter Cb) should fall back to list8x8[1] (inter Y)")
	}
	if l8[4] != l8[2] {
		t.Fatalf("list8x8[4] (intra Cr) should fall back to list8x8[2] (intra Cb)")
	}
	if l8[5] != l8[3] {
		t.Fatalf("list8x8[5] (inter Cr) should fall back to list8x8[3] (inter Cb)")
	}
}

func TestResolvedScalingListsPPSInheritsFromSPS(t *testing.T) {
	sps := &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: true,
	}
	sps.ScalingList4x4Present[0] = true
	sps.ScalingList4x4[0] = flatList4x4(9)

	pps := &PPS{PicScalingMatrixPresent: false}
	l4, _ := pps.ResolvedScalingLists(sps)
	want, _ := sps.ResolvedScalingLists()
	if l4 != want {
		t.Fatalf("PPS without its own matrix should inherit the SPS-resolved lists exactly")
	}
}

func TestResolvedScalingListsPPSNoSPSMatrixIsFlat(t *testing.T) {
	sps := &SPS{ProfileIDC: 100, ChromaFormatIDC: Chroma420, SeqScalingMatrixPresent: false}
	pps := &PPS{PicScalingMatrixPresent: false}
	l4, l8 := pps.ResolvedScalingLists(sps)
	if l4[0] != flatScalingList4x4 || l8[0] != flatScalingList8x8 {
		t.Fatalf("PPS with no scaling info anywhere should resolve to flat")
	}
}

func TestResolvedScalingListsPPSFallsBackToSPSWhenAbsent(t *testing.T) {
	sps := &SPS{
		ProfileIDC:              100,
		ChromaFormatIDC:         Chroma420,
		SeqScalingMatrixPresent: true,
	}
	sps.ScalingList4x4Present[0] = true
	sps.ScalingList4x4[0] = flatList4x4(3)
	sps.ScalingList8x8Present[0] = true
	sps.ScalingList8x8[0] = flatList8x8(7)

	pps := &PPS{
		HasExtension:            true,
		Transform8x8Mode:        true,
		PicScalingMatrixPresent: true,
	}
	l4, l8 := pps.ResolvedScalingLists(sps)
	if l4[0] != flatList4x4(3) {
		t.Fatalf("pps list4x4[0] absent should fall back to sps list4x4[0], got %v", l4[0])
	}
	if l8[0] != flatList8x8(7) {
		t.Fatalf("pps list8x8[0] absent should fall back to sps list8x8[0], got %v", l8[0])
	}
}

func TestResolvedScalingListsPPSFallsBackToDefaultWhenSPSHasNoMatrix(t *testing.T) {
	sps := &SPS{ProfileIDC: 100, ChromaFormatIDC: Chroma420, SeqScalingMatrixPresent: false}
	pps := &PPS{
		HasExtension:            true,
		Transform8x8Mode:        true,
		PicScalingMatrixPresent: true,
	}
	l4, l8 := pps.ResolvedScalingLists(sps)
	if l4[0] != scanDefaultScalingList4x4Intra {
		t.Fatalf("pps list4x4[0] should fall back to Default_4x4_Intra when sps has no matrix, got %v", l4[0])
	}
	if l8[1] != scanDefaultScalingList8x8Inter {
		t.Fatalf("pps list8x8[1] should fall back to Default_8x8_Inter when sps has no matrix, got %v", l8[1])
	}
}

func TestResolvedScalingListsPPSWithoutTransform8x8SkipsEightByEight(t *testing.T) {
	sps := &SPS{ProfileIDC: 100, ChromaFormatIDC: Chroma420, SeqScalingMatrixPresent: false}
	pps := &PPS{
		HasExtension:            true,
		Transform8x8Mode:        false,
		PicScalingMatrixPresent: true,
	}
	_, l8 := pps.ResolvedScalingLists(sps)
	for i := 0; i < 6; i++ {
		if l8[i] != flatScalingList8x8 {
			t.Fatalf("list8x8[%d] should be flat when transform_8x8_mode is off, got %v", i, l8[i])
		}
	}
}
