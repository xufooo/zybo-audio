// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"syscall"
	"unsafe"
)

const (
	dspBaseAddr = 0x43C00000
	dspMapSize  = 0x10000

	regCtrl   = 0x30
	regCidx   = 0x34
	regCdat   = 0x38
	regLimThr = 0x3C
	regLimAtt = 0x40
	regLimRel = 0x44

	legacyBands = 6

	maxBands    = maxSections
	coefPerBand = 5

	coefTotal = legacyBands * coefPerBand

	sampleRate = 48000.0
	coefShift  = 15
	qOne       = 1 << coefShift
	coefMax    = (1 << 17) - 1
	coefMin    = -(1 << 17)
	coefMask   = 0x3FFFF
	ctrlMask   = 0x1F

	minBandFreq = 20.0
	maxBandFreq = 20000.0
	minBandQ    = 0.1
	maxBandQ    = 10.0
	maxBandGain = 24.0

	ctrlHeadroom = 1 << 19

	preampFreeBoostDB = 3.0

	headroomDB = 18.0

	limDefaultThrQ = 29491
	limDefaultAttQ = 26214
	limDefaultRelQ = 32784
)

var (
	limDefaultThrDB = 20 * math.Log10(float64(limDefaultThrQ)/qOne)
	limDefaultAttMs = -1000.0 / (sampleRate * math.Log(float64(limDefaultAttQ)/qOne))
	limDefaultRelMs = 1000.0 / (sampleRate * math.Log(float64(limDefaultRelQ)/qOne))
)

var (
	dspMem       []byte
	dspFile      *os.File
	dspAvailable bool

	dspLimiterEnabled = true

	dspLimiterThrDB = 0.0
	dspLimiterAttMs = limDefaultAttMs
	dspLimiterRelMs = limDefaultRelMs
)

var errDSPUnavailable = errors.New("DSP unavailable(/dev/mem unmapped or IP missing)")

type slotConfig struct {
	Type   string
	Freq   float64
	Q      float64
	GainDB float64
}

type eqPreset struct {
	Name     string
	PreampDB float64
	Slots    [maxBands]slotConfig
}

var presets = map[string]eqPreset{
	"flat": {"Flat", 0, [maxBands]slotConfig{
		{"off", 0, 0, 0}, {"off", 0, 0, 0}, {"off", 0, 0, 0},
		{"off", 0, 0, 0}, {"off", 0, 0, 0}, {"off", 0, 0, 0},
	}},
	"rock": {"Rock", 0, [maxBands]slotConfig{
		{"LS", 60, 0.7, 4}, {"PK", 150, 0.7, 2}, {"PK", 400, 0.7, 1},
		{"PK", 1000, 0.7, -2}, {"PK", 3000, 0.7, 2}, {"HS", 10000, 0.7, 3},
	}},
	"jazz": {"Jazz", 0, [maxBands]slotConfig{
		{"LS", 60, 0.7, 3}, {"PK", 150, 0.7, 2}, {"PK", 400, 0.5, 1},
		{"PK", 1000, 0.5, 1.5}, {"PK", 3000, 0.7, 1}, {"HS", 10000, 0.7, 2},
	}},
	"classical": {"Classical", 0, [maxBands]slotConfig{
		{"LS", 60, 0.5, 2}, {"PK", 150, 0.5, 1}, {"PK", 400, 0.5, -1},
		{"PK", 1000, 0.7, 1}, {"PK", 3000, 0.7, 1}, {"HS", 10000, 0.7, 2},
	}},
	"vocal": {"Vocal", 0, [maxBands]slotConfig{
		{"PK", 60, 0.7, -3}, {"PK", 150, 0.7, -1}, {"PK", 400, 0.7, 2},
		{"PK", 1000, 1.0, 4}, {"PK", 3000, 0.7, 2}, {"PK", 10000, 0.7, -1},
	}},
	"bass": {"Bass Boost", 0, [maxBands]slotConfig{
		{"LS", 60, 0.5, 6}, {"PK", 150, 0.5, 4}, {"PK", 400, 0.5, 2},
		{"PK", 1000, 0.7, 1}, {"PK", 3000, 0.7, 0}, {"HS", 10000, 0.7, 1},
	}},
}

