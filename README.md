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

## What it is not

**It does not decode pictures.** There is no entropy decoding, no residual, no
transform, no motion compensation and no deblocking here — nothing in this
module turns a bitstream into pixels. It answers the questions a decoder has to
answer *before* it can: which pictures are references, in what order a slice
addresses them, what each picture's order count is, and how two predictions are
weighted.

For pixels, compose it with a decoder. [`oops1/go.264`](https://github.com/oops1/go.264)
is a pure-Go H.264 decoder and the two agree on **785 reference lists across 18
streams** and on **all 65536 implicit-weight pairs** — see below.

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

BSD-3-Clause.
