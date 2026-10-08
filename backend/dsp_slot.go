// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"math"
	"strings"
	"time"
)

const (
	regSlotAddr = 0x48
	regSlotData = 0x4C
	regStatus   = 0x50
	regCap0     = 0x54
	regCap1     = 0x58
	regCap2     = 0x5C
	regCap3     = 0x60

	regCap4 = 0x64
)

const (
	capMagic    = 0x5A44
	ctrlCommit  = 1 << 16
	ctrlBankSel = 1 << 17

	capOpcodeLimTP = 1 << 9

	capOpcodeHeadroom = 1 << 10

	capOpcodeMix2 = 1 << 3

	capOpcodeDyn = 1 << 11

	slotWordSize = 8
	slotHdrAddr  = dspSlotMax * 8

	dspSlotMax = 24

	maxSections = 48
	secPerSlot  = 3
	opNop       = 0
	opBiquad    = 1
	opDyn       = 2
	opMix2      = 3
	opDelay     = 4

	capOpcodeDelay = 1 << 4

	capOpcodeFIR = 1 << 5

	capOpcodeSFIR = 1 << 7

	capOpcodeJointStereo = 1 << 12

	opJDST = 9
	opJ3DS = 10

	jointStateReserve = 2

	colorfulJointCycles = 40

	opSFIR = 7

	opFIR = 5

	firTaps = 8192

	firMACS = 16

	firCycles = 256

	sfirTaps = 64
	sfirMACS = 8

	sfirOvh = 11

	sfirCycles = sfirTaps/sfirMACS + sfirOvh

	coefFIRBase = 2 * coefWordsPerBank

	coefSFIRBase = coefFIRBase + 2*firTaps

	opPoly = 6

	capOpcodePoly = 1 << 6

	opSat    = 8
	flBypass = 1 << 0
)

const (
	swCfg  = 0
	swCfb  = 1
	swStb  = 2
	swInA  = 3
	swInB  = 4
	swOutB = 5
	swPrm  = 6
	swRsv  = 7
)

var dspEngineGen int

type dspEngineCaps struct {
	Present    bool
	Version    uint16
	Opcodes    uint32
	NumSlots   int
	FirMaxLog2 int
	DelayLog2  int

	NumSections  int
	NumCoefWords int

	DelaySlots int

	CostPerSection int
	CostPoly       int

	SFIRMaxLog2  int
	SFIRMACS     int
	SFIROverhead int
}

func dspProbeEngine() {
	dspEngineGen = 0
	if !dspAvailable {
		return
	}
	c0 := regRead(regCap0)
	if uint16(c0>>16) != capMagic {

		log.Printf("DSP engine: legacy bitstream (no CAP magic, read back %#x) => using fixed 6-section path", c0)
		return
	}
	dspEngineGen = 1
	caps := dspReadEngineCaps()
	log.Printf("DSP engine: 0.2 slot-table engine v%d (%d slots, opcode bitmap %#08x, FIR log2=%d, DELAY log2=%d)",
		caps.Version, caps.NumSlots, caps.Opcodes, caps.FirMaxLog2, caps.DelayLog2)
}

func dspReadEngineCaps() dspEngineCaps {
	c0 := regRead(regCap0)
	c1 := regRead(regCap1)
	c2 := regRead(regCap2)
	c3 := regRead(regCap3)
	c4 := regRead(regCap4)
	return dspEngineCaps{
		Present:        uint16(c0>>16) == capMagic,
		Version:        uint16(c0 & 0xFFFF),
		Opcodes:        c1,
		NumSlots:       int(c2 & 0xFF),
		FirMaxLog2:     int((c2 >> 8) & 0xFF),
		DelayLog2:      int((c2 >> 16) & 0xFF),
		NumSections:    int((c2 >> 24) & 0xFF),
		NumCoefWords:   int((c3 >> 24) & 0xFF),
		DelaySlots:     int((c3 >> 16) & 0xFF),
		CostPerSection: int(c3 & 0xFF),
		CostPoly:       int((c3 >> 8) & 0xFF),

		SFIRMaxLog2:  int(c4 & 0xFF),
		SFIRMACS:     int((c4 >> 8) & 0xFF),
		SFIROverhead: int((c4 >> 16) & 0xFF),
	}
}

var (
	hwMaxSections = maxSections
	hwCoefWords   = coefWordsPerBank
	hwCoefFIRBase = coefFIRBase
	hwDelayWords  = maxDelayWords

	hwDelaySlots = 1

	hwSFIRMaxTaps  = sfirTaps
	hwSFIROvh      = sfirOvh
	hwCoefSFIRBase = coefSFIRBase
)

