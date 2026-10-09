// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"sync"
	"testing"
)

func TestCtrlRMWAtomic(t *testing.T) {
	fakeHardware(t, 0xFFFFFFFF)
	const bitA = uint32(1 << 0)
	const bitB = uint32(1 << 4)
	regWrite(regCtrl, 0)
	const rounds = 200
	var wg sync.WaitGroup
	for i := 0; i < rounds; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); dspSetCtrlBits(bitA, bitA) }()
		go func() { defer wg.Done(); dspSetCtrlBits(bitB, bitB) }()
		wg.Wait()
		if got := regRead(regCtrl); got&(bitA|bitB) != (bitA | bitB) {
			t.Fatalf("round %d: CTRL=%#x, both A/B bits must be set (lost update)", i, got)
		}
		regWrite(regCtrl, 0)
	}
}
