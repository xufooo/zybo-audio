// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"sync"
	"time"
)

const (
	mixerControl = "Master"

	volumeCacheTTL = 2 * time.Second

	volumeMinDB = -60.0
	volumeMaxDB = 0.0

	mixerZeroDBRaw = 122
)

var (
	volumeMu     sync.Mutex
	volumePct    = -1
	volumeDB     float64
	volumeReadAt time.Time
)

var volumeDBRe = regexp.MustCompile(`\[(-?[0-9.]+)dB\]`)

func pctToDB(pct int) float64 {
	if pct <= 0 {
		return volumeMinDB
	}
	if pct >= 100 {
		return volumeMaxDB
	}
	return volumeMinDB + (volumeMaxDB-volumeMinDB)*float64(pct)/100.0
}

func dbToPct(db float64) int {
	if db <= volumeMinDB {
		return 0
	}
	if db >= volumeMaxDB {
		return 100
	}
	return int((db-volumeMinDB)/(volumeMaxDB-volumeMinDB)*100.0 + 0.5)
}

func mixerRawForPct(pct int) int {
	if pct <= 0 {
		return 0
	}
	raw := mixerZeroDBRaw + int(math.Round(pctToDB(pct)))
	if raw > 127 {
		raw = 127
	}
	if raw < 1 {
		raw = 1
	}
	return raw
}

func quantizePct(pct int) int {
	return dbToPct(float64(mixerRawForPct(pct) - mixerZeroDBRaw))
}

func parseMixerDB(out string) (float64, bool) {
	m := volumeDBRe.FindStringSubmatch(out)
	if m == nil {
		return 0, false
	}
	db, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, false
	}
	if db < volumeMinDB-5 {
		return volumeMinDB, true
	}
	return db, true
}

func readSystemVolume(fallback int) int {
	volumeMu.Lock()
	defer volumeMu.Unlock()
	if volumePct >= 0 && time.Since(volumeReadAt) < volumeCacheTTL {
		return volumePct
	}
	out, err := exec.Command("amixer", "-c", "0", "sget", mixerControl).Output()
	if err != nil {
		return fallback
	}
	db, ok := parseMixerDB(string(out))
	if !ok {
		return fallback
	}
	pct := dbToPct(db)
	volumePct, volumeDB, volumeReadAt = pct, db, time.Now()
	return pct
}

func systemVolumeDB(fallback int) float64 {
	readSystemVolume(fallback)
	volumeMu.Lock()
	defer volumeMu.Unlock()
	return volumeDB
}

func setSystemVolume(pct int) error {
	if pct <= 0 {
		return exec.Command("amixer", "-c", "0", "sset", mixerControl, "0").Run()
	}
	return exec.Command("amixer", "-c", "0", "sset", mixerControl,
		strconv.Itoa(mixerRawForPct(pct))).Run()
}

func noteSystemVolume(pct int) {
	volumeMu.Lock()
	if pct <= 0 {
		volumePct, volumeDB = 0, volumeMinDB
	} else {
		volumePct = quantizePct(pct)
		volumeDB = float64(mixerRawForPct(pct) - mixerZeroDBRaw)
	}
	volumeReadAt = time.Now()
	volumeMu.Unlock()
}
