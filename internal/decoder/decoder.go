package decoder

import (
	"errors"
	"fmt"
	"reflect"

	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/level"
	"github.com/go-avkit/h264/internal/loopfilter"
	"github.com/go-avkit/h264/internal/nal"
)

type Decoder struct {
	spsMap map[uint32]*h264.SPS
	ppsMap map[uint32]*h264.PPS

	scanner *nal.Scanner
	grid    *mbGrid
	cur     *frame.Picture
	sps     *h264.SPS
	buffer  *dpb
	lastHdr *sliceHeader

	pending    []*frame.Picture
	maxReorder int

	recycle   bool
	delivered []*frame.Picture
	pool      []*frame.Picture

	motionOwners []*frame.Picture
	motionFree   []*frame.Motion

	scal    *scalingTables
	scalSPS *h264.SPS
	scalPPS *h264.PPS

	sliceCount int
	limits     Limits
}

type Limits struct {
	MaxFrameMBs  int
	MaxNALBytes  int
	EnforceLevel bool
}

func New() *Decoder {
	return &Decoder{
		spsMap:  make(map[uint32]*h264.SPS),
		ppsMap:  make(map[uint32]*h264.PPS),
		scanner: nal.NewScanner(),
	}
}

func (d *Decoder) SetLimits(l Limits) { d.limits = l }

func (d *Decoder) Limits() Limits { return d.limits }

func (d *Decoder) SPS(id uint32) *h264.SPS { return d.spsMap[id] }

func (d *Decoder) PPS(id uint32) *h264.PPS { return d.ppsMap[id] }

// SetRecycle lets the decoder reuse the planes of a picture it has returned,
// from the next call to Decode or Flush on: the pictures of one call are valid
// until the next. A caller that keeps pictures longer must leave it off, which
// is the default; each picture is then a fresh allocation.
func (d *Decoder) SetRecycle(on bool) {
	d.recycle = on
	if !on {
		d.delivered, d.pool = nil, nil
	}
}

// release takes back the pictures returned by earlier calls that are no longer
// a reference, so that newPicture can hand their planes out again. A picture
// still in the reference set, or still waiting for its turn to be output, is
// not touched.
func (d *Decoder) release() {
	if !d.recycle {
		return
	}
	keep := d.delivered[:0]
	for _, p := range d.delivered {
		held := false
		if d.buffer != nil {
			for _, r := range d.buffer.refs {
				if r.pic == p {
					held = true
					break
				}
			}
		}
		if held {
			keep = append(keep, p)
			continue
		}
		if len(d.pool) < maxPooledPictures {
			d.pool = append(d.pool, p)
		}
	}
	clear(d.delivered[len(keep):])
	d.delivered = keep
}

// maxPooledPictures bounds the free list: a stream never needs more than its
// reference frames and the pictures held for reordering, 16 and 16 at most.
const maxPooledPictures = 34

// newPicture returns a picture of the given size in macroblocks: a recycled
// one when a free one has that geometry, else a new allocation. A recycled
// picture keeps the samples of its previous life; every macroblock of a
// picture is written before the picture is output, and a macroblock a corrupt
// stream never states is whatever was there, not zero.
func (d *Decoder) newPicture(widthMBs, heightMBs int) *frame.Picture {
	for len(d.pool) > 0 {
		p := d.pool[len(d.pool)-1]
		d.pool = d.pool[:len(d.pool)-1]
		if p.Width == widthMBs*16 && p.Height == heightMBs*16 {
			p.Recycle()
			return p
		}
	}
	return frame.NewPicture(widthMBs, heightMBs)
}

func (d *Decoder) Decode(data []byte) ([]*frame.Picture, error) {
	d.release()
	out, err := d.decode(data)
	if d.recycle {
		d.delivered = append(d.delivered, out...)
	}
	return out, err
}

func (d *Decoder) Flush() ([]*frame.Picture, error) {
	d.release()
	out, err := d.flush()
	if d.recycle {
		d.delivered = append(d.delivered, out...)
	}
	return out, err
}

