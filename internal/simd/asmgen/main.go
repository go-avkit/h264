package main

import (
	"fmt"

	. "github.com/mmcloughlin/avo/build"
	. "github.com/mmcloughlin/avo/operand"
	"github.com/mmcloughlin/avo/reg"
)

func main() {
	ConstraintExpr("amd64,!purego")

	genForward4x4()
	genInverse4x4()
	genQuant4x4()
	genDequantLeft4x4()
	genDequantRight4x4()
	genAddResidual4x4()
	genDeblockLumaNormalAVX2()
	genDeblockLumaVerticalNormalAVX2()
	genDeblockLumaStrongAVX2()

	for _, size := range lumaMCSizes {
		genSixTapHoriz(size[0], size[1])
		genSixTapVert(size[0], size[1])
		genSixTapHV(size[0], size[1])
		if size[0] == 16 {
			genSixTapHorizAVX2(size[0], size[1])
			genSixTapVertAVX2(size[0], size[1])
			genSixTapHVAVX2(size[0], size[1])
		}
	}
	for _, size := range lumaMCSizes {
		genAvgBytes(size[0], size[1])
		genCopyBlock(size[0], size[1])
	}
	for _, size := range chromaMCSizes {
		genBilinearChroma(size[0], size[1])
	}

	Generate()
}

var lumaMCSizes = [][2]int{
	{16, 16}, {16, 8}, {8, 16}, {8, 8}, {8, 4}, {4, 8}, {4, 4},
}

var chromaMCSizes = [][2]int{
	{8, 8}, {8, 4}, {8, 2}, {4, 8}, {4, 4}, {4, 2},
}

func vAdd(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PADDD(b, d)
	return d
}

func vSub(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PSUBL(b, d)
	return d
}

func vDouble(a reg.VecVirtual) reg.VecVirtual {
	return vAdd(a, a)
}

func vHalf(a reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PSRAL(Imm(1), d)
	return d
}

func loadBlockRows(ptr reg.Register) (r0, r1, r2, r3 reg.VecVirtual) {
	r0 = XMM()
	MOVOU(Mem{Base: ptr}, r0)
	r1 = XMM()
	MOVOU(Mem{Base: ptr, Disp: 16}, r1)
	r2 = XMM()
	MOVOU(Mem{Base: ptr, Disp: 32}, r2)
	r3 = XMM()
	MOVOU(Mem{Base: ptr, Disp: 48}, r3)
	return
}

func storeBlockRows(ptr reg.Register, r0, r1, r2, r3 reg.VecVirtual) {
	MOVOU(r0, Mem{Base: ptr})
	MOVOU(r1, Mem{Base: ptr, Disp: 16})
	MOVOU(r2, Mem{Base: ptr, Disp: 32})
	MOVOU(r3, Mem{Base: ptr, Disp: 48})
}

func transpose4x4(r0, r1, r2, r3 reg.VecVirtual) (c0, c1, c2, c3 reg.VecVirtual) {
	t0 := XMM()
	MOVOU(r0, t0)
	PUNPCKLLQ(r1, t0)
	t1 := XMM()
	MOVOU(r0, t1)
	PUNPCKHLQ(r1, t1)
	t2 := XMM()
	MOVOU(r2, t2)
	PUNPCKLLQ(r3, t2)
	t3 := XMM()
	MOVOU(r2, t3)
	PUNPCKHLQ(r3, t3)

	c0 = XMM()
	MOVOU(t0, c0)
	PUNPCKLQDQ(t2, c0)
	c1 = XMM()
	MOVOU(t0, c1)
	PUNPCKHQDQ(t2, c1)
	c2 = XMM()
	MOVOU(t1, c2)
	PUNPCKLQDQ(t3, c2)
	c3 = XMM()
	MOVOU(t1, c3)
	PUNPCKHQDQ(t3, c3)
	return
}

func genForward4x4() {
	TEXT("forward4x4", NOSPLIT, "func(b *[16]int32)")
	Pragma("noescape")
	Doc("")
	ptr := Load(Param("b"), GP64())

	r0, r1, r2, r3 := loadBlockRows(ptr)
	c0, c1, c2, c3 := transpose4x4(r0, r1, r2, r3)

	o0, o1, o2, o3 := forwardButterfly(c0, c1, c2, c3)
	m0, m1, m2, m3 := transpose4x4(o0, o1, o2, o3)
	f0, f1, f2, f3 := forwardButterfly(m0, m1, m2, m3)

	storeBlockRows(ptr, f0, f1, f2, f3)
	RET()
}

func forwardButterfly(s0, s1, s2, s3 reg.VecVirtual) (o0, o1, o2, o3 reg.VecVirtual) {
	t0 := vAdd(s0, s3)
	t1 := vAdd(s1, s2)
	t2 := vSub(s1, s2)
	t3 := vSub(s0, s3)

	o0 = vAdd(t0, t1)
	o1 = vAdd(vDouble(t3), t2)
	o2 = vSub(t0, t1)
	o3 = vSub(t3, vDouble(t2))
	return
}

func genInverse4x4() {
	TEXT("inverse4x4", NOSPLIT, "func(b *[16]int32)")
	Pragma("noescape")
	Doc("")
	ptr := Load(Param("b"), GP64())

	r0, r1, r2, r3 := loadBlockRows(ptr)
	c0, c1, c2, c3 := transpose4x4(r0, r1, r2, r3)

	o0, o1, o2, o3 := inverseRowButterfly(c0, c1, c2, c3)
	m0, m1, m2, m3 := transpose4x4(o0, o1, o2, o3)
	f0, f1, f2, f3 := inverseColumnButterfly(m0, m1, m2, m3)

	storeBlockRows(ptr, f0, f1, f2, f3)
	RET()
}

