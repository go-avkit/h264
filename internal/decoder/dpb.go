package decoder

import (
	"sort"

	h264 "github.com/go-avkit/h264"
	"github.com/go-avkit/h264/internal/frame"
)

type refFrame struct {
	pic          *frame.Picture
	frameNum     uint32
	frameNumWrap int
	picNum       int
	longTerm     bool
	longTermIdx  int
}

type dpb struct {
	refs        []*refFrame
	maxNumRefs  int
	maxFrameNum uint32

	prevPicOrderCntMsb int
	prevPicOrderCntLsb int
	prevFrameNumOffset int
	prevFrameNum       uint32
}

func newDPB(sps *h264.SPS) *dpb {
	n := int(sps.MaxNumRefFrames)
	if n < 1 {
		n = 1
	}
	return &dpb{maxNumRefs: n, maxFrameNum: uint32(1) << sps.Log2MaxFrameNum}
}

func (b *dpb) clear() {
	b.refs = b.refs[:0]
	b.prevPicOrderCntMsb = 0
	b.prevPicOrderCntLsb = 0
	b.prevFrameNumOffset = 0
	b.prevFrameNum = 0
}

func (b *dpb) computePOC(sps *h264.SPS, hdr *sliceHeader) int {
	switch sps.POCType {
	case 0:
		maxLsb := int(1) << sps.Log2MaxPOCLSB
		lsb := int(hdr.PicOrderCntLsb)
		msb := 0
		if hdr.IDR {
			b.prevPicOrderCntMsb = 0
			b.prevPicOrderCntLsb = 0
		} else {
			switch {
			case lsb < b.prevPicOrderCntLsb && b.prevPicOrderCntLsb-lsb >= maxLsb/2:
				msb = b.prevPicOrderCntMsb + maxLsb
			case lsb > b.prevPicOrderCntLsb && lsb-b.prevPicOrderCntLsb > maxLsb/2:
				msb = b.prevPicOrderCntMsb - maxLsb
			default:
				msb = b.prevPicOrderCntMsb
			}
		}
		poc := msb + lsb
		if hdr.NalRefIDC != 0 {
			b.prevPicOrderCntMsb = msb
			b.prevPicOrderCntLsb = lsb
		}
		return poc

	case 2:
		offset := 0
		if !hdr.IDR {
			offset = b.prevFrameNumOffset
			if hdr.FrameNum < b.prevFrameNum {
				offset += int(b.maxFrameNum)
			}
		}
		b.prevFrameNumOffset = offset
		b.prevFrameNum = hdr.FrameNum
		poc := 2 * (offset + int(hdr.FrameNum))
		if hdr.NalRefIDC == 0 {
			poc--
		}
		return poc
	}
	return b.pocType1(sps, hdr)
}

func (b *dpb) pocType1(sps *h264.SPS, hdr *sliceHeader) int {
	offset := 0
	if !hdr.IDR {
		offset = b.prevFrameNumOffset
		if hdr.FrameNum < b.prevFrameNum {
			offset += int(b.maxFrameNum)
		}
	}
	b.prevFrameNumOffset = offset
	b.prevFrameNum = hdr.FrameNum

	cycle := len(sps.OffsetForRefFrame)
	absFrameNum := 0
	if cycle != 0 {
		absFrameNum = offset + int(hdr.FrameNum)
	}
	if hdr.NalRefIDC == 0 && absFrameNum > 0 {
		absFrameNum--
	}
	expected := 0
	if absFrameNum > 0 {
		cycleCnt := (absFrameNum - 1) / cycle
		inCycle := (absFrameNum - 1) % cycle
		var sum int
		for _, v := range sps.OffsetForRefFrame {
			sum += int(v)
		}
		expected = cycleCnt * sum
		for i := 0; i <= inCycle; i++ {
			expected += int(sps.OffsetForRefFrame[i])
		}
	}
	if hdr.NalRefIDC == 0 {
		expected += int(sps.OffsetForNonRefPic)
	}
	return expected + int(hdr.DeltaPicOrderCnt[0])
}

