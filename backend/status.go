// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"time"
)

var startTime = time.Now()

var pcmStatusPath = "/proc/asound/card0/pcm0p/sub0/status"

func systemIsPlaying() bool {
	out, err := ioutilReadFile(pcmStatusPath)
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "state: RUNNING")
}

func getSystemStatus() SystemStatus {
	return SystemStatus{
		CPUPercent: getCPUPercent(),
		MemUsedMB:  getMemUsedMB(),
		MemTotalMB: 512,
		UptimeSec:  int64(time.Since(startTime).Seconds()),
	}
}

func getCPUPercent() int {

	data, err := ioutilReadFile("/proc/stat")
	if err != nil {
		return 0
	}
	line := strings.Split(string(data), "\n")[0]
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return 0
	}

	var user, nice, system, idle uint64
	user, _ = strconv.ParseUint(fields[1], 10, 64)
	nice, _ = strconv.ParseUint(fields[2], 10, 64)
	system, _ = strconv.ParseUint(fields[3], 10, 64)
	idle, _ = strconv.ParseUint(fields[4], 10, 64)

	total := user + nice + system + idle
	if total == 0 {
		return 0
	}
	used := total - idle
	return int(used * 100 / total)
}

func getMemUsedMB() int {
	data, err := ioutilReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {

		}
		if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseInt(fields[1], 10, 64)
				return 512 - int(kb/1024)
			}
		}
	}
	return 80
}

func buildStatus() APIStatus {
	return APIStatus{
		Version: appVersion,
		Source:  currentSource,
		Title:   currentTitle,
		Artist:  currentArtist,
		Album:   currentAlbum,

		Playing: systemIsPlaying(),

		Volume:   readSystemVolume(currentVolume),
		VolumeDB: systemVolumeDB(currentVolume),
		DSP:      dspStatusSnapshot(),
		System:   getSystemStatus(),
		Sources:  sourceStates(),
	}
}

func statusPusher(hub *wsHub, sm *sourceManager) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var lastChecksum string

	for range ticker.C {

		collectMetadata()

		status := buildStatus()

		jsonBytes, _ := json.Marshal(status)
		checksum := string(jsonBytes)
		if checksum != lastChecksum {
			hub.broadcast(jsonBytes)
			lastChecksum = checksum
		}
	}
}

var ioutilReadFile = os.ReadFile
var osReadFile = os.ReadFile