var (
	dspHeadroomOn bool

	chainGainDB float64

	currentUserChain []ChainItem
	currentSlots     [maxBands]slotConfig
	currentPreampDB  float64

	currentPreampAppliedDB float64
)

func bandLimit() int {
	if dspEngineGen == 1 {

		if hwMaxSections < maxBands {
			return hwMaxSections
		}
		return maxBands
	}
	return legacyBands
}

func dspHeadroomUsable() bool {
	if dspEngineGen != 1 || !dspLimiterEnabled {
		return false
	}
	return dspReadEngineCaps().HeadroomAvailable()
}

func coefWindow() int {
	if dspEngineGen == 1 {
		return hwCoefWords
	}
	return bandLimit() * coefPerBand
}

var lastPlanCoefs []int32

func coefWritten() int {
	if dspEngineGen == 1 {
		if len(lastPlanCoefs) == 0 {
			return dspActiveBands() * coefPerBand
		}
		return len(lastPlanCoefs)
	}
	return legacyBands * coefPerBand
}

func dspOpen() error {
	var err error
	dspFile, err = os.OpenFile("/dev/mem", os.O_RDWR|os.O_SYNC, 0)
	if err != nil {
		return fmt.Errorf("open /dev/mem: %w", err)
	}
	dspMem, err = syscall.Mmap(int(dspFile.Fd()), int64(dspBaseAddr), dspMapSize,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		dspFile.Close()
		return fmt.Errorf("mmap 0x%x: %w", dspBaseAddr, err)
	}
	dspAvailable = true
	return nil
}

func dspClose() {
	if dspMem != nil {
		syscall.Munmap(dspMem)
	}
	if dspFile != nil {
		dspFile.Close()
	}
}

func regWrite(off uint32, val uint32) {
	if !dspAvailable {
		return
	}
	*(*uint32)(unsafe.Pointer(&dspMem[off])) = val
}

func regRead(off uint32) uint32 {
	if !dspAvailable {
		return 0
	}
	return *(*uint32)(unsafe.Pointer(&dspMem[off]))
}

func limiterThrDBWritable(db float64) bool {
	return db <= 0 && db >= -60
}

func dspInit() {

	dspProbeEngine()

	dspAdoptHardwareCaps()

	initThrDB := -1.0
	if !limiterThrDBWritable(initThrDB) {
		initThrDB = dspLimiterThrDB
		log.Printf("limiter initial threshold %.1f dBFS not writable,falling back to %.1f dBFS", initThrDB, dspLimiterThrDB)
	}
	if err := dspSetLimiter(dspLimiterEnabled, initThrDB); err != nil {
		log.Printf("DSP limiter init failed: %v", err)
	}
	if err := dspSetLimiterTimes(dspLimiterAttMs, dspLimiterRelMs); err != nil {
		log.Printf("DSP limiter time constants init failed: %v", err)
	}
	if err := dspApplyPreset("flat"); err != nil {
		log.Printf("DSP initial preset failed: %v", err)
	}
	dspEnabled = true
	dspBypass = false
	if err := dspWriteCtrl(); err != nil {
		log.Printf("DSP CTRL init failed: %v", err)
	}
	log.Printf("DSP ready:up to %d sections dual-mono @%.0fHz,limiter %s(%s)@%.1fdBFS",
		bandLimit(), sampleRate, onOff(dspLimiterEnabled), limiterModeName(), initThrDB)
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func firstBiquadNode(nodes []planNode) int {
	for i := range nodes {
		if nodes[i].Kind == planKindBiquad {
			return i
		}
	}
	return -1
}

func dspActiveBands() int {
	n := 0
	for i := 0; i < maxBands; i++ {
		if !isBandOff(currentSlots[i].Type) {
			n = i + 1
		}
	}
	return n
}

func dspCtrlBandsField() int {
	n := dspActiveBands()
	if n > 7 {
		n = 7
	}
	return n
}

func dspWriteCtrl() error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	val := uint32(0)

	if dspBypass || !dspEnabled {
		val |= 1
	}
	val |= uint32(dspCtrlBandsField()&0x7) << 1
	if !dspLimiterEnabled {
		val |= 1 << 4
	}

	if dspLimiterTP {
		val |= ctrlLimTP
	}

	if dspHeadroomOn {
		val |= ctrlHeadroom
	}

	val |= regRead(regCtrl) & ctrlBankSel
	regWrite(regCtrl, val)

	mask := dspCtrlVerifyMask()
	if back := regRead(regCtrl) & mask; back != val&mask {
		return fmt.Errorf("CTRL readback mismatch:wrote %#x reads %#x(mask %#x)", val&mask, back, mask)
	}
	return nil
}

