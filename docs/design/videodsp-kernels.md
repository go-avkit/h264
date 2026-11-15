# Shared video DSP kernels: design, step 1 (H.264 first, HEVC and AV1 next)

Status: design only, nothing implemented. Refs go-avkit/avkit#89 and item 4 of
go-avkit/h264#48.

This document is a clean-room specification. It holds equations, shapes and
measurements, never code from another decoder. Whoever implements a kernel
works from the arithmetic stated here and from the ITU-T text, not from
FFmpeg or dav1d sources.

## 1. What already exists (prior art, read on 2026-11-15)

| Where | What it holds | Consequence |
|---|---|---|
| go-avkit/h264 `internal/simd` (v0.10.0) | amd64 only, generated with avo v0.6.0 (inherited from oops1/go.264): SAD, SATD, 4x4 forward/inverse transform, quant/dequant, AddResidual4x4, six-tap H/V/HV at 16x16 … 4x4, AvgBytes, CopyBlock, chroma bilinear 8/4 wide, luma deblock normal/strong (AVX2). Portable fallbacks in `generic.go`. | The kernel *list* for amd64 is already known and tested. arm64 has nothing: every Apple and Android device runs the scalar Go path. |
| go-avkit/h264 `decoder/jvt_test.go` | ITU-T H.264.1 conformance streams, compared frame by frame | The decoder-level oracle already exists. |
| go-videodsp/yuv | Portable planar YUV→RGBA, "shared by every video decoder" | The org that owns shared video pixel code exists; no kernel repo in it yet. |
| go-asmgen/asmgen | Plan 9 text for 6 targets; typed AVX2 integer ops (#44); arm64 typed ops are float only; NEON integer work goes through `Raw`. `fft/` holds go-fft's kernel *builders* per arch; `internal/gap` + `goasm-patches` register instructions `cmd/asm` lacks as `WORD`s with llvm-mc/GNU as references. | The go-fft precedent is the model: builders live in go-asmgen, a thin generator lives in the consumer. Missing NEON integer instructions go through the gap registry, not hand-written `WORD`s. |
| go-simd/* | Generic algorithms (hash, base64, popcount …) on all 6 targets | Not a home for codec-specific arithmetic (see §4.1). |
| FFmpeg 9.0.2 `libavcodec/aarch64` (local clone, read for shapes only) | h264dsp, h264qpel, h264chroma, h264pred, h264idct NEON; hevcdsp; a shared `h26x/` directory used by HEVC and VVC | Confirms the kernel *shapes* in §5. |

`cmd/asm` (Go 1.27.4) NEON integer coverage, checked against
`cmd/internal/obj/arm64/a.out.go`: present — `VADD VSUB VMUL VMLA VMLS VUMLAL
VSMLAL VUMULL VSMULL VURHADD VUHADD VSRHADD VSQADD VUQADD VSQSUB VUQSUB VUSHLL
VSSHLL VUXTL VSXTL VXTN VSQXTN VSQXTUN VUQXTN VSHRN VSRSHR VSSHR VUSHR VUSRA
VCMHI VCMHS VCMGT VCMGE VBSL VBIT VBIF VTRN1/2 VZIP1/2 VUZP1/2 VEXT VTBL VLD1-4
VST1-4 VUMAX VUMIN VSMAX VSMIN VABS VNEG`. **Absent** and needed here: `UABD/SABD`
(deblocking's |p0−q0| < α tests), `URSHR`, `RSHRN`, `SQRSHRUN` (rounding
narrow-with-saturate: the last step of every MC filter and of the inverse
transform add), `UADDL/USUBL/USUBW/SADDW` (widening add/sub), integer
`MUL/MLA` by element, `SQRDMULH`. Each becomes a go-asmgen gap entry. SVE2 is
absent from `cmd/asm` entirely.

## 2. Profile (go-avkit/h264 v0.10.0, real content)

Clip: 240 frames of a 1920x1080 WEB release, High profile, CABAC, 8x8
transform, B-frames with implicit weighted bi-prediction (`weighted_bipred_idc
= 2`, x264's default), 4 references, about 6.5 Mbit/s; cut with stream copy
(`-c copy -bsf:v h264_mp4toannexb`), no re-encode. Apple M4 Max, `GOMAXPROCS=1`,
two rounds under pprof.

**Load average was 16–22 (above the threshold of 8): the times below are
indicative; the shares are more robust than the times.**

| Decoder | 240 frames | fps | vs ffmpeg |
|---|---|---|---|
| ffmpeg 9.0.2 `-threads 1` (NEON) | 1.05 s | 227 | 1 |
| ffmpeg 9.0.2 `-threads 1 -cpuflags 0` (plain C) | 1.93 s | 125 | 0.54× |
| go-avkit/h264 v0.10.0 | 6.37–6.44 s | 37.5 | **0.165×** |

We are 3.3× slower than FFmpeg's *plain C*. Half of the gap is therefore not
SIMD at all: it is structure (see §3).

| Stage (cumulative share of CPU) | Share | Notes |
|---|---|---|
| Luma quarter-sample MC (`mc.PredictLuma`) | **38.2 %** | of which rounding averages of two predictions 19.6 % (`averageBlocks` 12.0, `averageSrcAndBlock` 7.6), six-tap H/V/HV 14.4 %, integer-position copy 4.2 % |
| Bi-prediction weighting (`mc.WeightBi`, implicit) | **11.4 %** | scalar per sample, run even when both weights are 32 |
| Deblocking filters (luma + chroma) | **14.7 %** | `filterLumaSample` alone 9.2 % |
| Deblocking bS derivation | 3.8 % | |
| Reference border extension (`ExtendBorders`, memmove) | 5.1 % | whole-picture padding per reference |
| CABAC (engine + residual + syntax) | 6.3 % | `DecodeDecision` 3.8 % |
| Inverse transform, dequant, residual add | 4.6 % | `Inverse8x8` 3.1 % |
| Chroma MC (eighth-sample bilinear) | 2.6 % | |
| Scheduler yields (`parallel.Wavefront` on one P) | 2.7 % | `goschedImpl` |
| Intra prediction | 1.6 % | |
| Other (MB parsing, MV prediction, colocated, motion store) | ≈ 9 % | |

## 3. Algorithm before kernels (in h264, portable, cheap)

These are not SIMD and they come first, because they also speed up every
target without assembly (riscv64, loong64, wasm):

1. **Separable centre position.** The HV half-sample `j` is computed today by
   six horizontal filters *per output sample* (6× redundant). Filter the
   `h+5` rows horizontally once into a 16-bit row buffer, then vertically.
2. **Fuse quarter-sample averaging into the producer.** Positions a, c, d, n,
   e, g, p, r, f, i, k, q are rounding averages of two half/full-sample
   predictions; today each is a full block written to memory and re-read by
   a separate averaging pass (19.6 %). The kernel produces both terms in
   registers and averages before the single store.
3. **Bi-prediction**: when the implicit weights are (32, 32) the weighted
   formula reduces exactly to the default rounding average; take that path.
4. **Edge emulation instead of padding.** Build a padded block only when the
   motion vector's footprint leaves the picture, not a padded copy of every
   reference (5.1 %).
5. Wavefront on one P: run the deblocking rows inline when `GOMAXPROCS=1`.

## 4. Where the kernels live

### 4.1 Home: a new repository `go-videodsp/kernels` (to be created by the owner)

- go-videodsp already declares itself the home of pixel code "shared by every
  video decoder" (yuv). H.264, HEVC (replacing gen2brain/h265) and AV1 all
  consume the same shapes.
- go-simd holds codec-agnostic algorithms, one repo per algorithm, with a
  stdlib-like drop-in contract. Codec kernels have a spec-defined integer
  contract and a codec conformance oracle; they do not fit there.
- One module with packages, not one repo per kernel family: the families share
  the generator, the dispatch, the test harness and the benchmark tables, and
  a decoder always needs all of them at once.
- **Builders** (instruction sequences reusable beyond video: 8x8 16-bit
  transpose, rounding average, widening multiply-accumulate rows,
  saturating narrow, 4x4/8x8 butterflies) go into
  `go-asmgen/asmgen/videodsp/{arm64,amd64}`, as `fft/` did for go-fft. The
  kernels repo keeps a thin generator that names and writes its `.s` files.
  That is "the lowest shared library, one implementation".

**This needs a new repository; it has not been created.** Until it is, the
alternative is packages under go-videodsp/yuv, which the name argues against.

### 4.2 Packages

| Package | H.264 now | HEVC next | AV1 next |
|---|---|---|---|
| `pix` | copy, rounding average, residual add with clip to bit depth, edge emulation | same | same |
| `mc` | six-tap quarter-sample luma (16 positions, put and avg), eighth-sample chroma bilinear, uni weight, bi weight, average | 8-tap luma / 4-tap chroma into 14-bit `int16` intermediates; uni/bi/weighted combine from intermediates | 8-tap regular/smooth/sharp + bilinear, scaled MC later; `prep` + avg / w_avg / mask combine |
| `itx` | 4x4, 8x8, DC-only, luma DC Hadamard 4x4, chroma DC 2x2, dequant | DCT 4–32, DST 4 | DCT/ADST/flip/identity 4–64 |
| `lf` | luma normal (bS<4), strong (bS=4), chroma both, H and V, bS derivation helper | HEVC luma/chroma deblock, SAO | deblock, CDEF, loop restoration (Wiener, SGR) |
| `ipred` | 16x16 V/H/DC/plane, 8x8 (with reference filtering), 4x4 nine modes, chroma DC/H/V/plane | 33 angular + planar + DC | directional, smooth, paeth, CfL |
| `cpu` | dispatch: arm64 NEON always (mandatory in ARMv8-A, Android included); amd64 AVX2 via go-asmgen `FeatureProbe`; SVE2 via HWCAP2 on linux/android only | | |

### 4.3 API conventions

- Planes are `[]byte` (8-bit) or `[]uint16` (bit depth 9–12, HEVC Main10,
  AV1 10/12), each with its own stride in samples. Every kernel exists in both
  element types; the 16-bit ones take `bitDepth` (the clip bound is
  `(1<<bitDepth)-1`).
- A call names an origin offset, the block width and height. Widths are the
  codec's: H.264 {2, 4, 8, 16}; HEVC {4, 8, 12, 16, 24, 32, 48, 64}; AV1 up to
  128. Assembly is written per width class (4, 8, 16, ≥16 in 16-column
  strips); heights loop inside the kernel.
- **Bounds are proved once per call** in Go (the whole footprint, including
  the filter's −2/+3 margins) before any assembly runs; a footprint that does
  not fit panics. The assembly never sees an unchecked offset.
- Coefficients: `int16` for 8-bit H.264 and for HEVC (the spec bounds them to
  16 bits); `int32` for H.264 above 8 bits and for AV1's 32-bit
  intermediates. The h264 decoder's `[16]int32` blocks move to `int16`.
- Intermediates of the two-dimensional filters are `int16` (H.264's
  horizontal six-tap of 8-bit samples spans −2550…10710); the second pass
  accumulates in 32 bits.
- Zero allocations per call; no goroutines.

## 5. Kernel list, priority order, arithmetic

Shares are of today's CPU on the clip above. "Recovered" is
`share × (1 − 1/speedup)` with the speedup FFmpeg's NEON shows over its C on
the same shape; the true numbers come from the per-kernel benchmarks (§7).

| # | Kernel | Share now | Assumed speedup | Recovered |
|---|---|---|---|---|
| 1 | Luma quarter-sample MC, fused (put and avg into destination), widths 16/8/4 | 38.2 % | ~10× | ~34 % |
| 2 | Bi-prediction combine: average, bi-weight (implicit and explicit), uni weight | 11.4 % | ~10× | ~10 % |
| 3 | Luma and chroma deblocking, one 16-sample edge per call, H and V | 14.7 % | ~6× | ~12 % |
| 4 | Inverse transform + add: 4x4, 8x8, DC-only, per-MB batch driven by the non-zero mask | 4.6 % | ~5× | ~3.7 % |
| 5 | Chroma eighth-sample MC, widths 8/4/2 | 2.6 % | ~6× | ~2.2 % |
| 6 | Edge emulation + copy (`pix`) | 5.1 % | algorithm | ~4.5 % |
| 7 | Deblocking bS derivation over 16 edges at once (compare MV/ref/nnz vectors) | 3.8 % | ~3× | ~2.5 % |
| 8 | Intra prediction | 1.6 % | ~4× | ~1.2 % |

Total recovered ≈ 70 % of today's time.

Arithmetic (equations of ITU-T H.264; see §8 for the reading status):

- **Luma (8.4.2.2.1).** Six-tap `(1, −5, 20, 20, −5, 1)`. Half-sample
  b, h: `Clip1((b1 + 16) >> 5)`. Centre j: `j1` from six intermediate b1 (or
  h1) values with the same taps, `Clip1((j1 + 512) >> 10)`; intermediates are
  not clipped. Quarter samples: `(x + y + 1) >> 1` of the two named neighbours
  (full or half samples, already clipped). The fused kernel computes the two
  terms in registers and averages them before the store.
- **Chroma (8.4.2.2.2).** `((8−dx)(8−dy)A + dx(8−dy)B + (8−dx)dyC + dxdyD + 32) >> 6`,
  dx, dy ∈ 0…7.
- **Weighted prediction (8.4.2.3).** Default: `(p0 + p1 + 1) >> 1`. Uni:
  `Clip1(((p·w + 2^(logWD−1)) >> logWD) + o)` (logWD ≥ 1, else `p·w + o`).
  Bi: `Clip1(((p0·w0 + p1·w1 + 2^logWD) >> (logWD + 1)) + ((o0 + o1 + 1) >> 1))`.
  Implicit weights are derived per reference pair from POC distances
  (logWD = 5, offsets 0).
- **Inverse transform (8.5.12, 8.5.13).** 4x4: rows then columns with the
  butterfly `e = s0+s2, f = s0−s2, g = (s1>>1)−s3, h = s1+(s3>>1)`, outputs
  `e+h, f+g, f−g, e−h`, final `(x + 32) >> 6`, then `Clip1(pred + r)`.
  8x8: the eight-point butterfly with `>>1` and `>>2` terms of 8.5.13,
  same final rounding. DC-only blocks: one value `(dc + 32) >> 6` added to all
  samples (the dominant case in P/B residuals).
- **Deblocking (8.7.2).** Per sample line across the edge, filter if
  `|p0−q0| < α ∧ |p1−p0| < β ∧ |q1−q0| < β`. bS < 4: `Δ = Clip3(−tc, tc,
  (((q0−p0)<<2) + (p1−q1) + 4) >> 3)`, tc = tc0 + (ap<β) + (aq<β) for luma,
  tc0 + 1 for chroma; p1/q1 corrected with `(p2 + ((p0+q0+1)>>1) − (p1<<1)) >> 1`
  clipped to ±tc0. bS = 4: the strong filter with the `α/4 + 2` test. Each
  NEON call handles 16 lines (one MB edge), with `tc0` per 4 lines and an
  all-lines-skip early exit.
- **Intra (8.3).** Straightforward broadcast/sum shapes; plane mode needs
  a 16-bit multiply-accumulate ramp.

Shapes confirmed in FFmpeg's aarch64 tree (names only): qpel is one entry
point per (size, fractional position) for put and avg, so averaging is fused;
bi-weight writes in place into the first prediction; IDCT entry points take
`int16` blocks and a whole-MB variant walks the non-zero mask; each deblocking
call filters one 16-sample edge with four tc0 values.

## 6. Architectures

1. **arm64 NEON first.** Apple M-series and every Android arm64 device. One
   code path, no runtime probe needed. Instructions missing from `cmd/asm`
   (§1) are registered in go-asmgen's gap table with llvm-mc and GNU as
   reference encodings, so they turn into mnemonics when Go gains them.
2. **amd64 AVX2**, porting the existing avo kernels of `internal/simd` onto
   go-asmgen builders (same list, then the missing ones). avo is retired from
   h264 once the asmgen versions are at least as fast.
3. **Portable Go reference** for every kernel, written from the equations,
   used on riscv64, loong64, ppc64le, s390x, wasm and as the oracle.
4. **SVE2** only where a benchmark shows a gain. On today's Armv9 Android cores
   the vector length is 128 bits, so SVE2 brings predication and a few
   instructions, not width. Encodings are raw `WORD`s through the same gap
   registry; dispatch on `HWCAP2_SVE2` (linux/android). Apple cores do not
   expose SVE.
5. **GPU / Vulkan / GLES / Metal**: not for decoding (MC on the GPU loses to
   transfer latency, and CABAC is serial). They serve YUV→RGB and scaling in
   go-videodsp/yuv (go-macos/metal on Apple; Vulkan compute or GLES on
   Android). The Neural Engine has no role in a bit-exact decoder.
6. **OS hardware decoders** (VideoToolbox, MediaCodec): an optional delegate
   behind the decoder interface, off by default; not part of this module.

## 7. Oracles and benchmarks

**Correctness: bit-exact by construction.** These are integer kernels whose
results the standard defines exactly; a conforming decoder has no error
bound to trade. (FMA, reordering and approximate algorithms do not apply.)

- Each kernel against its portable reference: exhaustive where the parameter
  space allows (all 16 quarter positions × widths × heights; deblock over all
  α/β indices, every tc0, bS 1–4; chroma over all 64 (dx, dy)); random blocks
  including the extremes 0 and 255 (1023 for 10-bit) for the rest.
- Native fuzz targets per family comparing assembly with the reference, and
  fuzzing the Go bounds proof (offset, stride, width, height) so no input
  reaches assembly out of bounds. Corpora committed.
- Decoder level: the existing H.264.1 conformance test (`TestJVTConformance`)
  and frame-hash equality with `ffmpeg -f framemd5` on clips of the local
  library.
- CI: amd64 and arm64 natively (asmdecl, regenerate-and-diff, runtime); other
  targets on the reference path.

**Speed.**

- Per kernel: Go benchmarks at the codec's block sizes, against FFmpeg's
  `checkasm --bench` for the same family (`h264dsp`, `h264qpel`,
  `h264chroma`, `h264pred`), built from the local FFmpeg 9.0.2 clone, C and
  NEON columns. Table per kernel in the README: reference Go, ours NEON,
  FFmpeg C, FFmpeg NEON.
- Whole decoder: `ffmpeg -threads 1 -benchmark -f null` and the h264 decode
  of the same Annex B clip, interleaved rounds, load ≤ 8.

## 8. Adoption by go-avkit/h264

1. Section 3's portable changes, each with a before/after table.
2. h264 imports `go-videodsp/kernels`; `internal/simd` shrinks to call sites.
   Family by family (MC luma → bi-prediction → deblock → IDCT → chroma →
   intra), one PR per family, each with the conformance test green and a
   benchmark table.
3. Coefficient storage moves from `int32` to `int16` for 8-bit streams.
4. amd64: the asmgen ports replace avo's output; avo leaves the module.

Reading status of the bibliography: the ITU-T H.264 (08/2024) PDF could not
be downloaded from this session (the ITU server refused automated retrieval),
so the clause numbers above are cited from the standard's known structure and
must be checked against the PDF before the first kernel lands. The arithmetic
itself was checked against the conformance-tested portable code of go-avkit/h264
(`internal/simd/generic.go`, `internal/mc/mc.go`). dav1d's `mc`/`itx`
design was not opened in this session; it must be read before the AV1 half of
`mc` (`prep` / `avg` / `w_avg` / `mask`) is specified.

## 9. Target

After §3 and kernels 1–8 on Apple M4, one core: the remaining time is about
30 % of today's, about 1.9 s for the clip, i.e. **≈ 0.5× ffmpeg `-threads 1`**
(from 0.165×). What remains is scalar: CABAC, macroblock parsing, motion
vector prediction, bS bookkeeping. With the CABAC and parsing work below,
**0.65–0.75×** is the realistic target. Parity on one core is not promised:
FFmpeg's entropy path is hand-tuned C with branch-free tricks the Go compiler
does not always reproduce, and bounds checks remain in the parser.

**CABAC is serial**: every bin's range depends on the previous one, so no SIMD
applies. What helps:

- Keep `range`, `offset` and the bit window in locals for a whole residual
  block (no reload through the struct per bin), and renormalise with a
  count-leading-zeros on a 64-bit window refilled several bytes at a time
  (go-avkit/h264#47 starts this).
- One table lookup returning both the LPS range and the next state.
- Residual decoding specialised per block category (4x4, 8x8, DC, chroma DC):
  context offsets precomputed, significance map and last flags in one tight
  loop; bypass bins of `coeff_abs_level_minus1` and mvd suffixes decoded
  several at a time with one comparison per bit, no renormalisation call.
- MPS path as the fall-through branch.
- Across cores rather than within: pipeline entropy decoding of macroblock
  row n+1 against reconstruction of row n, and frame threading (what
  `ffmpeg -threads N` does). Its share grows with bitrate: on 30+ Mbit/s
  Blu-ray sources it, not MC, becomes the first cost after the kernels.
