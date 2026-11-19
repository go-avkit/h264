package decoder

import (
	"fmt"

	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/bits"
	"github.com/go-avkit/h264/internal/cabac"
	"github.com/go-avkit/h264/internal/frame"
	"github.com/go-avkit/h264/internal/transform"
)

type sliceDecoder struct {
	// chromaBlocks holds the four AC blocks of one chroma plane in the shape the
	// kernels take (sixteen blocks, four used); a local array would be zeroed
	// on every call.
	chromaBlocks [16]transform.Block

	r    *bits.Reader
	sps  *h264.SPS
	pps  *h264.PPS
	hdr  *sliceHeader
	pic  *frame.Picture
	grid *mbGrid

	refList         []*frame.Picture
	refListL1       []*frame.Picture
	numRefIdxActive int
	numRefIdxL1     int
	directSpatial   bool
	colocated       *frame.Picture
	colShortTerm    bool
	direct8x8       bool
	weights         *predWeights
	weightMode      int

	dirBase directBase // spatial direct, once per B macroblock (binter.go)

	emuLuma   [emuLumaSide * emuLumaSide]byte
	emuChroma [2][emuChromaSide * emuChromaSide]byte
	predA     predBuffer
	predB     predBuffer

	cur *mbState
	nb  neighbours

	mbx int
	mby int

	qpY         int
	sliceID     int
	constrained bool
	scal        *scalingTables

	cb                 *cabac.Decoder
	prevQPDeltaNonZero bool

	subPartsAtLeast8x8 bool
	mvdOverflow        bool

	segments []motionSegment
}

func (d *sliceDecoder) transform8x8Inc() int {
	inc := 0
	if m := d.nb.left; m != nil && m.Transform8x8 {
		inc++
	}
	if m := d.nb.top; m != nil && m.Transform8x8 {
		inc++
	}
	return inc
}

func (d *sliceDecoder) mayUse8x8Transform() bool {
	return d.pps.Transform8x8 && d.cur.cbpLuma != 0 && d.subPartsAtLeast8x8
}

func (d *sliceDecoder) totalMBs() int { return d.grid.widthMBs * d.grid.heightMBs }

// noRef16 is a refIdx table with no reference in any block.
var noRef16 = [16]int8{-1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1, -1}

func (d *sliceDecoder) startMB(mbAddr int) {
	d.mby = mbAddr / d.grid.widthMBs
	d.mbx = mbAddr - d.mby*d.grid.widthMBs
	m := d.grid.at(d.mbx, d.mby)
	d.cur = m
	// Cleared where it lies: a composite literal with fields is built on the
	// stack and copied, which writes the 648 bytes of a cold grid slot twice.
	*m = mbState{}
	m.SliceID = d.sliceID
	m.QPY = d.qpY
	m.ChromaQPOffset = [2]int{int(d.pps.ChromaQPIndexOffset), int(d.pps.SecondChromaQPOffset)}
	m.DisableDeblock = d.hdr.DisableDeblockingFilterIDC
	m.AlphaOffset = int(d.hdr.SliceAlphaC0OffsetDiv2) * 2
	m.BetaOffset = int(d.hdr.SliceBetaOffsetDiv2) * 2
	m.refIdxL0 = noRef16
	m.refIdxL1 = noRef16
	d.cur.Bipredictive = d.hdr.SliceType.IsB()
	if d.cur.Bipredictive {
		d.dirBase.ok = false
	}
	d.subPartsAtLeast8x8 = true
	d.nb = d.grid.around(d.mbx, d.mby, d.sliceID)
}

func (d *sliceDecoder) run() error {
	d.qpY = d.hdr.QPY()
	d.constrained = d.pps.ConstrainedIntraPred
	if d.pps.CABAC {
		d.cb = &cabac.Decoder{}
		return d.runCABAC()
	}
	var res mbResidual

	mbAddr := int(d.hdr.FirstMBInSlice)
	if mbAddr >= d.totalMBs() {
		return fmt.Errorf("%w: first_mb_in_slice %d beyond the picture", ErrCorrupt, mbAddr)
	}
	inter := !d.hdr.SliceType.IsI() && !d.hdr.SliceType.IsSI()

	for {
		if inter {
			run, err := d.r.ReadUE()
			if err != nil {
				return err
			}
			if int(run) > d.totalMBs()-mbAddr {
				return fmt.Errorf("%w: mb_skip_run %d overruns the picture", ErrCorrupt, run)
			}
			for i := uint32(0); i < run; i++ {
				d.startMB(mbAddr)
				if err := d.decodeSkip(); err != nil {
					return err
				}
				d.cur.Decoded = true
				mbAddr++
			}
			if run > 0 && !d.r.MoreRBSPData() {
				return nil
			}
		}
		if mbAddr >= d.totalMBs() {
			return fmt.Errorf("%w: slice extends past the last macroblock", ErrCorrupt)
		}
		d.startMB(mbAddr)
		res.reset()
		if err := d.decodeMacroblock(&res); err != nil {
			return err
		}
		d.cur.Decoded = true

		mbAddr++
		if !d.r.MoreRBSPData() {
			return nil
		}
		if mbAddr >= d.totalMBs() {
			return fmt.Errorf("%w: slice extends past the last macroblock", ErrCorrupt)
		}
	}
}

func (d *sliceDecoder) decodeSkip() error {
	if d.hdr.SliceType.IsB() {
		return d.decodeBSkip()
	}
	return d.decodePSkip()
}

func (d *sliceDecoder) decodeMacroblock(res *mbResidual) error {
	v, err := d.r.ReadUE()
	if err != nil {
		return err
	}
	if d.hdr.SliceType.IsI() {
		info, ok := intraMBType(v)
		if !ok {
			return fmt.Errorf("%w: mb_type %d in an I slice", ErrCorrupt, v)
		}
		return d.decodeIntraMB(info, res)
	}
	if d.hdr.SliceType.IsB() {
		if v < 23 {
			return d.decodeInterMBB(bMBTypes[v], res)
		}
		info, ok := intraMBType(v - 23)
		if !ok {
			return fmt.Errorf("%w: intra mb_type %d in a B slice", ErrCorrupt, v)
		}
		return d.decodeIntraMB(info, res)
	}
	if !d.hdr.SliceType.IsP() && !d.hdr.SliceType.IsSP() {
		return fmt.Errorf("%w: %s slices", ErrUnsupported, d.hdr.SliceType)
	}
	if v < 5 {
		info, ok := interMBType(v)
		if !ok {
			return fmt.Errorf("%w: mb_type %d in a P slice", ErrCorrupt, v)
		}
		return d.decodeInterMB(info, res)
	}
	info, ok := intraMBType(v - 5)
	if !ok {
		return fmt.Errorf("%w: intra mb_type %d in a P slice", ErrCorrupt, v)
	}
	return d.decodeIntraMB(info, res)
}