func dspCtrlVerifyMask() uint32 {
	if dspEngineGen == 1 {
		m := uint32(ctrlMask | ctrlBankSel | ctrlLimTP)
		if dspReadEngineCaps().HeadroomAvailable() {
			m |= ctrlHeadroom
		}
		return m
	}
	return ctrlMask
}

func dspSetEnable(en bool) error {
	dspEnabled = en
	return dspWriteCtrl()
}

func dspSetBypass(bp bool) error {
	dspBypass = bp
	return dspWriteCtrl()
}

func dspSetMasterVolume(vol float64) error {
	return nil
}

func dspSetPreamp(gain float64) error {
	if gain <= 0 {
		return nil
	}
	currentPreampDB = 20 * math.Log10(gain)
	return dspWriteAllBands()
}

func limiterThrRegDB() float64 {
	db := dspLimiterThrDB
	if dspHeadroomOn {
		db -= headroomDB
	}
	return db
}

func dspWriteLimiterThr() error {
	db := limiterThrRegDB()
	if db > 0 {
		db = 0
	}
	if db < -60 {
		db = -60
	}
	thrQ := uint32(math.Round(math.Pow(10, db/20) * qOne))
	if thrQ < 1 {
		thrQ = 1
	}
	if thrQ > qOne {
		thrQ = qOne
	}
	regWrite(regLimThr, thrQ)
	if back := regRead(regLimThr) & coefMask; back != thrQ {
		return fmt.Errorf("limiter THR readback mismatch:wrote %d reads %d", thrQ, back)
	}
	return nil
}

func dspSetLimiter(enabled bool, thrDB float64) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if thrDB > 0 {
		thrDB = 0
	}
	if thrDB < -60 {
		thrDB = -60
	}

	wasUsable := dspHeadroomUsable()
	dspLimiterEnabled = enabled
	dspLimiterThrDB = thrDB
	if err := dspWriteLimiterThr(); err != nil {
		return err
	}

	if wasUsable != dspHeadroomUsable() {
		if err := dspWriteAllBands(); err != nil {
			return err
		}
		markStateDirty()
		return nil
	}
	if err := dspWriteCtrl(); err != nil {
		return err
	}
	markStateDirty()
	return nil
}