func dspAdoptHardwareCaps() {
	if dspEngineGen != 1 {
		return
	}
	adoptCapsFrom(dspReadEngineCaps())
}

func adoptCapsFrom(caps dspEngineCaps) {
	if n := caps.NumSections; n > 0 {
		if n < hwMaxSections {
			log.Printf("⚠️ Bitstream reports NSEC=%d, is below the backend section cap %d => **tightened to %d**"+
				"(new-backend/old-bitstream silent coefficient-drop case, hardware wins)",
				n, hwMaxSections, n)
		}
		hwMaxSections = n
	}
	if n := caps.NumCoefWords; n > 0 {
		if n != hwCoefWords {
			log.Printf("Bitstream reports NCOEF=%d (backend const %d) => coefficient space and FIR base follow hardware",
				n, hwCoefWords)
		}
		hwCoefWords = n
		hwCoefFIRBase = 2 * n

		hwCoefSFIRBase = hwCoefFIRBase + 2*firTaps
	}

	if caps.SFIRAvailable() && caps.SFIRMACS > 0 && caps.SFIRMaxLog2 > 0 {
		taps := caps.SFIRMACS << caps.SFIRMaxLog2
		ovh := caps.SFIROverhead
		if taps != hwSFIRMaxTaps || ovh != hwSFIROvh {
			log.Printf("Bitstream reports small FIR: taps <=%d (%d MAC/cycle, fixed overhead %d cycles) => following hardware",
				taps, caps.SFIRMACS, ovh)
		}
		hwSFIRMaxTaps = taps
		hwSFIROvh = ovh
	} else if !caps.SFIRAvailable() {

		hwSFIRMaxTaps = 0
	}
	if n := caps.DelaySlots; n > 0 {
		if n != hwDelaySlots {
			log.Printf("Bitstream reports %d independent delay slot pointers per channel (CAP3[23:16]) => %d delay", n, n)
		}
		hwDelaySlots = n
	}
	if n := caps.DelayLog2; n > 0 {

		hwDelayWords = 1 << n
	}
	log.Printf("Runtime capacity: sections <=%d, coefficients %d words/bank, FIR base %d, small-FIR base %d (taps <=%d),"+
		"delay ring %d words/channel (%.1f ms @48k)",
		hwMaxSections, hwCoefWords, hwCoefFIRBase, hwCoefSFIRBase, hwSFIRMaxTaps,
		hwDelayWords, float64(hwDelayWords)/48.0)
}

const frameBudgetCycles = 1041

func costPerSectionRuntime() int {
	if n := dspReadEngineCaps().CostPerSection; n >= 7 {
		return n
	}
	return 12
}

func costPolyRuntime() int {
	if n := dspReadEngineCaps().CostPoly; n >= 20 {
		return n
	}
	return 26
}

func sfirCyclesRuntime() int {
	if !dspAvailable || dspEngineGen != 1 {
		return sfirCycles
	}
	c := dspReadEngineCaps()
	if c.SFIRMACS > 0 && c.SFIRAvailable() {
		taps := c.SFIRMACS << c.SFIRMaxLog2
		if taps > 0 && c.SFIROverhead > 0 {
			return taps/c.SFIRMACS + c.SFIROverhead
		}
	}
	return sfirCycles
}

func chainFrameCost(sections int, hasFIR, hasPOLY, hasSFIR, hasJoint bool, slots int) int {
	cost := sections*costPerSectionRuntime() + slots*2
	if hasFIR {
		cost += firCycles
	}
	if hasSFIR {
		cost += sfirCyclesRuntime()
	}
	if hasPOLY {
		cost += costPolyRuntime()
	}
	if hasJoint {
		cost += colorfulJointCycles
	}
	return cost
}

func dspReadStatus() uint32 {
	return regRead(regStatus)
}

func (c dspEngineCaps) TruepeakAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeLimTP != 0
}

func (c dspEngineCaps) HeadroomAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeHeadroom != 0
}

func (c dspEngineCaps) JointStereoAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeJointStereo != 0
}

func (c dspEngineCaps) DelayAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeDelay != 0
}

func (c dspEngineCaps) Mix2Available() bool {
	return c.Present && c.Opcodes&capOpcodeMix2 != 0
}

type firParams struct {
	Taps int    `json:"taps"`
	Name string `json:"name,omitempty"`
}

var currentFIR *firParams

func firGainDBForHeadroom() float64 {
	irMu.Lock()
	defer irMu.Unlock()
	if currentFIR == nil || len(irBank[0]) == 0 {
		return 0
	}
	var worst float64
	for _, ch := range irBank {
		var l1 float64
		for _, v := range ch {
			if v < 0 {
				l1 += -float64(v)
			} else {
				l1 += float64(v)
			}
		}
		if l1 > worst {
			worst = l1
		}
	}
	if worst <= 0 {
		return 0
	}
	return 20 * math.Log10(worst/32768.0)
}