func (d *Decoder) decode(data []byte) ([]*frame.Picture, error) {
	d.scanner.Append(data)
	var out []*frame.Picture
	for {
		unit, ok := d.scanner.Next()
		if !ok {
			break
		}
		pics, err := d.handleUnit(unit)
		out = append(out, pics...)
		if err != nil {
			return out, err
		}
	}
	if d.limits.MaxNALBytes > 0 && d.scanner.Buffered() > d.limits.MaxNALBytes {
		return out, fmt.Errorf("%w: %d bytes buffered without a NAL unit boundary, the caller allows %d",
			ErrOverLimit, d.scanner.Buffered(), d.limits.MaxNALBytes)
	}
	return out, nil
}

func (d *Decoder) flush() ([]*frame.Picture, error) {
	var out []*frame.Picture
	if unit, ok := d.scanner.Flush(); ok {
		pics, err := d.handleUnit(unit)
		out = append(out, pics...)
		if err != nil {
			return out, err
		}
	}
	out = append(out, d.finishPicture()...)
	out = append(out, d.bump(true)...)
	return out, nil
}

func minPOCIndex(pics []*frame.Picture) int {
	best := 0
	for i, p := range pics {
		if p.POC < pics[best].POC {
			best = i
		}
	}
	return best
}

func (d *Decoder) bump(drain bool) []*frame.Picture {
	var out []*frame.Picture
	for len(d.pending) > 0 && (drain || len(d.pending) > d.maxReorder) {
		i := minPOCIndex(d.pending)
		out = append(out, d.pending[i])
		d.pending = append(d.pending[:i], d.pending[i+1:]...)
	}
	return out
}

// takeMotion returns a cleared Motion for the current geometry, recycled when
// one is free: a Motion is 18 bytes per 4x4 block, 1.1 MB at 720p, and one
// was allocated per picture.
func (d *Decoder) takeMotion() *frame.Motion {
	w, h := d.grid.widthMBs, d.grid.heightMBs
	for len(d.motionFree) > 0 {
		m := d.motionFree[len(d.motionFree)-1]
		d.motionFree = d.motionFree[:len(d.motionFree)-1]
		if m.Fits(w, h) {
			m.Reset()
			return m
		}
	}
	return frame.NewMotion(w, h)
}

// recycleMotion takes back the Motion of every picture that has left the
// reference set: nothing can name it as a co-located picture any more, and
// the Frame handed to the caller does not expose it.
func (d *Decoder) recycleMotion() {
	keep := d.motionOwners[:0]
	for _, p := range d.motionOwners {
		held := false
		for _, r := range d.buffer.refs {
			if r.pic == p {
				held = true
				break
			}
		}
		if held {
			keep = append(keep, p)
			continue
		}
		d.motionFree = append(d.motionFree, p.Motion)
		p.Motion = nil
	}
	clear(d.motionOwners[len(keep):])
	d.motionOwners = keep
}

func (d *Decoder) finishPicture() []*frame.Picture {
	if d.cur == nil {
		return nil
	}
	pic := d.cur
	d.cur = nil
	if d.grid != nil {
		loopfilter.Apply(pic, d.grid.widthMBs, d.grid.heightMBs, func(mbx, mby int) *loopfilter.MB {
			m := d.grid.at(mbx, mby)
			if m == nil {
				return nil
			}
			return &m.MB
		})
	}
	pic.ExtendBorders()
	if d.grid != nil {
		// Only a reference picture can be a co-located picture (8.4.1.2.1
		// draws it from RefPicList1[0]), so only it keeps its motion.
		if d.lastHdr == nil || d.lastHdr.NalRefIDC != 0 {
			pic.Motion = d.grid.motion(d.takeMotion())
			d.motionOwners = append(d.motionOwners, pic)
		}
	}
	if d.buffer != nil && d.lastHdr != nil {
		d.buffer.store(pic, d.lastHdr)
		d.recycleMotion()
	}
	d.pending = append(d.pending, pic)
	return d.bump(false)
}