func inverseRowButterfly(s0, s1, s2, s3 reg.VecVirtual) (o0, o1, o2, o3 reg.VecVirtual) {
	t0 := vAdd(s0, s2)
	t1 := vSub(s0, s2)
	t2 := vSub(vHalf(s1), s3)
	t3 := vAdd(s1, vHalf(s3))

	o0 = vAdd(t0, t3)
	o1 = vAdd(t1, t2)
	o2 = vSub(t1, t2)
	o3 = vSub(t0, t3)
	return
}

func broadcastImm32(v uint32) reg.VecVirtual {
	g := GP32()
	MOVL(U32(v), g)
	x := XMM()
	MOVD(g, x)
	PSHUFD(Imm(0), x, x)
	return x
}

func inverseColumnButterfly(s0, s1, s2, s3 reg.VecVirtual) (o0, o1, o2, o3 reg.VecVirtual) {
	t0 := vAdd(s0, s2)
	t1 := vSub(s0, s2)
	t2 := vSub(vHalf(s1), s3)
	t3 := vAdd(s1, vHalf(s3))

	round := broadcastImm32(32)
	round4x4 := func(v reg.VecVirtual) reg.VecVirtual {
		d := XMM()
		MOVOU(v, d)
		PADDD(round, d)
		PSRAL(Imm(6), d)
		return d
	}

	o0 = round4x4(vAdd(t0, t3))
	o1 = round4x4(vAdd(t1, t2))
	o2 = round4x4(vSub(t1, t2))
	o3 = round4x4(vSub(t0, t3))
	return
}

func genQuant4x4() {
	TEXT("quant4x4", NOSPLIT, "func(b *[16]int32, mf *[16]int32, f int32, qbits uint64)")
	Pragma("noescape")
	Doc("")
	bPtr := Load(Param("b"), GP64())
	mfPtr := Load(Param("mf"), GP64())
	fGP := Load(Param("f"), GP32())
	qbitsGP := Load(Param("qbits"), GP64())

	fVec := XMM()
	MOVD(fGP, fVec)
	PSHUFD(Imm(0), fVec, fVec)

	countVec := XMM()
	MOVQ(qbitsGP, countVec)

	for i := 0; i < 4; i++ {
		disp := i * 16
		bv := XMM()
		MOVOU(Mem{Base: bPtr, Disp: disp}, bv)
		mfv := XMM()
		MOVOU(Mem{Base: mfPtr, Disp: disp}, mfv)

		absv := XMM()
		PABSD(bv, absv)
		PMULLD(mfv, absv)
		PADDD(fVec, absv)
		PSRAL(countVec, absv)
		PSIGND(bv, absv)

		MOVOU(absv, Mem{Base: bPtr, Disp: disp})
	}
	RET()
}

func genDequantLeft4x4() {
	TEXT("dequantLeft4x4", NOSPLIT, "func(b *[16]int32, scale *[16]int32, shift uint64)")
	Pragma("noescape")
	Doc("")
	bPtr := Load(Param("b"), GP64())
	scalePtr := Load(Param("scale"), GP64())
	shiftGP := Load(Param("shift"), GP64())

	countVec := XMM()
	MOVQ(shiftGP, countVec)

	for i := 0; i < 4; i++ {
		disp := i * 16
		bv := XMM()
		MOVOU(Mem{Base: bPtr, Disp: disp}, bv)
		sv := XMM()
		MOVOU(Mem{Base: scalePtr, Disp: disp}, sv)

		PMULLD(sv, bv)
		PSLLL(countVec, bv)

		MOVOU(bv, Mem{Base: bPtr, Disp: disp})
	}
	RET()
}

func genDequantRight4x4() {
	TEXT("dequantRight4x4", NOSPLIT, "func(b *[16]int32, scale *[16]int32, shift uint64, round int32)")
	Pragma("noescape")
	Doc("")
	bPtr := Load(Param("b"), GP64())
	scalePtr := Load(Param("scale"), GP64())
	shiftGP := Load(Param("shift"), GP64())
	roundGP := Load(Param("round"), GP32())

	countVec := XMM()
	MOVQ(shiftGP, countVec)

	roundVec := XMM()
	MOVD(roundGP, roundVec)
	PSHUFD(Imm(0), roundVec, roundVec)

	for i := 0; i < 4; i++ {
		disp := i * 16
		bv := XMM()
		MOVOU(Mem{Base: bPtr, Disp: disp}, bv)
		sv := XMM()
		MOVOU(Mem{Base: scalePtr, Disp: disp}, sv)

		PMULLD(sv, bv)
		PADDD(roundVec, bv)
		PSRAL(countVec, bv)

		MOVOU(bv, Mem{Base: bPtr, Disp: disp})
	}
	RET()
}

func genAddResidual4x4() {
	TEXT("addResidual4x4", NOSPLIT, "func(plane []byte, stride int, b *[16]int32)")
	Pragma("noescape")
	Doc("")
	planePtr := Load(Param("plane").Base(), GP64())
	strideGP := Load(Param("stride"), GP64())
	bPtr := Load(Param("b"), GP64())

	for y := 0; y < 4; y++ {
		wide := XMM()
		PMOVZXBD(Mem{Base: planePtr}, wide)

		res := XMM()
		MOVOU(Mem{Base: bPtr, Disp: y * 16}, res)
		PADDD(res, wide)

		words := XMM()
		MOVOU(wide, words)
		PACKSSLW(words, words)

		bytes := XMM()
		MOVOU(words, bytes)
		PACKUSWB(bytes, bytes)

		out := GP32()
		MOVD(bytes, out)
		MOVL(out, Mem{Base: planePtr})

		if y != 3 {
			ADDQ(strideGP, planePtr)
		}
	}
	RET()
}

func loadDW4At(ptr reg.Register, disp int) reg.VecVirtual {
	v := XMM()
	if disp == 0 {
		PMOVZXBD(Mem{Base: ptr}, v)
	} else {
		PMOVZXBD(Mem{Base: ptr, Disp: disp}, v)
	}
	return v
}