func firParamsBlocks(p *firParams) int {
	taps := firTaps
	if p != nil && p.Taps > 0 && p.Taps <= firTaps {
		taps = p.Taps
	}
	blocks := taps / firMACS
	if blocks < 1 {
		blocks = 1
	}
	lg := 0
	for (1 << lg) < blocks {
		lg++
	}
	return 1 << lg
}

func firAvailable() bool {
	return dspEngineGen == 1 && dspReadEngineCaps().FIRAvailable()
}

func (c dspEngineCaps) FIRAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeFIR != 0
}

func (c dspEngineCaps) DynAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeDyn != 0
}

func (c dspEngineCaps) BiquadAvailable() bool {
	return c.Present && c.Opcodes&(1<<1) != 0
}

func (c dspEngineCaps) PolyAvailable() bool {
	return c.Present && c.Opcodes&capOpcodePoly != 0
}

func (c dspEngineCaps) SFIRAvailable() bool {
	return c.Present && c.Opcodes&capOpcodeSFIR != 0
}

func sfirAvailableNow() bool {
	if !dspAvailable || dspEngineGen != 1 {
		return false
	}
	c := dspReadEngineCaps()
	return c.SFIRAvailable() && c.SFIRMACS > 0 && c.SFIRMaxLog2 > 0
}

func slotLimit() int {
	if !dspAvailable || dspEngineGen != 1 {
		return dspSlotMax
	}
	if n := dspReadEngineCaps().NumSlots; n > 0 && n < dspSlotMax {
		return n
	}
	return dspSlotMax
}

type slotPlan struct {
	Words []slotWord

	Coefs []int32

	FirCoefs []int32

	SFirCoefs []int32

	Slots int

	Sections int

	StereoFrame bool

	JBus int

	HasBassMix  bool
	BassMixCoef int
}

type slotWord struct {
	Addr  int
	Value uint32
}

func buildSlotPlan(sections [][5]int32) (slotPlan, error) {
	var p slotPlan
	if len(sections) == 0 {

		p.Slots = 0
		return p, nil
	}
	if len(sections) > hwMaxSections {
		return p, newCapacityError("sections", len(sections), hwMaxSections,
			"Chain too long: %d sections > engine limit %d sections (coefficient/state RAM cannot hold it, will not truncate)",
			len(sections), hwMaxSections)
	}

	sec := 0
	slot := 0
	for sec < len(sections) {
		n := len(sections) - sec
		if n > secPerSlot {
			n = secPerSlot
		}
		cfg := uint32(opBiquad) | uint32(n)<<16
		p.Words = append(p.Words,
			slotWord{slot*slotWordSize + swCfg, cfg},
			slotWord{slot*slotWordSize + swCfb, uint32(sec * 5)},
			slotWord{slot*slotWordSize + swStb, uint32(sec)},
			slotWord{slot*slotWordSize + swInA, uint32(slot)},
			slotWord{slot*slotWordSize + swInB, 0xFFFF},
			slotWord{slot*slotWordSize + swOutB, uint32(slot + 1)},
			slotWord{slot*slotWordSize + swPrm, 0},
			slotWord{slot*slotWordSize + swRsv, 0},
		)
		for k := 0; k < n; k++ {
			c := sections[sec+k]
			p.Coefs = append(p.Coefs, c[0], c[1], c[2], c[3], c[4])
		}
		sec += n
		slot++
	}
	p.Slots = slot
	p.Sections = sec
	if p.StereoFrame {
		p.Sections += jointStateReserve
	}
	return p, nil
}

func dspActiveBankCtrl() uint32 {
	if dspReadStatus()&0x2 != 0 {
		return ctrlBankSel
	}
	return 0
}

func dspWriteActiveCoef(idx int, v int32) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if dspEngineGen != 1 {
		return fmt.Errorf("Bitstream is not the 0.2 slot-table engine (CAP0 has no magic), no runtime-writable coefficient RAM")
	}
	if idx < 0 || idx >= hwCoefWords {
		return fmt.Errorf("Coefficient index %d out of range (this bitstream has only %d words)", idx, hwCoefWords)
	}
	dspSetCtrlBits(ctrlBankSel, dspActiveBankCtrl())
	regWrite(regCidx, uint32(idx))
	regWrite(regCdat, uint32(v)&coefMask)
	return nil
}

