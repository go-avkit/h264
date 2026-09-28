// Copyright (c) 2026, go-avkit
// SPDX-License-Identifier: BSD-3-Clause

package h264

// Picture is one coded picture: the units that carry it, and what it is.
//
// Units are the NAL units in the order they arrived, parameter sets and all, so a
// caller writing a sample table has the bytes of a sample and nothing else to
// gather. Sync says the picture can be decoded without any before it, which is
// what a sample table calls a sync sample and a player calls a seek point.
type Picture struct {
	Units []Unit
	Type  SliceType
	Sync  bool
	Bytes int // the units' payloads and headers, not the separators between them
}

// SplitPictures groups a stream's NAL units into the pictures they carry.
//
// ⛔ A picture may be carried by several slices, so the units between one
// picture's first slice and the next one's belong together. The slice at
// macroblock zero is what marks the start, and it is the condition that does
// almost all the work: measured against ffprobe on 32 files, it alone put every
// picture boundary in the right place.
//
// It is not the whole rule the format states, and the rest is here because that
// one condition cannot tell two cases apart. Two pictures in a row both begin at
// macroblock zero -- that is the normal case and the reason the condition works.
// But a REDUNDANT slice repeats a picture already carried, also from macroblock
// zero, and a field pair carries two pictures whose slices both start there. So a
// slice at macroblock zero begins a new picture only if something that identifies
// the picture has also changed: the frame number, the parameter set, the field it
// codes, whether it is an IDR or which one, the picture order count, or whether it
// is a reference at all.
//
// Conditions the standard lists that are NOT here: nothing depends on memory
// management operations or on a stream switching between coded frames and coded
// fields mid-sequence, both of which need state this does not keep. A stream doing
// either would have its pictures grouped by the conditions above, which is right
// wherever one of them changes and wrong only where none does.
//
// Parameter sets and delimiters that arrive before a picture's first slice belong
// to that picture, not the one before: a decoder must have them in hand before the
// slice that uses them.
func SplitPictures(units []Unit, sps SPS, pps PPS) ([]Picture, error) {
	var out []Picture
	var pending []Unit
	var current *Picture
	var prev SliceHeader
	var havePrev bool

	commit := func() {
		if current == nil {
			return
		}
		out = append(out, *current)
		current = nil
	}

	for _, u := range units {
		if u.Type != UnitIDR && u.Type != UnitNonIDR {
			// Everything that is not a slice waits for the slice it precedes.
			pending = append(pending, u)
			continue
		}
		h, err := ParseSliceHeader(u, sps, pps)
		if err != nil {
			// ⛔ The picture being gathered is committed before the refusal, or
			// it would be dropped: a caller told "this went wrong" also needs
			// what did read, and the last picture is the one most likely to be
			// the reason. Returning without it loses a whole picture silently.
			commit()
			return out, err
		}
		if h.BeginsPicture() && (!havePrev || newPicture(prev, h)) {
			commit()
			current = &Picture{Type: h.Type, Sync: h.IDR}
		}
		if current == nil {
			// Slices before any picture began: a stream joined part way through.
			// They carry no picture this can name, and dropping them silently
			// would make a count of pictures disagree with a count of slices for
			// a reason nobody could see.
			current = &Picture{Type: h.Type, Sync: h.IDR}
		}
		current.Units = append(current.Units, pending...)
		for _, p := range pending {
			current.Bytes += len(p.Payload) + 1
		}
		pending = nil
		current.Units = append(current.Units, u)
		current.Bytes += len(u.Payload) + 1
		prev, havePrev = h, true
	}
	commit()
	// Units after the last slice -- a delimiter, a parameter set for a picture
	// that never arrived -- belong to no picture and are not invented one.
	return out, nil
}

// newPicture says whether b identifies a different picture from a.
//
// Each of these is a field a picture is identified BY, so a change in any one of
// them is a different picture. Equal in all of them, from macroblock zero, is a
// slice repeating a picture already carried.
func newPicture(a, b SliceHeader) bool {
	switch {
	case a.FrameNum != b.FrameNum,
		a.PPSID != b.PPSID,
		a.FieldPic != b.FieldPic,
		a.BottomField != b.BottomField,
		a.IDR != b.IDR,
		a.IDR && b.IDR && a.IDRPicID != b.IDRPicID,
		a.POCLSB != b.POCLSB,
		a.DeltaPOCBottom != b.DeltaPOCBottom,
		a.DeltaPOC != b.DeltaPOC:
		return true
	}
	return false
}
