// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
)

const (
	limSampleRateHz = 96000.0

	ctrlLimTP = 1 << 18

	limMaxMsTP = 341.0
)

var dspLimiterTP = true

func limKFromMsTP(ms float64) uint32 {
	if ms < 0.05 {
		ms = 0.05
	}
	if ms > limMaxMsTP {
		ms = limMaxMsTP
	}
	k := 1.0 - math.Exp(-1.0/(ms/1000.0*limSampleRateHz))
	q := uint32(math.Round(k * qOne))
	if q < 1 {
		q = 1
	}
	if q > qOne-1 {
		q = qOne - 1
	}
	return q
}

func limMsFromKTP(q uint32) float64 {
	if q == 0 || q >= qOne {
		return 0
	}
	k := float64(q) / qOne
	return -1000.0 / (limSampleRateHz * math.Log(1.0-k))
}

func limiterModeName() string {
	if dspLimiterTP {
		return "truepeak"
	}
	return "feedback"
}

func dspSetLimiterTP(tp bool) error {
	if !dspAvailable {
		return errDSPUnavailable
	}
	dspLimiterTP = tp
	if err := dspSetLimiterTimes(dspLimiterAttMs, dspLimiterRelMs); err != nil {
		return err
	}
	return dspWriteCtrl()
}