func dspDownloadSlotPlan(p slotPlan) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if dspEngineGen != 1 {
		return fmt.Errorf("Bitstream is not the 0.2 slot-table engine (CAP0 has no magic), cannot download slot table")
	}

	antiPopCancelRamp()

	st := dspReadStatus()
	bank := uint32(0)
	if st&0x2 == 0 {
		bank = ctrlBankSel
	}
	dspSetCtrlBits(ctrlBankSel, bank)

	for i, c := range p.Coefs {
		regWrite(regCidx, uint32(i))
		regWrite(regCdat, uint32(c)&coefMask)
	}

	for i, c := range p.FirCoefs {
		regWrite(regCidx, uint32(hwCoefFIRBase+i))
		regWrite(regCdat, uint32(c)&coefMask)
	}

	for i, c := range p.SFirCoefs {
		regWrite(regCidx, uint32(hwCoefSFIRBase+i))
		regWrite(regCdat, uint32(c)&coefMask)
	}

	regWrite(regSlotAddr, 0)
	for _, w := range p.Words {
		regWrite(regSlotData, w.Value)
	}

	hdr := uint32(p.Slots) & 0xFF
	if p.StereoFrame {
		hdr |= 1 << 8
		hdr |= (uint32(p.JBus) & 0x1F) << 9
	}
	regWrite(regSlotAddr, slotHdrAddr)
	regWrite(regSlotData, hdr)

	var opseq []string
	for i := 0; i < p.Slots; i++ {
		for _, w := range p.Words {
			if w.Addr == i*slotWordSize+swCfg {
				opseq = append(opseq, fmt.Sprintf("%d", w.Value&0xFF))
			}
		}
	}
	log.Printf("Slot table download: %d slots / %d sections / %d coefficients (bank %d) slot order=[%s] order key=0NOP 1BIQUAD 2DYN 3MIX2 4DELAY 5FIR 6POLY 7SFIR 8SAT 9JDST 10J3DS",
		p.Slots, p.Sections, len(p.Coefs), 1-(bank>>17), strings.Join(opseq, ","))

	dspSetCtrlBits(ctrlCommit, ctrlCommit)

	deadline := time.Now().Add(50 * time.Millisecond)
	for dspReadStatus()&0x1 != 0 {
		if time.Now().After(deadline) {
			return fmt.Errorf("COMMIT timed out without taking effect (STATUS=%#x)", dspReadStatus())
		}
		time.Sleep(200 * time.Microsecond)
	}

	bank = 0
	if dspReadStatus()&0x2 != 0 {
		bank = ctrlBankSel
	}
	dspSetCtrlBits(ctrlBankSel, bank)
	return nil
}

func dspSetCtrlBits(mask, val uint32) {
	cur := regRead(regCtrl)
	regWrite(regCtrl, (cur&^mask)|(val&mask))
}

const (
	dynScratchBus = 23

	crossLoBus = 22

	maxDelayWords = 8192

	flDlyRight = 0x04

	planKindBiquad = "biquad"
	planKindDyn    = "dyn"
	planKindCross  = "cross"
	planKindDelay  = "delay"
	planKindFIR    = "fir"

	planKindExciter = "exciter"

	planKindAnalogX = "analogx"

	planKindXHIFI = "xhifi"

	planKindDynBass = "dynbass"

	planKindViPERBass = "viperbass"

	planKindViPERBassPBP = "viperbass_pbp"

	planKindColorfulMusic = "colorfulmusic"

	coefWordsPerBank = 240
)

type planNode struct {
	Kind  string
	Coefs [5]int32
	Side  [5]int32

	Hi  [5]int32
	Mix [2]int32

	Len   int
	Flags int

	Blocks int

	Poly [12]int32

	Extra [5]int32

	MixB [2]int32

	Joint [9]int32

	Mix3D [2]int32

	SFir []int32
}

