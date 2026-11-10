# h264

Pure-Go (CGO=0) reader for the H.264/AVC bitstream, and the **derivations clause
8.2 defines on top of it**: picture order counts, the reference picture set, the
reference lists a slice predicts from, and the weights a bi-predicted block is
combined with.

```go
pred := h264.NewPredictor()
for _, u := range units {
    h, ref, err := h264.ParseSliceReferences(u, sps, pps)
    if err != nil || !h.BeginsPicture() {
        continue
    }
    p, err := pred.Picture(u, h, ref, sps, pps)   // POC, L0, L1, and a handle
}
```

## The decoder

`github.com/go-avkit/h264/decoder` turns an Annex B stream into pictures, in
display order: CABAC and CAVLC, Baseline, Main and High (8x8 transform, scaling
matrices), progressive, 8-bit 4:2:0, pure Go, no assembly required. Field and
MBAFF coding, lossless transform bypass, higher bit depths and other chroma
formats are refused with `ErrUnsupported`.

```go
d := decoder.New(decoder.Limits{MaxFrameMBs: 1 << 17})
frames, err := d.Decode(annexB)      // any chunking; pictures come out as they are ready
rest, err := d.Flush()
i420 := frames[0].AppendI420(nil)
```

The root package (parsers and derivations, 100 % covered) and the decoder are two
layers of one module. The decoder was **imported from
[`oops1/go.264`](https://github.com/oops1/go.264) v1.12.0 (commit `3373a3f`)**,
by Valery Chukalin, whose BSD 2-Clause licence is kept in `LICENSE-go.264`; the
encoder and the hardware back ends of that project are not part of this import.
Its own header parsers (`internal/syntax`) are still there, next to the ones in
the root package, until the two are unified.

**Verified** against ITU-T H.264.1: `TestJVTConformance` compares every picture of
the 73 streams in `testdata/jvt` with ffmpeg's `framemd5`, bit for bit. 67
match; `MR2_TANDBERG_E`, `MR3_TANDBERG_B`, `MR4_TANDBERG_C` and `MR5_TANDBERG_C`
need MMCO 5 and frame-number gaps, which go.264 v1.12.0 does not handle
([oops1/go.264#2](https://github.com/oops1/go.264/pull/2)), and the test fails
if one of them starts to pass without being taken off its list. CI downloads the
streams (`tools/jvtfetch`) and runs them, then fuzzes `decoder.Decoder` and the
header reader; the corpus is in `testdata/`.

**Speed**, decode only, one core, 1280x720 x264 GOP 60, the IDR and the 45
pictures after it (what a poster frame at 1.5 s into a GOP costs), Apple M4 Max:

| | time |
|---|---|
| this decoder, as imported | 249 to 259 ms |
| `ffmpeg -threads 1 -ss 5.5 -i clip -frames:v 1` (process start and exit included, about 30 ms) | 88 ms |

It is slower than ffmpeg, which is an open defect
([go-avkit/decode#10](https://github.com/go-avkit/decode/issues/10)); the
profile is in the issue and the work follows.

## What the root package is not

**It does not decode pictures.** There is no entropy decoding, no residual, no
transform, no motion compensation and no deblocking in it — it answers the
questions a decoder has to answer *before* it can: which pictures are references,
in what order a slice addresses them, what each picture's order count is, and how
two predictions are weighted. The decoder above is the other layer.

The two agree on **785 reference lists across 18 streams** and on **all 65536
implicit-weight pairs** — see below.

## What is in it

| | |
|---|---|
| `SplitAnnexB`, `SplitLengthPrefixed`, `Unit` | the NAL layer |
| `ParseSPS`, `ParsePPS` | the parameter sets |
| `ParseSliceHeader`, `ParseSliceReferences` | the slice header, through `dec_ref_pic_marking` |
| `SplitPictures` | coded pictures, without decoding any |
| `POCCounter` | picture order counts, types 0 and 2 (§8.2.1) |
| `RefSet` | the reference picture set: sliding window and every marking operation (§8.2.5) |
| `InitialRefLists`, `ApplyRefListOps` | the reference lists and their modifications (§8.2.4) |
| `ImplicitWeighting`, `ExplicitWeighting`, `WeightingFor`, `DistScaleFactor` | the prediction weights (§8.4.2.3) |
| `Predictor` | the five above, composed in the one order that is correct |
| `ErrRefIdxRange` | a set or a slice stating more active references than a list may hold |

**Order count type 1 is not derived.** It needs `offset_for_ref_frame` and its
cycle, which the set reader does not read, so `POCCounter` returns `ErrPOCType`
rather than a number it cannot compute. Measured over 158 sequences from one
library: every one was type 0 or type 2, none was type 1.

**Field coding is refused, not guessed.** A field pair shares a frame number and
is marked as a unit; nothing here implements that, and answering anyway would
hand back a set that is quietly wrong.

**Windows, macOS, Linux; six 64-bit architectures.** 100% statement coverage per
function, gated in CI.

## A count from the stream sizes an allocation

⛔ **`num_ref_idx_lX_active_minus1` is a 32-bit syntax element and a reference
entry is 24 bytes.** An 18-byte picture parameter set states 4 294 967 295 of
them. The allocation in `ApplyRefListOps` was measured linear — 1 Mi entries for
25.2 MB, 10 Mi for 251.7 MB — so that set asks for about 103 GB.

7.4.2.2 puts the field in 0..31. The bound is applied where the count is
**read**: in `ParsePPS` and in the slice header's own override. It used to live
in `readWeightList` alone, whose comment said as much — "how many weights a list
may hold" — and a slice that stated no weight table carried the count straight
past it to the list construction.

The check is made on the value **before** the `+1` the syntax carries: at the
top of the range that increment wraps to zero, and a set claiming a list of no
entries would read as conformant.

### A selector the consumer indexes with

⛔ **`cabac_init_idc` picks one of THREE context tables and
`disable_deblocking_filter_idc` one of three behaviours.** Neither was bounded,
and a consumer indexes with them: a slice stating **1 048 576** was read and
handed on. 7.4.3 puts both in 0..2.

⛔ **`SliceQPY` indexes the quantisation tables, and it is a SUM** — the picture
set's `InitQP` plus the slice's delta. Neither end was bounded, so a delta of
1000 gave a quantiser of **1026**, twenty times the largest the tables hold.

The floor **moves with the bit depth**: `QpBdOffsetY` is six per bit past eight,
so a ten-bit sequence may legally state a quantiser down to −12. A bound written
as zero refuses that stream, and there is a witness for it.

Bounded with them, each because something later indexes a table with it: both
deblocking offsets (−6..6), the picture and switching quantisers, and both
chroma quantiser offsets (−12..12).

### A width is read before it is narrowed

⛔ **`log2_max_frame_num_minus4` and `log2_max_pic_order_cnt_lsb_minus4` are BIT
COUNTS** — the slice header reads `frame_num` and the order count with them, so
a reader given a wrong one is misaligned from that field onwards. Each was kept
in a byte with **no bound at all**, and so were both bit depths.

A narrowing conversion does not merely lose a value: it maps out-of-range values
**onto legal ones**. `uint8(1048576)` is 0, and `uint8(256) + 4` is 4 — the
smallest width the format allows — so a check made afterwards would *pass* them.

All four are now read as a full `uint32`, checked against 7.4.2.1.1's range, and
narrowed afterwards. Both boundaries are pinned.

### A weight is a shift count and a multiplier

⛔ **`luma_log2_weight_denom` leaves this package as a SHIFT COUNT** — a
`Weighting` computes `1 << LogDenom` and shifts a sample by it. A count the
stream chose freely is a shift by whatever it likes, and in Go that is not a
panic: it is a silently wrong number. Measured before the bound: a slice
declaring **1 048 576** was read and handed on.

⛔ **A weight multiplies a sample before anything clamps it.** One of 2³⁰
overflows the arithmetic rather than making a bright picture; that too was read
and handed on.

7.4.3.2 puts both denominators in **0..7** and every weight and offset in
**-128..127**, and `ParseSliceReferences` now refuses the rest. Both boundaries
are pinned: -128 and 127 are read, 128 and -129 are not.

## How it is verified

Every rule has a witness, and every witness was checked to **fail when its rule
is ablated** — a test that passes against the broken code is not a test. Beyond
that, three controls that do not depend on this code being right:

- **The reference set against ffmpeg.** `ffmpeg -threads 1 -debug mmco` prints
  its own reference set; it shares no code with this. **3110 reference pictures
  across 101 streams, 3110 agree, zero disagreements**, and no disagreement about
  how many reference pictures a stream has either.
- **The reference lists against `oops1/go.264`.** **785 lists across 18 streams,
  zero disagreements.**
- **The implicit weights, exhaustively.** The clause clips `tb` and `td` to
  [-128, 127], so 65536 pairs is the whole domain rather than a sample: **all
  65536 agree** with `go.264`, plus the long-term fallback.

Those comparisons found defects in both directions. They caught `Prediction`
carrying the list *ordering* rather than the list the slice can index, and they
localised an output-reordering defect in `go.264` that had been reported — by
this project — as a decoding defect it was not.

## Licence

BSD-3-Clause. The decoder (`decoder/` and `internal/` except where a file says
otherwise) is BSD-2-Clause, Copyright (c) 2026 Valery Chukalin, from
`oops1/go.264`: see `LICENSE-go.264`.