func (d *Decoder) handleUnit(ebsp []byte) ([]*frame.Picture, error) {
	u, err := nal.Parse(ebsp)
	if err != nil {
		return nil, err
	}
	// The escaping was undone once, by nal.Parse; the readers of the root package
	// take that copy instead of making their own.
	unit := h264.Unit{Type: h264.UnitType(u.Type), RefIDC: u.RefIDC, Payload: ebsp[1:], Unescaped: u.RBSP}
	switch u.Type {
	case nal.TypeSPS:
		s, err := h264.ParseSPS(unit)
		if err != nil {
			return nil, refusal(err)
		}
		sps := &s
		if err := d.checkSupported(sps); err != nil {
			return nil, err
		}
		if old, ok := d.spsMap[sps.ID]; ok && reflect.DeepEqual(old, sps) {
			return nil, nil
		}
		d.spsMap[sps.ID] = sps
		return nil, nil

	case nal.TypePPS:
		id, spsID, err := h264.PPSIDs(unit)
		if err != nil {
			return nil, refusal(err)
		}
		// A set may arrive before the sequence set it names; what it needs of that
		// one (the bit depth to bound its quantiser, the chroma format to count its
		// scaling lists) is assumed 8-bit 4:2:0 until a set is there, and a picture
		// set that states scaling lists cannot be read without it.
		ref := h264.SPS{ChromaFormat: 1, BitDepthLuma: 8, BitDepthChroma: 8}
		if s := d.spsMap[spsID]; s != nil {
			ref = *s
		}
		p, err := h264.ParsePPS(unit, ref)
		if err != nil {
			return nil, refusal(err)
		}
		if p.ScalingPresent && d.spsMap[spsID] == nil {
			return nil, fmt.Errorf("%w: picture parameter set %d states scaling lists and its sequence set %d is absent", ErrNoParameters, id, spsID)
		}
		pps := &p
		if old, ok := d.ppsMap[pps.ID]; ok && reflect.DeepEqual(old, pps) {
			return nil, nil
		}
		d.ppsMap[pps.ID] = pps
		return nil, nil

	case nal.TypeSliceIDR, nal.TypeSliceNonIDR:
		return d.decodeSlice(u, unit)
	}
	return nil, nil
}

// refusal says what the decoder says of a parameter set or a header the reader
// of the syntax refused: a feature it does not follow is ErrUnsupported, and
// anything else is returned as it came.
func refusal(err error) error {
	switch {
	case errors.Is(err, h264.ErrUnsupportedSPS), errors.Is(err, h264.ErrUnsupportedPPS), errors.Is(err, h264.ErrSliceGroups):
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	}
	return err
}

func (d *Decoder) checkSupported(sps *h264.SPS) error {
	if sps.ChromaArrayType() != 1 {
		return fmt.Errorf("%w: chroma format %d", ErrUnsupported, sps.ChromaFormat)
	}
	if sps.BitDepthLuma != 8 || sps.BitDepthChroma != 8 {
		return fmt.Errorf("%w: bit depth above 8", ErrUnsupported)
	}
	if !sps.FrameMBSOnly {
		return fmt.Errorf("%w: field or MBAFF coding", ErrUnsupported)
	}
	if sps.TransformBypass {
		return fmt.Errorf("%w: lossless transform bypass", ErrUnsupported)
	}
	if err := level.CheckCeiling(sps); err != nil {
		return fmt.Errorf("%w: %s", ErrOverLimit, err)
	}
	if d.limits.EnforceLevel {
		if err := level.CheckSPS(sps); err != nil {
			return fmt.Errorf("%w: %s", ErrOverLimit, err)
		}
	}
	if d.limits.MaxFrameMBs > 0 {
		if n := int(sps.MBWidth * sps.MBHeight); n > d.limits.MaxFrameMBs {
			return fmt.Errorf("%w: %d macroblocks per frame, the caller allows %d",
				ErrOverLimit, n, d.limits.MaxFrameMBs)
		}
	}
	return nil
}

func (d *Decoder) scalingFor(sps *h264.SPS, pps *h264.PPS) *scalingTables {
	if d.scalSPS == sps && d.scalPPS == pps && d.scal != nil {
		return d.scal
	}
	d.scal = buildScalingTables(sps, pps)
	d.scalSPS = sps
	d.scalPPS = pps
	return d.scal
}