func memAt(p reg.Register, d int) Mem {
	if d == 0 {
		return Mem{Base: p}
	}
	return Mem{Base: p, Disp: d}
}

func wAdd(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PADDW(b, d)
	return d
}

func wSub(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PSUBW(b, d)
	return d
}

func wMul(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PMULLW(b, d)
	return d
}

func dAdd(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PADDD(b, d)
	return d
}

type tapLoader func(k int) reg.VecVirtual

func hTapLoader(ptr reg.Register, x, n int) tapLoader {
	if n >= 8 {
		return func(k int) reg.VecVirtual {
			v := XMM()
			PMOVZXBW(memAt(ptr, x-2+k), v)
			return v
		}
	}
	a := XMM()
	PMOVZXBW(memAt(ptr, x-2), a)
	b := XMM()
	PMOVZXBW(memAt(ptr, x-1), b)
	return func(k int) reg.VecVirtual {
		v := XMM()
		if k == 5 {
			MOVOU(b, v)
			PSRLDQ(Imm(8), v)
			return v
		}
		MOVOU(a, v)
		if k != 0 {
			PSRLDQ(Imm(uint64(2*k)), v)
		}
		return v
	}
}

func vTapLoader(rows []reg.Register, x, n int) tapLoader {
	return func(k int) reg.VecVirtual {
		v := XMM()
		if n >= 8 {
			PMOVZXBW(memAt(rows[k], x), v)
			return v
		}
		MOVD(memAt(rows[k], x), v)
		PMOVZXBW(v, v)
		return v
	}
}

func tapSum6W(load tapLoader, c20, c5 reg.VecVirtual) reg.VecVirtual {
	s := wAdd(load(0), load(5))
	s = wAdd(s, wMul(wAdd(load(2), load(3)), c20))
	return wSub(s, wMul(wAdd(load(1), load(4)), c5))
}

func roundPackW(v, round reg.VecVirtual, sh int) reg.VecVirtual {
	d := XMM()
	MOVOU(v, d)
	PADDW(round, d)
	PSRAW(Imm(uint64(sh)), d)
	p := XMM()
	MOVOU(d, p)
	PACKUSWB(p, p)
	return p
}

func storeBytesN(v reg.VecVirtual, ptr reg.Register, disp, n int) {
	if n >= 8 {
		MOVQ(v, memAt(ptr, disp))
		return
	}
	storeBytes4(v, ptr, disp)
}

func hvPairSum(win []reg.VecVirtual, c1m5, c2020, cm51 reg.VecVirtual, high bool) reg.VecVirtual {
	mk := func(a, b, coef reg.VecVirtual) reg.VecVirtual {
		d := XMM()
		MOVOU(a, d)
		if high {
			PUNPCKHWL(b, d)
		} else {
			PUNPCKLWL(b, d)
		}
		PMADDWL(coef, d)
		return d
	}
	s := dAdd(mk(win[0], win[1], c1m5), mk(win[2], win[3], c2020))
	return dAdd(s, mk(win[4], win[5], cm51))
}

func hvRoundPack(lo, hi, round reg.VecVirtual) reg.VecVirtual {
	rl := XMM()
	MOVOU(lo, rl)
	PADDD(round, rl)
	PSRAL(Imm(10), rl)
	rh := XMM()
	MOVOU(hi, rh)
	PADDD(round, rh)
	PSRAL(Imm(10), rh)
	PACKSSLW(rh, rl)
	PACKUSWB(rl, rl)
	return rl
}

const (
	packed20  = 20 | 20<<16
	packed5   = 5 | 5<<16
	packed16  = 16 | 16<<16
	coef1m5   = 1 | 0xFFFB<<16
	coef2020  = 20 | 20<<16
	coefm51   = 0xFFFB | 1<<16
	roundHV32 = 512
)

func stripWidth(w int) int {
	if w > 8 {
		return 8
	}
	return w
}

func ybroadcastImm32(v uint32) reg.VecVirtual {
	g := GP32()
	MOVL(U32(v), g)
	x := XMM()
	VMOVD(g, x)
	y := YMM()
	VPBROADCASTD(x, y)
	return y
}

func yAddW(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPADDW(a, b, d)
	return d
}

func ySubW(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPSUBW(b, a, d)
	return d
}

func yMulW(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPMULLW(a, b, d)
	return d
}

func yAddD(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPADDD(a, b, d)
	return d
}

func yHTapLoader(ptr reg.Register, x int) tapLoader {
	return func(k int) reg.VecVirtual {
		v := YMM()
		VPMOVZXBW(memAt(ptr, x-2+k), v)
		return v
	}
}

func yVTapLoader(rows []reg.Register, x int) tapLoader {
	return func(k int) reg.VecVirtual {
		v := YMM()
		VPMOVZXBW(memAt(rows[k], x), v)
		return v
	}
}

func yTapSum6W(load tapLoader, c20, c5 reg.VecVirtual) reg.VecVirtual {
	s := yAddW(load(0), load(5))
	s = yAddW(s, yMulW(yAddW(load(2), load(3)), c20))
	return ySubW(s, yMulW(yAddW(load(1), load(4)), c5))
}

func yStore16(v reg.VecVirtual, ptr reg.Register, disp int) {
	p := YMM()
	VPACKUSWB(v, v, p)
	VPERMQ(Imm(8), p, p)
	VMOVDQU(p.AsX(), memAt(ptr, disp))
}

func yRoundPackStore(v, round reg.VecVirtual, sh int, ptr reg.Register, disp int) {
	d := YMM()
	VPADDW(round, v, d)
	VPSRAW(Imm(uint64(sh)), d, d)
	yStore16(d, ptr, disp)
}