func dspSetLimiterTimes(attMs, relMs float64) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if attMs < 0.05 {
		attMs = 0.05
	}
	if attMs > 50 {
		attMs = 50
	}
	if relMs < 1 {
		relMs = 1
	}
	if relMs > 2000 {
		relMs = 2000
	}

	if dspLimiterTP {
		attQ := limKFromMsTP(attMs)
		relQ := limKFromMsTP(relMs)
		regWrite(regLimAtt, attQ)
		regWrite(regLimRel, relQ)
		if back := regRead(regLimAtt) & coefMask; back != attQ {
			return fmt.Errorf("true peak limiter ATT readback mismatch:wrote %d reads %d", attQ, back)
		}
		if back := regRead(regLimRel) & coefMask; back != relQ {
			return fmt.Errorf("true peak limiter REL readback mismatch:wrote %d reads %d", relQ, back)
		}
		dspLimiterAttMs = attMs
		dspLimiterRelMs = relMs
		return nil
	}

	kAtt := math.Exp(-1.0 / (attMs / 1000.0 * sampleRate))
	kRel := math.Exp(1.0 / (relMs / 1000.0 * sampleRate))
	attQ := uint32(math.Round(kAtt * qOne))
	relQ := uint32(math.Round(kRel * qOne))
	if attQ > qOne-1 {
		attQ = qOne - 1
	}
	if attQ < 1 {
		attQ = 1
	}
	if relQ < qOne+1 {
		relQ = qOne + 1
	}
	regWrite(regLimAtt, attQ)
	regWrite(regLimRel, relQ)
	if back := regRead(regLimAtt) & coefMask; back != attQ {
		return fmt.Errorf("limiter ATT readback mismatch:wrote %d reads %d", attQ, back)
	}
	if back := regRead(regLimRel) & coefMask; back != relQ {
		return fmt.Errorf("limiter REL readback mismatch:wrote %d reads %d", relQ, back)
	}
	dspLimiterAttMs = attMs
	dspLimiterRelMs = relMs
	return nil
}

func dspWriteBand(band int, b0, b1, b2, a1, a2 int32) {
	regWrite(regCidx, uint32(band*coefPerBand))
	for _, v := range [coefPerBand]int32{b0, b1, b2, a1, a2} {
		regWrite(regCdat, uint32(v)&coefMask)
	}
	regRead(regCtrl)
}

func decodeCoefReadback(v uint32) int32 {
	return int32(v)
}

func dspReadCoeff(idx int) int32 {
	if idx < 0 || idx >= coefWindow() {
		return 0
	}
	regWrite(regCidx, uint32(idx))
	return decodeCoefReadback(regRead(regCdat))
}

func dspDumpCoeffs() []int32 {
	out := make([]int32, coefWritten())
	for i := range out {
		out[i] = dspReadCoeff(i)
	}
	return out
}

func isBandOff(t string) bool {
	return t == "" || strings.EqualFold(t, "off") || strings.EqualFold(t, "none")
}

func sanitizeBand(sc slotConfig) slotConfig {
	if sc.Type == "" {
		sc.Type = "PK"
	}
	sc.Type = strings.ToUpper(sc.Type)
	if sc.Freq < minBandFreq {
		sc.Freq = minBandFreq
	}
	if sc.Freq > maxBandFreq {
		sc.Freq = maxBandFreq
	}
	if sc.Freq > sampleRate*0.45 {
		sc.Freq = sampleRate * 0.45
	}
	if sc.Q < minBandQ {
		sc.Q = minBandQ
	}
	if sc.Q > maxBandQ {
		sc.Q = maxBandQ
	}
	if sc.GainDB > maxBandGain {
		sc.GainDB = maxBandGain
	}
	if sc.GainDB < -maxBandGain {
		sc.GainDB = -maxBandGain
	}
	return sc
}

func checkCoef(name string, v int32) (int32, error) {
	if v > coefMax || v < coefMin {
		return 0, fmt.Errorf("coefficient %s=%d exceeds Q3.15 range [%d,%d](points/gain too aggressive)",
			name, v, coefMin, coefMax)
	}
	return v, nil
}

func adaptNoGain(f func(freq, Q, fs float64) (int32, int32, int32, int32, int32)) func(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32) {
	return func(freq, Q, _, fs float64) (int32, int32, int32, int32, int32) {
		return f(freq, Q, fs)
	}
}