func (b *dpb) updatePicNums(currFrameNum uint32) {
	for _, r := range b.refs {
		if r.longTerm {
			continue
		}
		if r.frameNum > currFrameNum {
			r.frameNumWrap = int(r.frameNum) - int(b.maxFrameNum)
		} else {
			r.frameNumWrap = int(r.frameNum)
		}
		r.picNum = r.frameNumWrap
	}
}

func (b *dpb) shortTermByPicNum(picNum int) *refFrame {
	for _, r := range b.refs {
		if !r.longTerm && r.picNum == picNum {
			return r
		}
	}
	return nil
}

func (b *dpb) longTermByPicNum(num int) *refFrame {
	for _, r := range b.refs {
		if r.longTerm && r.longTermIdx == num {
			return r
		}
	}
	return nil
}

func (b *dpb) remove(target *refFrame) {
	for i, r := range b.refs {
		if r == target {
			b.refs = append(b.refs[:i], b.refs[i+1:]...)
			return
		}
	}
}

func (b *dpb) buildListP(hdr *sliceHeader, active int) []*frame.Picture {
	shortTerm := make([]*refFrame, 0, len(b.refs))
	longTerm := make([]*refFrame, 0, len(b.refs))
	for _, r := range b.refs {
		if r.longTerm {
			longTerm = append(longTerm, r)
		} else {
			shortTerm = append(shortTerm, r)
		}
	}
	sort.SliceStable(shortTerm, func(i, j int) bool {
		return shortTerm[i].picNum > shortTerm[j].picNum
	})
	sort.SliceStable(longTerm, func(i, j int) bool {
		return longTerm[i].longTermIdx < longTerm[j].longTermIdx
	})
	list := append(shortTerm, longTerm...)

	if hdr.ModificationL0Present {
		list = b.applyModifications(list, hdr.RefPicListModificationL0, hdr.FrameNum, active)
	}
	return truncate(list, active)
}

func (b *dpb) splitByOrder(currPOC int) (before, after, long []*refFrame) {
	for _, r := range b.refs {
		switch {
		case r.longTerm:
			long = append(long, r)
		case r.pic.POC < currPOC:
			before = append(before, r)
		default:
			after = append(after, r)
		}
	}
	sort.SliceStable(before, func(i, j int) bool { return before[i].pic.POC > before[j].pic.POC })
	sort.SliceStable(after, func(i, j int) bool { return after[i].pic.POC < after[j].pic.POC })
	sort.SliceStable(long, func(i, j int) bool { return long[i].longTermIdx < long[j].longTermIdx })
	return before, after, long
}

func sameFrames(a, b []*refFrame) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (b *dpb) buildListsB(hdr *sliceHeader, currPOC, activeL0, activeL1 int) ([]*frame.Picture, []*frame.Picture) {
	before, after, long := b.splitByOrder(currPOC)

	list0 := make([]*refFrame, 0, len(b.refs))
	list0 = append(list0, before...)
	list0 = append(list0, after...)
	list0 = append(list0, long...)

	list1 := make([]*refFrame, 0, len(b.refs))
	list1 = append(list1, after...)
	list1 = append(list1, before...)
	list1 = append(list1, long...)

	if len(list1) > 1 && sameFrames(list0, list1) {
		list1[0], list1[1] = list1[1], list1[0]
	}

	if hdr.ModificationL0Present {
		list0 = b.applyModifications(list0, hdr.RefPicListModificationL0, hdr.FrameNum, activeL0)
	}
	if hdr.ModificationL1Present {
		list1 = b.applyModifications(list1, hdr.RefPicListModificationL1, hdr.FrameNum, activeL1)
	}
	return truncate(list0, activeL0), truncate(list1, activeL1)
}

func truncate(list []*refFrame, active int) []*frame.Picture {
	out := make([]*frame.Picture, 0, active)
	for i := 0; i < active && i < len(list); i++ {
		out = append(out, list[i].pic)
	}
	return out
}