func (d *Decoder) decodeSlice(u nal.Unit, unit h264.Unit) ([]*frame.Picture, error) {
	id, err := h264.SlicePPSID(unit)
	if err != nil {
		return nil, refusal(err)
	}
	pps := d.ppsMap[id]
	if pps == nil {
		return nil, fmt.Errorf("%w: picture parameter set %d", ErrNoParameters, id)
	}
	sps := d.spsMap[pps.SPSID]
	if sps == nil {
		return nil, fmt.Errorf("%w: sequence parameter set %d", ErrNoParameters, pps.SPSID)
	}
	h, ref, err := h264.ParseSliceReferences(unit, *sps, *pps)
	if err != nil {
		return nil, refusal(err)
	}
	hdr := newSliceHeader(h, ref)
	r := bits.NewReader(u.RBSP)
	if err := r.Seek(hdr.DataBit); err != nil {
		return nil, err
	}
	weighted := pps.WeightedPred && (hdr.SliceType.IsP() || hdr.SliceType.IsSP())
	var out []*frame.Picture
	if hdr.FirstMBInSlice == 0 {
		out = append(out, d.finishPicture()...)
		if hdr.IDR {
			out = append(out, d.bump(true)...)
		}
		d.maxReorder = sps.MaxNumReorder()
		if d.sps != sps || d.buffer == nil {
			d.buffer = newDPB(sps)
		}
		if hdr.IDR {
			d.buffer.refs = d.buffer.refs[:0]
		}
		d.sps = sps
		d.cur = d.newPicture(int(sps.MBWidth), int(sps.MBHeight))
		d.cur.FrameNum = hdr.FrameNum
		d.cur.IDR = hdr.IDR
		d.cur.CropWidth = int(sps.Width)
		d.cur.CropHeight = int(sps.Height)
		d.cur.POC = d.buffer.computePOC(sps, hdr)
		d.grid = d.freshGrid(int(sps.MBWidth), int(sps.MBHeight))
		d.sliceCount = 0
	}
	if d.cur == nil {
		return out, ErrNoParameters
	}
	d.sliceCount++
	d.lastHdr = hdr

	d.buffer.updatePicNums(hdr.FrameNum)
	active := hdr.NumRefIdxL0Active()
	activeL1 := hdr.NumRefIdxL1Active()
	var refList, refListL1 []*frame.Picture
	switch {
	case hdr.SliceType.IsP() || hdr.SliceType.IsSP():
		refList = d.buffer.buildListP(hdr, active)
		if len(refList) == 0 {
			return out, fmt.Errorf("%w: P slice with an empty reference list", ErrCorrupt)
		}
	case hdr.SliceType.IsB():
		refList, refListL1 = d.buffer.buildListsB(hdr, d.cur.POC, active, activeL1)
		if len(refList) == 0 || len(refListL1) == 0 {
			return out, fmt.Errorf("%w: B slice with an empty reference list", ErrCorrupt)
		}
	}

	sd := &sliceDecoder{
		r:               r,
		sps:             sps,
		pps:             pps,
		hdr:             hdr,
		pic:             d.cur,
		grid:            d.grid,
		refList:         refList,
		refListL1:       refListL1,
		numRefIdxActive: active,
		numRefIdxL1:     activeL1,
		scal:            d.scalingFor(sps, pps),
		directSpatial:   hdr.DirectSpatialMvPred,
		direct8x8:       sps.Direct8x8,
		sliceID:         d.sliceCount,
	}
	if len(refListL1) != 0 {
		sd.colocated = refListL1[0]
		sd.colShortTerm = !refListL1[0].LongTerm
	}
	switch {
	case weighted:
		sd.weights = &hdr.PredWeight
		sd.weightMode = weightExplicit
	case hdr.SliceType.IsB() && pps.WeightedBipredIDC == 1:
		sd.weights = &hdr.PredWeight
		sd.weightMode = weightExplicit
	case hdr.SliceType.IsB() && pps.WeightedBipredIDC == 2:
		sd.weightMode = weightImplicit
	}
	if err := sd.run(); err != nil {
		return out, err
	}
	return out, nil
}
