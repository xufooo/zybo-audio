// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

type sourceManager struct {
	mu     sync.Mutex
	active string
}

var sourceUnits = map[string][]string{
	"airplay": {"shairport-sync.service"},

	"dlna": {"mpd.service", "upmpdcli.service"},

	"local": {"mpd.service"},
}

func needsIface(name string) bool {
	return name == "airplay" || name == "dlna"
}

func (sm *sourceManager) init() error {
	sm.active = "idle"

	for name := range sourceUnits {
		if !sourceAvailable(name) {
			log.Printf("Source '%s': unavailable (unit missing)", name)
			continue
		}
		log.Printf("Source '%s': available", name)
	}

	if unitActive("shairport-sync.service") {
		sm.active = "airplay"
		log.Printf("Detected AirPlay already running (shairport-sync.service)")
	}
	if unitActive("upmpdcli.service") {
		sm.active = "dlna"
		log.Printf("Detected DLNA already running (upmpdcli.service)")
	}
	if unitActive("mpd.service") && sm.active == "idle" {

		sm.active = "local"
		log.Printf("Detected local playback already running (mpd.service)")
	}
	return nil
}

func sourceAvailable(name string) bool {
	if units, ok := sourceUnits[name]; ok {
		found := false
		for _, u := range units {
			if unitExists(u) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
		if needsIface(name) {
			return len(usableIfaces()) > 0
		}
		return true
	}
	return false
}

var sourceStopExtra = map[string][]string{
	"local": {"mpd.socket"},
	"dlna":  {"mpd.socket"},
}

func unitsToStop(name string) []string {
	out := append([]string(nil), sourceUnits[name]...)
	return append(out, sourceStopExtra[name]...)
}

func systemctl(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func unitExists(unit string) bool {
	out, err := systemctl("list-unit-files", unit, "--no-legend", "--no-pager")
	return err == nil && strings.TrimSpace(out) != ""
}

func unitActive(unit string) bool {
	out, err := systemctl("is-active", unit)
	return err == nil && strings.TrimSpace(out) == "active"
}

func (sm *sourceManager) startAirPlay() error { return sm.startUnitSource("airplay") }

func (sm *sourceManager) startLocal() error { return sm.startUnitSource("local") }

func (sm *sourceManager) startUnitSource(name string) error {
	units := sourceUnits[name]
	if len(units) == 0 {
		return fmt.Errorf("unknown source: %s", name)
	}
	if !sourceAvailable(name) {
		return fmt.Errorf("%s unavailable on this machine (systemd unit missing)", name)
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.stopAllLocked()

	for _, u := range units {
		if out, err := systemctl("start", u); err != nil {
			return fmt.Errorf("systemctl start %s: %v (%s)", u, err, out)
		}
	}

	if name == "local" || name == "dlna" {
		if err := quietMPD(); err != nil {
			log.Printf("quietMPD: %v (continuing, source switch must not fail)", err)
		}
	}
	sm.active = name
	log.Printf("Source started: %s (%s)", name, strings.Join(units, ", "))
	return nil
}

type ifaceInfo struct {
	name string
}

var ifaceLister = defaultIfaceLister

func defaultIfaceLister() []ifaceInfo {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []ifaceInfo
	for _, fi := range ifs {
		if fi.Flags&net.FlagUp == 0 || fi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := fi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out = append(out, ifaceInfo{name: fi.Name})
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

func usableIfaces() []string {
	infos := ifaceLister()
	names := make([]string, 0, len(infos))
	for _, in := range infos {
		names = append(names, in.name)
	}
	return names
}

func (sm *sourceManager) selectDLNA() (string, error) {
	if err := sm.startUnitSource("dlna"); err != nil {
		return "", err
	}
	return "", nil
}

func (sm *sourceManager) healDlnaBinding() {
}

func (sm *sourceManager) dlnaIfaceLabel() string {
	return ""
}

func (sm *sourceManager) stopAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.stopAllLocked()
}

func (sm *sourceManager) stopAllLocked() {

	if sm.active == "local" || sm.active == "dlna" {
		if err := quietMPD(); err != nil {
			log.Printf("quietMPD on stop: %v (continuing)", err)
		}
	}
	for name := range sourceUnits {
		if !sourceAvailable(name) {
			continue
		}
		for _, u := range unitsToStop(name) {
			if _, err := systemctl("stop", u); err != nil {
				log.Printf("systemctl stop %s: %v", u, err)
			}
		}
	}

	sm.active = "idle"
}

func sourceStates() map[string]bool {
	return map[string]bool{
		"airplay": sourceAvailable("airplay"),
		"dlna":    sourceAvailable("dlna"),
		"local":   sourceAvailable("local"),
	}
}
