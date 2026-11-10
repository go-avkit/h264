// Package decoder is a pure Go (CGO=0) H.264/AVC video decoder: Annex B in,
// 8-bit 4:2:0 pictures out, in display order.
//
// It is the decoding layer of github.com/go-avkit/h264, imported from
// github.com/oops1/go.264 v1.12.0 (commit 3373a3f) by Valery Chukalin under the
// BSD 2-Clause licence reproduced in LICENSE-go.264. Baseline, Main and High
// profile progressive streams are decoded; field and MBAFF coding, lossless
// transform bypass, bit depths above 8 and chroma formats other than 4:2:0 are
// refused with ErrUnsupported.
package decoder

import (
	"github.com/go-avkit/h264/internal/decoder"
	"github.com/go-avkit/h264/internal/frame"
)

// Errors a Decoder returns. They are wrapped with detail: test with errors.Is.
var (
	ErrOverLimit   = decoder.ErrOverLimit
	ErrUnsupported = decoder.ErrUnsupported
	ErrCorrupt     = decoder.ErrCorrupt
)

// Limits bounds what a stream may make the decoder allocate or accept. A zero
// field means no limit.
type Limits = decoder.Limits

// Frame is one decoded picture. Its planes alias the decoder's storage, and
// they are valid until the next call to Decode or Flush on the same Decoder:
// the decoder reuses the storage of a picture that is no longer a reference
// for a later one. A caller that keeps a picture longer copies it first
// (AppendI420 does).
type Frame struct {
	Y       []byte
	Cb      []byte
	Cr      []byte
	StrideY int
	StrideC int
	Width   int
	Height  int
}

// I420Size is the length of the picture packed as planar I420.
func (f *Frame) I420Size() int { return f.Width * f.Height * 3 / 2 }

// AppendI420 appends the picture, cropped and packed as planar I420.
func (f *Frame) AppendI420(dst []byte) []byte {
	for y := 0; y < f.Height; y++ {
		dst = append(dst, f.Y[y*f.StrideY:y*f.StrideY+f.Width]...)
	}
	cw, ch := f.Width/2, f.Height/2
	for _, plane := range [][]byte{f.Cb, f.Cr} {
		for y := 0; y < ch; y++ {
			dst = append(dst, plane[y*f.StrideC:y*f.StrideC+cw]...)
		}
	}
	return dst
}

func frameFromPicture(p *frame.Picture) *Frame {
	width, height := p.CropWidth, p.CropHeight
	if width <= 0 || width > p.Width {
		width = p.Width
	}
	if height <= 0 || height > p.Height {
		height = p.Height
	}
	return &Frame{
		Y:       p.Y[p.LumaOffset(0, 0):],
		Cb:      p.Cb[p.ChromaOffset(0, 0):],
		Cr:      p.Cr[p.ChromaOffset(0, 0):],
		StrideY: p.StrideY,
		StrideC: p.StrideC,
		Width:   width,
		Height:  height,
	}
}

// Decoder decodes one Annex B stream. It is not safe for concurrent use.
type Decoder struct {
	d *decoder.Decoder
}

// New returns a decoder that enforces limits.
func New(limits Limits) *Decoder {
	d := decoder.New()
	d.SetLimits(limits)
	d.SetRecycle(true)
	return &Decoder{d: d}
}

// Decode feeds Annex B bytes, which need not end on a NAL unit boundary, and
// returns the pictures that became ready, in display order. On error it still
// returns the pictures decoded before it.
func (d *Decoder) Decode(annexB []byte) ([]*Frame, error) {
	pics, err := d.d.Decode(annexB)
	return convert(pics), err
}

// Flush decodes what is buffered and returns every picture still held for
// reordering.
func (d *Decoder) Flush() ([]*Frame, error) {
	pics, err := d.d.Flush()
	return convert(pics), err
}

func convert(pics []*frame.Picture) []*Frame {
	if len(pics) == 0 {
		return nil
	}
	out := make([]*Frame, 0, len(pics))
	for _, p := range pics {
		out = append(out, frameFromPicture(p))
	}
	return out
}