func yHVPairSum(win []reg.VecVirtual, c1m5, c2020, cm51 reg.VecVirtual, high bool) reg.VecVirtual {
	mk := func(a, b, coef reg.VecVirtual) reg.VecVirtual {
		d := YMM()
		if high {
			VPUNPCKHWD(b, a, d)
		} else {
			VPUNPCKLWD(b, a, d)
		}
		e := YMM()
		VPMADDWD(coef, d, e)
		return e
	}
	s := yAddD(mk(win[0], win[1], c1m5), mk(win[2], win[3], c2020))
	return yAddD(s, mk(win[4], win[5], cm51))
}

func yHVRoundPackStore(lo, hi, round reg.VecVirtual, ptr reg.Register, disp int) {
	rl := YMM()
	VPADDD(round, lo, rl)
	VPSRAD(Imm(10), rl, rl)
	rh := YMM()
	VPADDD(round, hi, rh)
	VPSRAD(Imm(10), rh, rh)
	w := YMM()
	VPACKSSDW(rh, rl, w)
	yStore16(w, ptr, disp)
}

func genSixTapHorizAVX2(w, h int) {
	name := fmt.Sprintf("sixTapHoriz%dx%dAVX2", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := ybroadcastImm32(packed20)
	c5 := ybroadcastImm32(packed5)
	c16 := ybroadcastImm32(packed16)

	for y := 0; y < h; y++ {
		raw := yTapSum6W(yHTapLoader(src, 0), c20, c5)
		yRoundPackStore(raw, c16, 5, dst, 0)
		if y != h-1 {
			ADDQ(srcStride, src)
			ADDQ(dstStride, dst)
		}
	}
	VZEROUPPER()
	RET()
}

func genSixTapVertAVX2(w, h int) {
	name := fmt.Sprintf("sixTapVert%dx%dAVX2", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := ybroadcastImm32(packed20)
	c5 := ybroadcastImm32(packed5)
	c16 := ybroadcastImm32(packed16)

	m2, m1, z0, p1, p2, p3 := sixRowPointers(src, srcStride)
	rows := []reg.Register{m2, m1, z0, p1, p2, p3}

	for y := 0; y < h; y++ {
		raw := yTapSum6W(yVTapLoader(rows, 0), c20, c5)
		yRoundPackStore(raw, c16, 5, dst, 0)
		if y != h-1 {
			advanceSixRowPointers(srcStride, m2, m1, z0, p1, p2, p3)
			ADDQ(dstStride, dst)
		}
	}
	VZEROUPPER()
	RET()
}

func genSixTapHVAVX2(w, h int) {
	name := fmt.Sprintf("sixTapHV%dx%dAVX2", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := ybroadcastImm32(packed20)
	c5 := ybroadcastImm32(packed5)
	c1m5 := ybroadcastImm32(coef1m5)
	c2020 := ybroadcastImm32(coef2020)
	cm51 := ybroadcastImm32(coefm51)
	c512 := ybroadcastImm32(roundHV32)

	p := GP64()
	MOVQ(src, p)
	SUBQ(srcStride, p)
	SUBQ(srcStride, p)

	win := make([]reg.VecVirtual, 6)
	for k := 0; k < 6; k++ {
		win[k] = yTapSum6W(yHTapLoader(p, 0), c20, c5)
		if k != 5 {
			ADDQ(srcStride, p)
		}
	}
	for y := 0; y < h; y++ {
		lo := yHVPairSum(win, c1m5, c2020, cm51, false)
		hi := yHVPairSum(win, c1m5, c2020, cm51, true)
		yHVRoundPackStore(lo, hi, c512, dst, 0)
		if y != h-1 {
			ADDQ(srcStride, p)
			copy(win, win[1:])
			win[5] = yTapSum6W(yHTapLoader(p, 0), c20, c5)
			ADDQ(dstStride, dst)
		}
	}
	VZEROUPPER()
	RET()
}

func clipRoundDW(raw reg.VecVirtual, round int32, shiftN int) reg.VecVirtual {
	v := XMM()
	MOVOU(raw, v)
	roundVec := broadcastImm32(uint32(round))
	PADDD(roundVec, v)
	PSRAL(Imm(uint64(shiftN)), v)
	return v
}

func packClip4(v reg.VecVirtual) reg.VecVirtual {
	words := XMM()
	MOVOU(v, words)
	PACKSSLW(words, words)
	bytes := XMM()
	MOVOU(words, bytes)
	PACKUSWB(bytes, bytes)
	return bytes
}

func storeBytes4(v reg.VecVirtual, ptr reg.Register, disp int) {
	out := GP32()
	MOVD(v, out)
	if disp == 0 {
		MOVL(out, Mem{Base: ptr})
	} else {
		MOVL(out, Mem{Base: ptr, Disp: disp})
	}
}

func genSixTapHoriz(w, h int) {
	name := fmt.Sprintf("sixTapHoriz%dx%d", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := broadcastImm32(packed20)
	c5 := broadcastImm32(packed5)
	c16 := broadcastImm32(packed16)
	n := stripWidth(w)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x += n {
			raw := tapSum6W(hTapLoader(src, x, n), c20, c5)
			storeBytesN(roundPackW(raw, c16, 5), dst, x, n)
		}
		if y != h-1 {
			ADDQ(srcStride, src)
			ADDQ(dstStride, dst)
		}
	}
	RET()
}

func sixRowPointers(base reg.Register, stride reg.Register) (m2, m1, z0, p1, p2, p3 reg.Register) {
	m2 = GP64()
	MOVQ(base, m2)
	SUBQ(stride, m2)
	SUBQ(stride, m2)
	m1 = GP64()
	MOVQ(base, m1)
	SUBQ(stride, m1)
	z0 = base
	p1 = GP64()
	MOVQ(base, p1)
	ADDQ(stride, p1)
	p2 = GP64()
	MOVQ(base, p2)
	ADDQ(stride, p2)
	ADDQ(stride, p2)
	p3 = GP64()
	MOVQ(base, p3)
	ADDQ(stride, p3)
	ADDQ(stride, p3)
	ADDQ(stride, p3)
	return
}

func advanceSixRowPointers(stride reg.Register, ptrs ...reg.Register) {
	for _, p := range ptrs {
		ADDQ(stride, p)
	}
}

func genSixTapVert(w, h int) {
	name := fmt.Sprintf("sixTapVert%dx%d", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := broadcastImm32(packed20)
	c5 := broadcastImm32(packed5)
	c16 := broadcastImm32(packed16)
	n := stripWidth(w)

	m2, m1, z0, p1, p2, p3 := sixRowPointers(src, srcStride)
	rows := []reg.Register{m2, m1, z0, p1, p2, p3}

	for y := 0; y < h; y++ {
		for x := 0; x < w; x += n {
			raw := tapSum6W(vTapLoader(rows, x, n), c20, c5)
			storeBytesN(roundPackW(raw, c16, 5), dst, x, n)
		}
		if y != h-1 {
			advanceSixRowPointers(srcStride, m2, m1, z0, p1, p2, p3)
			ADDQ(dstStride, dst)
		}
	}
	RET()
}

func genSixTapHV(w, h int) {
	name := fmt.Sprintf("sixTapHV%dx%d", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	c20 := broadcastImm32(packed20)
	c5 := broadcastImm32(packed5)
	c1m5 := broadcastImm32(coef1m5)
	c2020 := broadcastImm32(coef2020)
	cm51 := broadcastImm32(coefm51)
	c512 := broadcastImm32(roundHV32)
	n := stripWidth(w)

	for x := 0; x < w; x += n {
		p := GP64()
		MOVQ(src, p)
		SUBQ(srcStride, p)
		SUBQ(srcStride, p)
		dp := GP64()
		MOVQ(dst, dp)

		win := make([]reg.VecVirtual, 6)
		for k := 0; k < 6; k++ {
			win[k] = tapSum6W(hTapLoader(p, x, n), c20, c5)
			if k != 5 {
				ADDQ(srcStride, p)
			}
		}
		for y := 0; y < h; y++ {
			lo := hvPairSum(win, c1m5, c2020, cm51, false)
			hi := lo
			if n >= 8 {
				hi = hvPairSum(win, c1m5, c2020, cm51, true)
			}
			storeBytesN(hvRoundPack(lo, hi, c512), dp, x, n)
			if y != h-1 {
				ADDQ(srcStride, p)
				copy(win, win[1:])
				win[5] = tapSum6W(hTapLoader(p, x, n), c20, c5)
				ADDQ(dstStride, dp)
			}
		}
	}
	RET()
}

func mulDW(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	MOVOU(a, d)
	PMULLD(b, d)
	return d
}

func genBilinearChroma(w, h int) {
	name := fmt.Sprintf("bilinearChroma%dx%d", w, h)
	TEXT(name, NOSPLIT, "func(dst []byte, dstStride int, src []byte, srcStride int, xFrac, yFrac int32)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())
	xF := Load(Param("xFrac"), GP32())
	yF := Load(Param("yFrac"), GP32())

	invX := GP32()
	MOVL(U32(8), invX)
	SUBL(xF, invX)
	invY := GP32()
	MOVL(U32(8), invY)
	SUBL(yF, invY)

	w00 := GP32()
	MOVL(invX, w00)
	IMULL(invY, w00)
	w10 := GP32()
	MOVL(xF, w10)
	IMULL(invY, w10)
	w01 := GP32()
	MOVL(invX, w01)
	IMULL(yF, w01)
	w11 := GP32()
	MOVL(xF, w11)
	IMULL(yF, w11)

	w00v := broadcastGP32(w00)
	w10v := broadcastGP32(w10)
	w01v := broadcastGP32(w01)
	w11v := broadcastGP32(w11)

	srcNext := GP64()
	MOVQ(src, srcNext)
	ADDQ(srcStride, srcNext)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x += 4 {
			a := loadDW4At(src, x)
			b := loadDW4At(src, x+1)
			c := loadDW4At(srcNext, x)
			d := loadDW4At(srcNext, x+1)
			sum := vAdd(vAdd(mulDW(a, w00v), mulDW(b, w10v)), vAdd(mulDW(c, w01v), mulDW(d, w11v)))
			rounded := clipRoundDW(sum, 32, 6)
			bytes := packClip4(rounded)
			storeBytes4(bytes, dst, x)
		}
		if y != h-1 {
			ADDQ(srcStride, src)
			ADDQ(srcStride, srcNext)
			ADDQ(dstStride, dst)
		}
	}
	RET()
}

func broadcastGP32(g reg.Register) reg.VecVirtual {
	x := XMM()
	MOVD(g, x)
	PSHUFD(Imm(0), x, x)
	return x
}

func yBroadcast(v uint32) reg.VecVirtual {
	g := GP32()
	MOVL(U32(v), g)
	x := XMM()
	VMOVD(g, x)
	y := YMM()
	VPBROADCASTW(x, y)
	return y
}

func yBroadcastReg(g reg.GPVirtual) reg.VecVirtual {
	x := XMM()
	VMOVD(g.As32(), x)
	y := YMM()
	VPBROADCASTW(x, y)
	return y
}

func yAdd(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPADDW(a, b, d)
	return d
}

func ySub(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPSUBW(b, a, d)
	return d
}

func yAbs(a reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPABSW(a, d)
	return d
}

func yGreater(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPCMPGTW(b, a, d)
	return d
}

func yEqual(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPCMPEQW(a, b, d)
	return d
}

func yAnd(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPAND(a, b, d)
	return d
}

func yShl(a reg.VecVirtual, n uint64) reg.VecVirtual {
	d := YMM()
	VPSLLW(Imm(n), a, d)
	return d
}

func yShr(a reg.VecVirtual, n uint64) reg.VecVirtual {
	d := YMM()
	VPSRAW(Imm(n), a, d)
	return d
}

func yMin(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPMINSW(a, b, d)
	return d
}

func yMax(a, b reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPMAXSW(a, b, d)
	return d
}

func ySelect(mask, ifTrue, ifFalse reg.VecVirtual) reg.VecVirtual {
	d := YMM()
	VPBLENDVB(mask, ifTrue, ifFalse, d)
	return d
}

func yClip(a, limit, negLimit reg.VecVirtual) reg.VecVirtual {
	return yMax(yMin(a, limit), negLimit)
}

func genDeblockLumaNormalAVX2() {
	TEXT("deblockLumaNormalAVX2", NOSPLIT,
		"func(plane []byte, offset int, stride int, tc0 *[16]uint8, bs *[16]uint8, alpha int32, beta int32)")
	Pragma("noescape")
	Doc("")
	base := Load(Param("plane").Base(), GP64())
	offset := Load(Param("offset"), GP64())
	stride := Load(Param("stride"), GP64())
	tc0Ptr := Load(Param("tc0"), GP64())
	bsPtr := Load(Param("bs"), GP64())
	alphaGP := Load(Param("alpha"), GP32())
	betaGP := Load(Param("beta"), GP32())
	ADDQ(offset, base)

	rowPtr := GP64()
	MOVQ(base, rowPtr)
	back := GP64()
	MOVQ(stride, back)
	IMUL3Q(Imm(3), back, back)
	SUBQ(back, rowPtr)

	rows := make([]reg.VecVirtual, 6)
	for i := range rows {
		v := YMM()
		VPMOVZXBW(Mem{Base: rowPtr}, v)
		rows[i] = v
		if i != len(rows)-1 {
			ADDQ(stride, rowPtr)
		}
	}
	p2, p1, p0, q0, q1, q2 := rows[0], rows[1], rows[2], rows[3], rows[4], rows[5]

	outP1, outP0, outQ0, outQ1 := lumaNormalOutputs(p2, p1, p0, q0, q1, q2,
		Mem{Base: tc0Ptr}, Mem{Base: bsPtr}, alphaGP.(reg.GPVirtual), betaGP.(reg.GPVirtual))

	storePtr := GP64()
	MOVQ(base, storePtr)
	back2 := GP64()
	MOVQ(stride, back2)
	SHLQ(Imm(1), back2)
	SUBQ(back2, storePtr)
	for _, v := range []reg.VecVirtual{outP1, outP0, outQ0, outQ1} {
		yStoreBytes(v, storePtr)
		ADDQ(stride, storePtr)
	}
	VZEROUPPER()
	RET()
}

func yStoreBytes(v reg.VecVirtual, ptr reg.Register) {
	packed := YMM()
	VPACKUSWB(v, v, packed)
	ordered := YMM()
	VPERMQ(Imm(0xD8), packed, ordered)
	VMOVDQU(ordered.AsX(), Mem{Base: ptr})
}

func genDeblockLumaStrongAVX2() {
	TEXT("deblockLumaStrongAVX2", NOSPLIT,
		"func(plane []byte, offset int, stride int, alpha int32, beta int32)")
	Pragma("noescape")
	Doc("")
	base := Load(Param("plane").Base(), GP64())
	offset := Load(Param("offset"), GP64())
	stride := Load(Param("stride"), GP64())
	alphaGP := Load(Param("alpha"), GP32())
	betaGP := Load(Param("beta"), GP32())
	ADDQ(offset, base)

	rowPtr := GP64()
	MOVQ(base, rowPtr)
	back := GP64()
	MOVQ(stride, back)
	SHLQ(Imm(2), back)
	SUBQ(back, rowPtr)

	rows := make([]reg.VecVirtual, 8)
	for i := range rows {
		v := YMM()
		VPMOVZXBW(Mem{Base: rowPtr}, v)
		rows[i] = v
		if i != len(rows)-1 {
			ADDQ(stride, rowPtr)
		}
	}
	p3, p2, p1, p0 := rows[0], rows[1], rows[2], rows[3]
	q0, q1, q2, q3 := rows[4], rows[5], rows[6], rows[7]

	edge, strongP, strongQ := func() (reg.VecVirtual, reg.VecVirtual, reg.VecVirtual) {
		alphaV := yBroadcastReg(alphaGP.(reg.GPVirtual))
		betaV := yBroadcastReg(betaGP.(reg.GPVirtual))
		gap := yAbs(ySub(p0, q0))
		e := yAnd(yGreater(alphaV, gap), yGreater(betaV, yAbs(ySub(p1, p0))))
		e = yAnd(e, yGreater(betaV, yAbs(ySub(q1, q0))))
		narrow := yGreater(yAdd(yShr(alphaV, 2), yBroadcast(2)), gap)
		sp := yAnd(yAnd(e, narrow), yGreater(betaV, yAbs(ySub(p2, p0))))
		sq := yAnd(yAnd(e, narrow), yGreater(betaV, yAbs(ySub(q2, q0))))
		return e, sp, sq
	}()

	four := yBroadcast(4)
	two := yBroadcast(2)
	triple := func(v reg.VecVirtual) reg.VecVirtual { return yAdd(yShl(v, 1), v) }

	at := func(k int) reg.Register {
		p := GP64()
		MOVQ(base, p)
		if k != 0 {
			step := GP64()
			MOVQ(stride, step)
			n := k
			if n < 0 {
				n = -n
			}
			if n != 1 {
				IMUL3Q(Imm(uint64(n)), step, step)
			}
			if k < 0 {
				SUBQ(step, p)
			} else {
				ADDQ(step, p)
			}
		}
		return p
	}

	yStoreBytes(ySelect(strongP,
		yShr(yAdd(yAdd(yShl(p3, 1), triple(p2)), yAdd(yAdd(p1, p0), yAdd(q0, four))), 3), p2), at(-3))
	yStoreBytes(ySelect(strongQ,
		yShr(yAdd(yAdd(yShl(q3, 1), triple(q2)), yAdd(yAdd(q1, q0), yAdd(p0, four))), 3), q2), at(2))

	yStoreBytes(ySelect(strongP, yShr(yAdd(yAdd(yAdd(p2, p1), yAdd(p0, q0)), two), 2), p1), at(-2))
	yStoreBytes(ySelect(strongQ, yShr(yAdd(yAdd(yAdd(q2, q1), yAdd(q0, p0)), two), 2), q1), at(1))

	wideP0 := yShr(yAdd(yAdd(yAdd(p2, yShl(p1, 1)), yAdd(yShl(p0, 1), yShl(q0, 1))), yAdd(q1, four)), 3)
	thinP0 := yShr(yAdd(yAdd(yShl(p1, 1), p0), yAdd(q1, two)), 2)
	yStoreBytes(ySelect(edge, ySelect(strongP, wideP0, thinP0), p0), at(-1))

	wideQ0 := yShr(yAdd(yAdd(yAdd(q2, yShl(q1, 1)), yAdd(yShl(q0, 1), yShl(p0, 1))), yAdd(p1, four)), 3)
	thinQ0 := yShr(yAdd(yAdd(yShl(q1, 1), q0), yAdd(p1, two)), 2)
	yStoreBytes(ySelect(edge, ySelect(strongQ, wideQ0, thinQ0), q0), at(0))

	VZEROUPPER()
	RET()
}

func xLowBytes(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKLBW(b, a, d)
	return d
}

func xHighBytes(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKHBW(b, a, d)
	return d
}

func xLowWords(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKLWD(b, a, d)
	return d
}

func xHighWords(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKHWD(b, a, d)
	return d
}

func xLowLongs(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKLDQ(b, a, d)
	return d
}

func xHighLongs(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKHDQ(b, a, d)
	return d
}

func xLowQuads(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKLQDQ(b, a, d)
	return d
}

func xHighQuads(a, b reg.VecVirtual) reg.VecVirtual {
	d := XMM()
	VPUNPCKHQDQ(b, a, d)
	return d
}

func transposeEightRows(rows []reg.VecVirtual) [4]reg.VecVirtual {
	a0 := xLowBytes(rows[0], rows[1])
	a1 := xLowBytes(rows[2], rows[3])
	a2 := xLowBytes(rows[4], rows[5])
	a3 := xLowBytes(rows[6], rows[7])
	b0 := xLowWords(a0, a1)
	b1 := xHighWords(a0, a1)
	b2 := xLowWords(a2, a3)
	b3 := xHighWords(a2, a3)
	return [4]reg.VecVirtual{
		xLowLongs(b0, b2),
		xHighLongs(b0, b2),
		xLowLongs(b1, b3),
		xHighLongs(b1, b3),
	}
}

func loadSampleColumns(base, stride reg.Register, want []int) []reg.VecVirtual {
	rowPtr := GP64()
	MOVQ(base, rowPtr)
	halves := make([][4]reg.VecVirtual, 2)
	for half := 0; half < 2; half++ {
		rows := make([]reg.VecVirtual, 8)
		for i := range rows {
			x := XMM()
			MOVQ(Mem{Base: rowPtr}, x)
			rows[i] = x
			ADDQ(stride, rowPtr)
		}
		halves[half] = transposeEightRows(rows)
	}
	out := make([]reg.VecVirtual, 8)
	for _, k := range want {
		packed := halves[0][k/2]
		upper := halves[1][k/2]
		var joined reg.VecVirtual
		if k%2 == 0 {
			joined = xLowQuads(packed, upper)
		} else {
			joined = xHighQuads(packed, upper)
		}
		wide := YMM()
		VPMOVZXBW(joined, wide)
		out[k] = wide
	}
	return out
}

func lumaNormalOutputs(p2, p1, p0, q0, q1, q2 reg.VecVirtual, tc0At, bsAt Mem, alphaGP, betaGP reg.GPVirtual) (reg.VecVirtual, reg.VecVirtual, reg.VecVirtual, reg.VecVirtual) {
	tc0V := YMM()
	VPMOVZXBW(tc0At, tc0V)
	zero := YMM()
	VPXOR(zero, zero, zero)

	apply := func() reg.VecVirtual {
		alphaV := yBroadcastReg(alphaGP)
		betaV := yBroadcastReg(betaGP)
		bsV := YMM()
		VPMOVZXBW(bsAt, bsV)
		m := yAnd(yGreater(alphaV, yAbs(ySub(p0, q0))), yGreater(bsV, zero))
		m = yAnd(m, yGreater(betaV, yAbs(ySub(p1, p0))))
		return yAnd(m, yGreater(betaV, yAbs(ySub(q1, q0))))
	}()

	betaV := yBroadcastReg(betaGP)
	nearP := yAnd(apply, yGreater(betaV, yAbs(ySub(p2, p0))))
	nearQ := yAnd(apply, yGreater(betaV, yAbs(ySub(q2, q0))))

	outP0, outQ0 := func() (reg.VecVirtual, reg.VecVirtual) {
		tc := ySub(ySub(tc0V, nearP), nearQ)
		d := yShr(yAdd(yAdd(yShl(ySub(q0, p0), 2), ySub(p1, q1)), yBroadcast(4)), 3)
		d = yClip(d, tc, ySub(zero, tc))
		full := yBroadcast(255)
		a := yMax(yMin(yAdd(p0, d), full), zero)
		b := yMax(yMin(ySub(q0, d), full), zero)
		return ySelect(apply, a, p0), ySelect(apply, b, q0)
	}()

	mid := yShr(yAdd(yAdd(p0, q0), yBroadcast(1)), 1)
	negTc0 := ySub(zero, tc0V)
	stepP := yClip(yShr(ySub(yAdd(p2, mid), yShl(p1, 1)), 1), tc0V, negTc0)
	outP1 := ySelect(nearP, yAdd(p1, stepP), p1)
	stepQ := yClip(yShr(ySub(yAdd(q2, mid), yShl(q1, 1)), 1), tc0V, negTc0)
	outQ1 := ySelect(nearQ, yAdd(q1, stepQ), q1)
	return outP1, outP0, outQ0, outQ1
}

func genDeblockLumaVerticalNormalAVX2() {
	TEXT("deblockLumaVerticalNormalAVX2", NOSPLIT,
		"func(plane []byte, offset int, stride int, tc0 *[16]uint8, bs *[16]uint8, alpha int32, beta int32)")
	Pragma("noescape")
	Doc("")
	base := Load(Param("plane").Base(), GP64())
	offset := Load(Param("offset"), GP64())
	stride := Load(Param("stride"), GP64())
	tc0Ptr := Load(Param("tc0"), GP64())
	bsPtr := Load(Param("bs"), GP64())
	alphaGP := Load(Param("alpha"), GP32())
	betaGP := Load(Param("beta"), GP32())
	ADDQ(offset, base)

	left := GP64()
	MOVQ(base, left)
	SUBQ(Imm(4), left)
	cols := loadSampleColumns(left, stride, []int{1, 2, 3, 4, 5, 6})

	outP1, outP0, outQ0, outQ1 := lumaNormalOutputs(cols[1], cols[2], cols[3], cols[4], cols[5], cols[6],
		Mem{Base: tc0Ptr}, Mem{Base: bsPtr}, alphaGP.(reg.GPVirtual), betaGP.(reg.GPVirtual))

	a := yPackBytes(outP1)
	b := yPackBytes(outP0)
	c := yPackBytes(outQ0)
	d := yPackBytes(outQ1)

	t0 := xLowBytes(a, b)
	t1 := xHighBytes(a, b)
	t2 := xLowBytes(c, d)
	t3 := xHighBytes(c, d)
	quads := [4]reg.VecVirtual{
		xLowWords(t0, t2),
		xHighWords(t0, t2),
		xLowWords(t1, t3),
		xHighWords(t1, t3),
	}

	rowPtr := GP64()
	MOVQ(base, rowPtr)
	SUBQ(Imm(2), rowPtr)
	for _, q := range quads {
		for lane := 0; lane < 4; lane++ {
			if lane == 0 {
				VMOVD(q, Mem{Base: rowPtr})
			} else {
				VPEXTRD(Imm(uint64(lane)), q, Mem{Base: rowPtr})
			}
			ADDQ(stride, rowPtr)
		}
	}
	VZEROUPPER()
	RET()
}

func yPackBytes(v reg.VecVirtual) reg.VecVirtual {
	packed := YMM()
	VPACKUSWB(v, v, packed)
	ordered := YMM()
	VPERMQ(Imm(0xD8), packed, ordered)
	narrow := XMM()
	VMOVDQU(ordered.AsX(), narrow)
	return narrow
}

func genAvgBytes(w, h int) {
	TEXT(fmt.Sprintf("avgBytes%dx%d", w, h), NOSPLIT,
		"func(dst []byte, dstStride int, a []byte, aStride int, b []byte, bStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	aPtr := Load(Param("a").Base(), GP64())
	aStride := Load(Param("aStride"), GP64())
	bPtr := Load(Param("b").Base(), GP64())
	bStride := Load(Param("bStride"), GP64())

	for y := 0; y < h; y++ {
		for x := 0; x < w; x += 16 {
			run := w - x
			if run > 16 {
				run = 16
			}
			va := XMM()
			vb := XMM()
			switch run {
			case 16:
				MOVOU(Mem{Base: aPtr, Disp: x}, va)
				MOVOU(Mem{Base: bPtr, Disp: x}, vb)
			case 8:
				MOVQ(Mem{Base: aPtr, Disp: x}, va)
				MOVQ(Mem{Base: bPtr, Disp: x}, vb)
			default:
				ga := GP32()
				MOVL(Mem{Base: aPtr, Disp: x}, ga)
				MOVD(ga, va)
				gb := GP32()
				MOVL(Mem{Base: bPtr, Disp: x}, gb)
				MOVD(gb, vb)
			}
			PAVGB(vb, va)
			switch run {
			case 16:
				MOVOU(va, Mem{Base: dst, Disp: x})
			case 8:
				MOVQ(va, Mem{Base: dst, Disp: x})
			default:
				out := GP32()
				MOVD(va, out)
				MOVL(out, Mem{Base: dst, Disp: x})
			}
		}
		if y != h-1 {
			ADDQ(dstStride, dst)
			ADDQ(aStride, aPtr)
			ADDQ(bStride, bPtr)
		}
	}
	RET()
}

func genCopyBlock(w, h int) {
	TEXT(fmt.Sprintf("copyBlock%dx%d", w, h), NOSPLIT,
		"func(dst []byte, dstStride int, src []byte, srcStride int)")
	Pragma("noescape")
	Doc("")
	dst := Load(Param("dst").Base(), GP64())
	dstStride := Load(Param("dstStride"), GP64())
	src := Load(Param("src").Base(), GP64())
	srcStride := Load(Param("srcStride"), GP64())

	for y := 0; y < h; y++ {
		switch w {
		case 16:
			v := XMM()
			MOVOU(Mem{Base: src}, v)
			MOVOU(v, Mem{Base: dst})
		case 8:
			g := GP64()
			MOVQ(Mem{Base: src}, g)
			MOVQ(g, Mem{Base: dst})
		default:
			g := GP32()
			MOVL(Mem{Base: src}, g)
			MOVL(g, Mem{Base: dst})
		}
		if y != h-1 {
			ADDQ(srcStride, src)
			ADDQ(dstStride, dst)
		}
	}
	RET()
}