func buildSlotPlanNodes(nodes []planNode) (slotPlan, error) {
	var p slotPlan
	if len(nodes) == 0 {
		p.Slots = 0
		return p, nil
	}

	sections := 0
	for _, n := range nodes {
		switch n.Kind {
		case planKindDyn:
			sections += 2
		case planKindCross:
			sections += 2
		case planKindDelay:

		case planKindXHIFI:

			sections += 12
		case planKindAnalogX:

			sections += 5
		case planKindExciter:

			sections += 3
			if n.MixB[0] != 0 {
				sections++
			}
		case planKindViPERBass:

			sections += 3
		case planKindViPERBassPBP:

			sections += 3
		case planKindDynBass:

			sections += dynamicBassSections
		case planKindColorfulMusic:

			sections += jointStateReserve
		case planKindFIR:

		default:
			sections++
		}
	}
	if sections > hwMaxSections {
		return p, newCapacityError("sections", sections, hwMaxSections,
			"Chain too long: %d sections (incl. DYN detection bandpass)> engine limit %d sections, will not truncate",
			sections, hwMaxSections)
	}

	sec := 0
	coefIdx := 0
	slot := 0
	bus := 0

	peakBus := 0
	noteBus := func(b int) {
		if b == 0xFFFF || b == crossLoBus || b == dynScratchBus {
			return
		}
		if b > peakBus {
			peakBus = b
		}
	}
	i := 0
	emitF := func(op int, n int, flags int, cfb int, stb int, inA int, inB int, outB int) error {

		if slot >= slotLimit() {
			return newCapacityError("slots", slot+1, slotLimit(),
				"Out of slots: this chain needs %d slots, engine has only %d"+
					"(takes the smaller of CAP2-reported NSLOT and the software const; overflow would wrap and clobber earlier slots,"+
					"so it is rejected here)", slot+1, slotLimit())
		}
		cfg := uint32(op) | uint32(n)<<16 | uint32(flags&0xFF)<<8
		noteBus(inA)
		noteBus(inB)
		noteBus(outB)
		p.Words = append(p.Words,
			slotWord{slot*slotWordSize + swCfg, cfg},
			slotWord{slot*slotWordSize + swCfb, uint32(cfb)},
			slotWord{slot*slotWordSize + swStb, uint32(stb)},
			slotWord{slot*slotWordSize + swInA, uint32(inA)},
			slotWord{slot*slotWordSize + swInB, uint32(inB)},
			slotWord{slot*slotWordSize + swOutB, uint32(outB)},
			slotWord{slot*slotWordSize + swPrm, 0},
			slotWord{slot*slotWordSize + swRsv, 0},
		)
		slot++
		return nil
	}
	emit := func(op int, n int, cfb int, stb int, inA int, inB int, outB int) error {
		return emitF(op, n, 0, cfb, stb, inA, inB, outB)
	}

	delayUsed := 0
	delaySlots := 0
	allocDelay := func(L int) (int, error) {
		if delaySlots >= hwDelaySlots {
			return 0, fmt.Errorf("This frame needs %d delay slots, bitstream supports only %d"+
				"(CAP3[23:16]; legacy bitstreams have a single delay-ring write pointer, two delay slots would clobber each other)",
				delaySlots+1, hwDelaySlots)
		}
		if delayUsed+L > hwDelayWords {
			return 0, fmt.Errorf("Delay ring too small: allocated %d words + this slot %d words > per-channel ring capacity %d words"+
				"(%.1f ms @48k) - multiple delay slots **share one ring**",
				delayUsed, L, hwDelayWords, float64(hwDelayWords)/48.0)
		}
		off := delayUsed
		delayUsed += L
		delaySlots++
		return off, nil
	}

	for i < len(nodes) {
		if nodes[i].Kind == planKindXHIFI {
			n := nodes[i]

			base := bus
			dBP, dLP := n.Flags, n.Len
			for k, d := range []int{dBP, dLP} {
				if d < 1 || d > hwDelayWords {
					return p, fmt.Errorf("Clarity XHIFI branch %d delay %d samples exceeds delay ring %d words",
						k+1, d, hwDelayWords)
				}
			}
			offBP, err := allocDelay(dBP)
			if err != nil {
				return p, fmt.Errorf("Clarity XHIFI BP branch delay: %w", err)
			}
			offLP, err := allocDelay(dLP)
			if err != nil {
				return p, fmt.Errorf("Clarity XHIFI LP branch delay: %w", err)
			}

			if coefIdx+70 > hwCoefWords {
				return p, fmt.Errorf("Coefficient RAM too small: Clarity XHIFI needs %d..%d, each bank has only %d words",
					coefIdx, coefIdx+69, hwCoefWords)
			}

			hb := coefIdx
			for k := 0; k < 3; k++ {
				p.Coefs = append(p.Coefs, n.Coefs[:]...)
			}
			coefIdx += 15
			if err := emit(opBiquad, 3, hb, sec, base, 0xFFFF, base+1); err != nil {
				return p, err
			}
			sec += 3

			lpb := coefIdx
			for k := 0; k < 3; k++ {
				p.Coefs = append(p.Coefs, n.Hi[:]...)
			}
			coefIdx += 15
			if err := emit(opBiquad, 3, lpb, sec, base, 0xFFFF, base+2); err != nil {
				return p, err
			}
			sec += 3

			hpb := coefIdx
			for k := 0; k < 3; k++ {
				p.Coefs = append(p.Coefs, n.Side[:]...)
			}
			coefIdx += 15
			if err := emit(opBiquad, 3, hpb, sec, base+2, 0xFFFF, base+3); err != nil {
				return p, err
			}
			sec += 3

			db := coefIdx
			p.Coefs = append(p.Coefs, int32(dBP), 0, 0, 0, 0)
			coefIdx += 5
			if err := emitF(opDelay, 1, 0, db, offBP, base+3, 0xFFFF, base+4); err != nil {
				return p, err
			}

			lb := coefIdx
			p.Coefs = append(p.Coefs, n.Extra[:]...)
			coefIdx += 5
			if err := emit(opBiquad, 1, lb, sec, base, 0xFFFF, base+2); err != nil {
				return p, err
			}
			sec++
			dl := coefIdx
			p.Coefs = append(p.Coefs, int32(dLP), 0, 0, 0, 0)
			coefIdx += 5
			if err := emitF(opDelay, 1, 0, dl, offLP, base+2, 0xFFFF, base+3); err != nil {
				return p, err
			}

			m1 := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, m1, sec, base+1, base+4, base+4); err != nil {
				return p, err
			}
			sec++
			m2 := coefIdx
			p.Coefs = append(p.Coefs, n.MixB[0], n.MixB[1], 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, m2, sec, base+4, base+3, base+1); err != nil {
				return p, err
			}
			sec++
			bus = base + 1
			i++
			continue
		}
		if nodes[i].Kind == planKindAnalogX {
			n := nodes[i]

			hb := coefIdx
			for _, v := range n.Coefs {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, hb, sec, bus, 0xFFFF, bus+1); err != nil {
				return p, err
			}
			sec++

			pb := coefIdx
			if coefIdx+12 > hwCoefWords {
				return p, fmt.Errorf("Coefficient RAM too small: AnalogX POLY needs %d..%d, each bank has only %d words",
					coefIdx, coefIdx+11, hwCoefWords)
			}
			for _, v := range n.Poly {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 12
			if err := emitF(opPoly, 11, 0, pb, sec, bus+1, 0xFFFF, bus+2); err != nil {
				return p, err
			}
			sec++

			mb := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, mb, sec, bus, bus+2, bus+3); err != nil {
				return p, err
			}
			sec++

			lb := coefIdx
			for _, v := range n.Hi {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, lb, sec, bus+3, 0xFFFF, bus+4); err != nil {
				return p, err
			}
			sec++

			kb := coefIdx
			for _, v := range n.Side {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, kb, sec, bus+4, 0xFFFF, bus+5); err != nil {
				return p, err
			}
			sec++
			bus += 5
			i++
			continue
		}
		if nodes[i].Kind == planKindExciter {
			n := nodes[i]

			hb := coefIdx
			for _, v := range n.Coefs {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, hb, sec, bus, 0xFFFF, bus+1); err != nil {
				return p, err
			}
			sec++

			pb := coefIdx
			if coefIdx+12 > hwCoefWords {
				return p, fmt.Errorf("Coefficient RAM too small: harmonic exciter needs %d..%d, each bank has only %d words",
					coefIdx, coefIdx+11, hwCoefWords)
			}
			for _, v := range n.Poly {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 12

			if err := emitF(opPoly, 11, 0, pb, sec, bus+1, 0xFFFF, bus+2); err != nil {
				return p, err
			}
			sec++

			lb := coefIdx
			for _, v := range n.Hi {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, lb, sec, bus+2, 0xFFFF, bus+3); err != nil {
				return p, err
			}
			sec++

			if n.MixB[0] != 0 {
				gb := coefIdx
				p.Coefs = append(p.Coefs, n.MixB[0], 0, 0, 0, 0)
				coefIdx += 5
				if err := emit(opMix2, 1, gb, sec, bus+3, bus+3, bus+3); err != nil {
					return p, err
				}
				sec++
			}

			mb := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, mb, sec, bus, bus+3, bus+4); err != nil {
				return p, err
			}
			bus += 4
			i++
			continue
		}
		if nodes[i].Kind == planKindFIR {
			n := nodes[i]
			blocks := n.Blocks
			if blocks == 0 {
				blocks = firTaps / firMACS
			}
			lg := 0
			for (1 << lg) < blocks {
				lg++
			}
			if (1 << lg) != blocks {
				return p, fmt.Errorf("FIR MACS block count must be a power of 2 (got %d): slot CFG field n uses log2 encoding", blocks)
			}
			if (firMACS << lg) > firTaps {
				return p, fmt.Errorf("FIR needs %d taps, exceeds unit limit %d", firMACS<<lg, firTaps)
			}

			irMu.Lock()
			loading := irLoading
			irMu.Unlock()
			flags := n.Flags
			if loading {
				flags |= flBypass
			}
			if err := emitF(opFIR, lg, flags, hwCoefFIRBase, 0, bus, 0xFFFF, bus+1); err != nil {
				return p, err
			}
			bus++
			i++
			continue
		}
		if nodes[i].Kind == planKindDynBass {

			n := nodes[i]
			base := bus

			if err := emit(opNop, 0, 0, 0, base, 0xFFFF, crossLoBus); err != nil {
				return p, err
			}

			sb := coefIdx
			p.Coefs = append(p.Coefs, dynamicBassPreScaleWeight(), dynamicBassPreScaleWeight(), 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, sb, sec, base, crossLoBus, base+1); err != nil {
				return p, err
			}
			sec++

			lb := coefIdx
			p.Coefs = append(p.Coefs, n.Coefs[:]...)
			coefIdx += 5
			if err := emit(opBiquad, 1, lb, sec, base+1, 0xFFFF, base+2); err != nil {
				return p, err
			}
			sec++

			ab := coefIdx
			p.Coefs = append(p.Coefs, q315Round(1.0), q315Round(1.0), 0, 0, 0)
			coefIdx += 5
			if err := emit(opMix2, 1, ab, sec, base, base+2, base+3); err != nil {
				return p, err
			}
			sec++
			bus = base + 3
			i++
			continue
		}
		if nodes[i].Kind == planKindViPERBass {
			n := nodes[i]

			base := bus
			pb := coefIdx
			p.Coefs = append(p.Coefs, n.Extra[:]...)
			p.Coefs = append(p.Coefs, n.Coefs[:]...)
			coefIdx += 10
			if err := emit(opBiquad, 2, pb, sec, base, 0xFFFF, base+1); err != nil {
				return p, err
			}
			sec += 2

			mb := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5

			p.HasBassMix, p.BassMixCoef = true, mb
			if err := emit(opMix2, 1, mb, sec, base, base+1, base+1); err != nil {
				return p, err
			}
			sec++
			bus++
			i++
			continue
		}
		if nodes[i].Kind == planKindViPERBassPBP {
			n := nodes[i]

			if hwSFIRMaxTaps < sfirTaps {
				return p, fmt.Errorf("ViPERBass Pure Bass+ needs **%d-tap small FIR**"+
					"(that is the official core' s dry path, not bypass), but this bitstream' s small-FIR cap is %d:"+
					"CAP1 bit7 (has OP_SFIR) and CAP4 (capacity) must both be set", sfirTaps, hwSFIRMaxTaps)
			}
			if len(n.SFir) != sfirTaps {
				return p, fmt.Errorf("ViPERBass PBP dry-path taps must be %d, got %d",
					sfirTaps, len(n.SFir))
			}
			L := n.Len
			if L < 1 || L > hwDelayWords {
				return p, fmt.Errorf("ViPERBass PBP wet delay %d samples exceeds delay ring %d words",
					L, hwDelayWords)
			}
			base := bus

			db := coefIdx
			p.Coefs = append(p.Coefs, int32(L), 0, 0, 0, 0)
			coefIdx += 5
			off, err := allocDelay(L)
			if err != nil {
				return p, fmt.Errorf("ViPERBass PBP wet delay: %w", err)
			}
			if err := emitF(opDelay, 1, 0, db, off, base, 0xFFFF, base+1); err != nil {
				return p, err
			}

			pb := coefIdx
			p.Coefs = append(p.Coefs, n.Extra[:]...)
			p.Coefs = append(p.Coefs, n.Coefs[:]...)
			coefIdx += 10
			if err := emit(opBiquad, 2, pb, sec, base+1, 0xFFFF, base+2); err != nil {
				return p, err
			}
			sec += 2

			sfirBlocks := sfirTaps / sfirMACS
			lg := 0
			for (1 << lg) < sfirBlocks {
				lg++
			}
			if (1 << lg) != sfirBlocks {
				return p, fmt.Errorf("Small FIR block count %d is not a power of 2 (slot CFG field n uses log2 encoding)", sfirBlocks)
			}

			if err := emitF(opSFIR, lg, 0, hwCoefSFIRBase, 0, base, 0xFFFF, base+3); err != nil {
				return p, err
			}

			p.SFirCoefs = append([]int32(nil), n.SFir...)
			p.SFirCoefs = append(p.SFirCoefs, n.SFir...)

			mb := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5

			p.HasBassMix, p.BassMixCoef = true, mb
			if err := emit(opMix2, 1, mb, sec, base+3, base+2, base+2); err != nil {
				return p, err
			}
			sec++
			bus = base + 2
			i++
			continue
		}
		if nodes[i].Kind == planKindColorfulMusic {

			n := nodes[i]
			base := bus
			db := coefIdx

			for _, v := range n.Joint {
				p.Coefs = append(p.Coefs, v)
			}
			p.Coefs = append(p.Coefs, 0)
			coefIdx += 10

			off, err := allocDelay(colorfulJointDelayWords(sampleRate))
			if err != nil {
				return p, fmt.Errorf("ColorfulMusic two delays: %w", err)
			}
			if err := emitF(opJDST, 1, 0, db, off, base+1, base, base+2); err != nil {
				return p, err
			}
			mb := coefIdx
			p.Coefs = append(p.Coefs, n.Mix3D[0], n.Mix3D[1], 0, 0, 0)
			coefIdx += 5
			if err := emitF(opJ3DS, 1, 0, mb, 0, base+1, base+2, 0xFFFF); err != nil {
				return p, err
			}
			bus = base + 3
			noteBus(bus)
			p.StereoFrame = true
			p.JBus = bus
			i++
			continue
		}
		if nodes[i].Kind == planKindDelay {
			n := nodes[i]
			L := n.Len
			if L < 1 {
				return p, fmt.Errorf("Delay length must be >=1 (got %d)", L)
			}
			if L > hwDelayWords {
				return p, fmt.Errorf("Delay %d samples exceeds limit %d (per-channel delay ring is only %d words = %.1f ms @48k)",
					L, hwDelayWords, hwDelayWords, float64(hwDelayWords)/48.0)
			}
			base := coefIdx
			p.Coefs = append(p.Coefs, int32(L), 0, 0, 0, 0)
			coefIdx += 5

			off, err := allocDelay(L)
			if err != nil {
				return p, err
			}

			if err := emitF(opDelay, 1, n.Flags, base, off, bus, 0xFFFF, bus+1); err != nil {
				return p, err
			}
			bus++
			i++
			continue
		}
		if nodes[i].Kind == planKindCross {

			n := nodes[i]
			loBase := coefIdx
			for _, v := range n.Coefs {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			emit(opBiquad, 1, loBase, sec, bus, 0xFFFF, crossLoBus)
			sec++

			hiBase := coefIdx
			for _, v := range n.Hi {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			emit(opBiquad, 1, hiBase, sec, bus, 0xFFFF, bus+1)
			sec++

			mixBase := coefIdx
			p.Coefs = append(p.Coefs, n.Mix[0], n.Mix[1], 0, 0, 0)
			coefIdx += 5

			if err := emit(opMix2, 1, mixBase, sec, bus+1, crossLoBus, bus+2); err != nil {
				return p, err
			}
			bus += 2
			i++
			continue
		}
		if nodes[i].Kind == planKindDyn {
			n := nodes[i]

			sideBase := coefIdx
			for _, v := range n.Side {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opBiquad, 1, sideBase, sec, bus, 0xFFFF, dynScratchBus); err != nil {
				return p, err
			}
			sec++

			dynBase := coefIdx
			for _, v := range n.Coefs {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			if err := emit(opDyn, 1, dynBase, sec, bus, dynScratchBus, bus+1); err != nil {
				return p, err
			}
			sec++
			bus++
			i++
			continue
		}

		n := 0
		base := coefIdx
		stb := sec
		for i+n < len(nodes) && nodes[i+n].Kind == planKindBiquad && n < secPerSlot {
			for _, v := range nodes[i+n].Coefs {
				p.Coefs = append(p.Coefs, v)
			}
			coefIdx += 5
			n++
		}
		if err := emit(opBiquad, n, base, stb, bus, 0xFFFF, bus+1); err != nil {
			return p, err
		}
		sec += n
		bus++
		i += n
	}

	if peakBus >= crossLoBus || bus >= crossLoBus {
		return p, fmt.Errorf("Out of buses: this chain needs bus %d, but %d/%d are engine-reserved"+
			"(cross-channel / DYN sidechain) - drop one effect that needs its own bus",
			peakBus, crossLoBus, dynScratchBus)
	}
	if coefIdx > hwCoefWords {
		return p, fmt.Errorf("Out of coefficient RAM: this chain needs %d words, each bank has only %d",
			coefIdx, hwCoefWords)
	}

	if p.StereoFrame && sec > hwMaxSections-jointStateReserve {
		return p, fmt.Errorf("Out of sections: this chain uses %d sections, but with the joint-stereo section each channel has only %d sections"+
			"(left (joint state always takes the last %d words)", sec, hwMaxSections-jointStateReserve,
			jointStateReserve)
	}
	p.Slots = slot
	p.Sections = sec
	if p.StereoFrame {
		p.Sections += jointStateReserve
	}
	return p, nil
}