func designBiquad(sc slotConfig) (int32, int32, int32, int32, int32, error) {

	var design func(freq, Q, gainDB, fs float64) (int32, int32, int32, int32, int32)
	switch sc.Type {
	case "PK":
		design = rbjPeakingEQ
	case "LS":
		design = rbjLowShelf
	case "HS":
		design = rbjHighShelf
	case "LP", "LPQ":
		design = adaptNoGain(rbjLowPass)
	case "HP", "HPQ":
		design = adaptNoGain(rbjHighPass)
	case "NO":
		design = adaptNoGain(rbjNotch)
	case "BP":
		design = adaptNoGain(rbjBandPass)
	case "AP":
		design = adaptNoGain(rbjAllPass)
	default:
		return 0, 0, 0, 0, 0, fmt.Errorf("unknown filter type:%s", sc.Type)
	}

	b0, b1, b2, a1, a2, err := designStableBiquad(sc.Freq, sc.Q, sc.GainDB, design)
	if err != nil {
		return 0, 0, 0, 0, 0, err
	}

	for _, c := range []struct {
		name string
		v    int32
	}{{"b0", b0}, {"b1", b1}, {"b2", b2}, {"a1", a1}, {"a2", a2}} {
		if _, err := checkCoef(c.name, c.v); err != nil {
			return 0, 0, 0, 0, 0, err
		}
	}
	return b0, b1, b2, a1, a2, nil
}

func dspSetSlot(band int, sc slotConfig) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	if band < 0 || band >= bandLimit() {
		return fmt.Errorf("band %d out of range(0..%d)", band, bandLimit()-1)
	}

	if isBandOff(sc.Type) {
		currentSlots[band] = slotConfig{Type: "off"}
	} else {
		sc = sanitizeBand(sc)

		if _, _, _, _, _, err := designBiquad(sc); err != nil {
			return fmt.Errorf("band %d: %w", band, err)
		}
		currentSlots[band] = sc
	}

	return dspWriteAllBands()
}

func dspFinalCoeffs() ([maxBands][coefPerBand]int32, float64) {
	var out [maxBands][coefPerBand]int32
	active := 0
	for i := 0; i < maxBands; i++ {
		sc := currentSlots[i]
		out[i] = [coefPerBand]int32{qOne, 0, 0, 0, 0}
		if isBandOff(sc.Type) {
			continue
		}
		if b0, b1, b2, a1, a2, err := designBiquad(sanitizeBand(sc)); err == nil {
			out[i] = [coefPerBand]int32{b0, b1, b2, a1, a2}
			active++
		}
	}

	maxGain := cascadeMaxGainDB(out)

	if g := firPeakGainDBForHeadroom(); g > maxGain {
		maxGain = g
	}

	dspHeadroomOn = dspHeadroomUsable() && maxGain > preampDeadZoneDB
	pre := effectivePreampDB(maxGain, currentPreampDB)
	if dspHeadroomOn {

		pre = 0
		if currentPreampDB < pre {
			pre = currentPreampDB
		}

		if need := maxGain + preampSafetyMarginDB - headroomDB; need > 0 {
			if cut := -need; cut < pre {
				pre = cut
			}
		}
	}

	if chainGainDB != 0 {
		pre += chainGainDB
		if pre > 12 {
			pre = 12
		}
	}
	if pre > 0.005 || pre < -0.005 {

		first := -1
		for i := 0; i < maxBands; i++ {
			if !isBandOff(currentSlots[i].Type) {
				first = i
				break
			}
		}
		if first >= 0 {
			base := out
			apply := func(db float64) [maxBands][coefPerBand]int32 {
				c := base
				g := math.Pow(10, db/20)
				for k := 0; k < 3; k++ {
					v := int32(math.Round(float64(base[first][k]) * g))
					if v > coefMax {
						v = coefMax
					} else if v < coefMin {
						v = coefMin
					}
					c[first][k] = v
				}
				return c
			}
			out = apply(pre)

		}
	}
	return out, pre
}