func (b *dpb) applyModifications(list []*refFrame, mods []refListMod, frameNum uint32, active int) []*refFrame {
	predPicNum := int(frameNum)
	refIdx := 0
	for _, m := range mods {
		var target *refFrame
		switch m.IDC {
		case 0, 1:
			diff := int(m.Value) + 1
			if m.IDC == 0 {
				predPicNum -= diff
				if predPicNum < 0 {
					predPicNum += int(b.maxFrameNum)
				}
			} else {
				predPicNum += diff
				if predPicNum >= int(b.maxFrameNum) {
					predPicNum -= int(b.maxFrameNum)
				}
			}
			pn := predPicNum
			if pn > int(frameNum) {
				pn -= int(b.maxFrameNum)
			}
			target = b.shortTermByPicNum(pn)
		case 2:
			target = b.longTermByPicNum(int(m.Value))
		}
		if target == nil {
			continue
		}
		list = moveToIndex(list, target, refIdx)
		refIdx++
		if refIdx >= active {
			break
		}
	}
	return list
}

func moveToIndex(list []*refFrame, target *refFrame, idx int) []*refFrame {
	if idx > len(list) {
		idx = len(list)
	}
	out := make([]*refFrame, 0, len(list)+1)
	out = append(out, list[:idx]...)
	out = append(out, target)
	for _, r := range list[idx:] {
		if r != target {
			out = append(out, r)
		}
	}
	return out
}

func (b *dpb) store(pic *frame.Picture, hdr *sliceHeader) {
	if hdr.NalRefIDC == 0 {
		return
	}
	if hdr.IDR {
		b.refs = b.refs[:0]
		entry := &refFrame{pic: pic, frameNum: hdr.FrameNum, longTerm: hdr.LongTermReference}
		pic.LongTerm = hdr.LongTermReference
		b.refs = append(b.refs, entry)
		b.updatePicNums(hdr.FrameNum)
		return
	}
	if hdr.AdaptiveRefPicMarking {
		entry := &refFrame{pic: pic, frameNum: hdr.FrameNum}
		b.applyMMCO(hdr, entry)
		b.refs = append(b.refs, entry)
		b.updatePicNums(hdr.FrameNum)
		b.enforceCapacity()
		return
	}
	b.refs = append(b.refs, &refFrame{pic: pic, frameNum: hdr.FrameNum})
	b.updatePicNums(hdr.FrameNum)
	b.slidingWindow()
	b.enforceCapacity()
}

func (b *dpb) enforceCapacity() {
	n := b.maxNumRefs
	if n < 1 {
		n = 1
	}
	if len(b.refs) <= n {
		return
	}
	b.refs = append(b.refs[:0], b.refs[len(b.refs)-n:]...)
}

func (b *dpb) applyMMCO(hdr *sliceHeader, current *refFrame) {
	for _, m := range hdr.MMCOs {
		switch m.Op {
		case 1:
			pn := int(hdr.FrameNum) - int(m.DifferenceOfPicNumsMinus1) - 1
			if r := b.shortTermByPicNum(pn); r != nil {
				b.remove(r)
			}
		case 2:
			if r := b.longTermByPicNum(int(m.LongTermPicNum)); r != nil {
				b.remove(r)
			}
		case 3:
			pn := int(hdr.FrameNum) - int(m.DifferenceOfPicNumsMinus1) - 1
			if r := b.shortTermByPicNum(pn); r != nil {
				r.longTerm = true
				r.longTermIdx = int(m.LongTermFrameIdx)
				r.pic.LongTerm = true
			}
		case 4:
			max := int(m.MaxLongTermFrameIdxPlus1) - 1
			for i := 0; i < len(b.refs); {
				if b.refs[i].longTerm && b.refs[i].longTermIdx > max {
					b.refs = append(b.refs[:i], b.refs[i+1:]...)
					continue
				}
				i++
			}
		case 5:
			b.refs = b.refs[:0]
		case 6:
			if r := b.longTermByPicNum(int(m.LongTermFrameIdx)); r != nil {
				b.remove(r)
			}
			current.longTerm = true
			current.longTermIdx = int(m.LongTermFrameIdx)
			current.pic.LongTerm = true
		}
	}
}

func (b *dpb) slidingWindow() {
	for len(b.refs) > b.maxNumRefs {
		oldest := -1
		var wrap int
		for i, r := range b.refs {
			if r.longTerm {
				continue
			}
			if oldest < 0 || r.frameNumWrap < wrap {
				oldest = i
				wrap = r.frameNumWrap
			}
		}
		if oldest < 0 {
			return
		}
		b.refs = append(b.refs[:oldest], b.refs[oldest+1:]...)
	}
}