func dspWriteAllBands() error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	coefs, pre := dspFinalCoeffs()
	currentPreampAppliedDB = pre

	if dspLimiterEnabled {
		if err := dspWriteLimiterThr(); err != nil {
			return err
		}
	}

	if dspEngineGen == 1 {

		nodes, err := buildChainNodes(coefs[:dspActiveBands()], currentDynBass, currentCrossfeed, currentSurround)
		if err != nil {
			return err
		}

		if pre != 0 && dspActiveBands() == 0 {
			if i := firstBiquadNode(nodes); i >= 0 {
				g := math.Pow(10, pre/20)
				for k := 0; k < 3; k++ {
					v := int32(math.Round(float64(nodes[i].Coefs[k]) * g))
					if v > coefMax {
						v = coefMax
					} else if v < coefMin {
						v = coefMin
					}
					nodes[i].Coefs[k] = v
				}
			} else {

				currentPreampAppliedDB = 0
			}
		}
		plan, err := buildSlotPlanNodes(nodes)
		if err != nil {
			return err
		}

		plan.FirCoefs = irPlanCoefs()
		if err := dspDownloadSlotPlan(plan); err != nil {
			return err
		}
		if len(plan.FirCoefs) > 0 {
			irClearDirty()
		}

		if err := dspAntiPopAfterDownload(plan); err != nil {

			log.Printf("ViPERBass antiPop ramp failed to start(chain already live,just missing that 1 s fade-in):%v", err)
		}
		return dspWriteCtrl()
	}

	for i := 0; i < legacyBands; i++ {
		c := coefs[i]
		dspWriteBand(i, c[0], c[1], c[2], c[3], c[4])
	}
	return dspWriteCtrl()
}

func getBandConfig(band int) slotConfig {
	if band < 0 || band >= maxBands {
		return slotConfig{Type: "off"}
	}
	return currentSlots[band]
}

func dspApplyPreset(name string) error {
	p, ok := presets[name]
	if !ok {
		return fmt.Errorf("unknown preset:%s", name)
	}
	if !dspAvailable {
		return errDSPUnavailable
	}
	for i, sc := range p.Slots {
		if isBandOff(sc.Type) {
			currentSlots[i] = slotConfig{Type: "off"}
			continue
		}
		s := sanitizeBand(sc)
		if _, _, _, _, _, err := designBiquad(s); err != nil {
			return fmt.Errorf("preset %s section %d:%w", name, i+1, err)
		}
		currentSlots[i] = s
	}

	currentPreampDB = p.PreampDB
	return dspWriteAllBands()
}

func dspExpectedCoeffs() []int32 {

	if dspEngineGen == 1 && len(lastPlanCoefs) > 0 {
		return append([]int32(nil), lastPlanCoefs...)
	}
	coefs, _ := dspFinalCoeffs()
	out := make([]int32, coefWritten())
	for i := 0; i < coefWritten()/coefPerBand; i++ {
		copy(out[i*coefPerBand:], coefs[i][:])
	}
	return out
}

func dspSelfCheck() error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	want := dspExpectedCoeffs()
	got := dspDumpCoeffs()
	for i := range want {
		if want[i] != got[i] {
			return fmt.Errorf("coefficient %d(band %d item %d)mismatch:expected %d readback %d",
				i, i/coefPerBand, i%coefPerBand, want[i], got[i])
		}
	}
	ctrl := regRead(regCtrl) & ctrlMask
	if nb := int((ctrl >> 1) & 0x7); nb != dspCtrlBandsField() {
		return fmt.Errorf("NR_BANDS mismatch:expected %d readback %d", dspCtrlBandsField(), nb)
	}
	return nil
}

func floatToQ315(val float64) int32 {
	return int32(math.Round(val * float64(qOne)))
}

func dspStatusSnapshot() DSPStatus {
	st := DSPStatus{
		Available:   dspAvailable,
		Enabled:     dspEnabled,
		Bypass:      dspBypass,
		Preset:      currentPreset,
		PreampDB:    currentPreampAppliedDB,
		BandsActive: dspActiveBands(),
		Limiter:     dspLimiterEnabled,
		LimiterMode: limiterModeName(),
		LimThrDB:    dspLimiterThrDB,
		LimAttMs:    dspLimiterAttMs,
		LimRelMs:    dspLimiterRelMs,
		Bands:       make([]BandStatus, maxBands),
	}
	for i := 0; i < maxBands; i++ {
		sc := currentSlots[i]
		st.Bands[i] = BandStatus{Type: sc.Type, Freq: sc.Freq, GainDB: sc.GainDB, Q: sc.Q}
	}
	return st
}
