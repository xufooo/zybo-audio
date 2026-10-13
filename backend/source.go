// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
)

type sourceManager struct {
	mu     sync.Mutex
	active string
	procs  map[string]*exec.Cmd

	dlnaIface string
}

var sourceUnits = map[string][]string{
	"airplay": {"shairport-sync.service"},
}

var sourceCommands = map[string]string{
	"dlna": "gmediarender",
}

func (sm *sourceManager) init() error {
	sm.procs = make(map[string]*exec.Cmd)
	sm.active = "idle"

	for name := range sourceUnits {
		if !sourceAvailable(name) {
			log.Printf("Source '%s': unavailable (unit or program missing)", name)
			continue
		}
		log.Printf("Source '%s': available", name)
	}
	for name, cmd := range sourceCommands {
		if _, err := exec.LookPath(cmd); err != nil {
			log.Printf("Source '%s': %s not in PATH, unavailable", name, cmd)
		} else {
			log.Printf("Source '%s': %s available", name, cmd)
		}
	}

	if unitActive("shairport-sync.service") {
		sm.active = "airplay"
		log.Printf("Detected AirPlay already running (shairport-sync.service)")
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
		if name == "airplay" {
			return len(usableIfaces()) > 0
		}
		return true
	}
	if cmd, ok := sourceCommands[name]; ok {
		if _, err := exec.LookPath(cmd); err != nil {
			return false
		}

		if name == "dlna" {
			return len(usableIfaces()) > 0
		}
		return true
	}
	return false
}

func systemctl(args ...string) (string, error) {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
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

func defaultIface(names []string) string {
	for _, n := range names {
		if strings.HasPrefix(n, "wl") {
			return n
		}
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

func wifiIface() string {
	return defaultIface(usableIfaces())
}

func nextIface(names []string, cur string) string {
	if len(names) == 0 {
		return ""
	}
	at := -1
	for i, n := range names {
		if n == cur {
			at = i
			break
		}
	}
	return names[(at+1)%len(names)]
}

func (sm *sourceManager) selectDLNA() (string, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	names := usableIfaces()
	iface := ""
	if sm.active != "dlna" {
		iface = defaultIface(names)
	} else if len(names) > 1 {
		iface = nextIface(names, sm.dlnaIface)
	} else if len(names) == 1 {
		iface = names[0]
	}
	if sm.active == "dlna" && iface == sm.dlnaIface {
		return iface, nil
	}
	if err := sm.spawnDLNA_locked(iface); err != nil {
		return "", err
	}
	sm.dlnaIface = iface
	sm.active = "dlna"
	return iface, nil
}

func (sm *sourceManager) healDlnaBinding() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.active != "dlna" {
		return
	}
	names := usableIfaces()
	for _, n := range names {
		if n == sm.dlnaIface {
			return
		}
	}
	if len(names) == 0 {
		sm.stopAllLocked()
		sm.dlnaIface = ""
		log.Printf("DLNA: bound interface %q gone with no usable NIC, back to idle", sm.dlnaIface)
		return
	}
	next := nextIface(names, sm.dlnaIface)
	if err := sm.spawnDLNA_locked(next); err != nil {
		log.Printf("DLNA: move to %s failed: %v", next, err)
		return
	}
	log.Printf("DLNA: interface %q gone, moved to %s", sm.dlnaIface, next)
	sm.dlnaIface = next
}

func (sm *sourceManager) dlnaIfaceLabel() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.active != "dlna" {
		return ""
	}
	return sm.dlnaIface
}

func (sm *sourceManager) spawnDLNA_locked(iface string) error {
	cmdName := sourceCommands["dlna"]
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("%s not in PATH (this image has no DLNA renderer installed)", cmdName)
	}
	sm.stopAllLocked()

	args := []string{
		"-f", "ZYBO Audio",
		"-p", "49494",
		"-u", "00000000-0000-0000-0000-000000000001",
	}
	if iface != "" {
		args = append(args, "-I", iface)
	}
	cmd := exec.Command(cmdName, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s: %w", cmdName, err)
	}

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("%s exited: %v", cmdName, err)
		}
	}()
	sm.procs["dlna"] = cmd
	log.Printf("Source started: dlna (%s, pid %d, iface %s)", cmdName, cmd.Process.Pid, iface)
	return nil
}

func (sm *sourceManager) stopAll() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.stopAllLocked()
}

func (sm *sourceManager) stopAllLocked() {
	for name, units := range sourceUnits {
		if !sourceAvailable(name) {
			continue
		}
		for _, u := range units {
			if _, err := systemctl("stop", u); err != nil {
				log.Printf("systemctl stop %s: %v", u, err)
			}
		}
	}

	for name, cmd := range sm.procs {
		if cmd != nil && cmd.Process != nil {
			log.Printf("Stopping source: %s (pid %d)", name, cmd.Process.Pid)
			cmd.Process.Signal(os.Interrupt)
			cmd.Process.Kill()
		}
		delete(sm.procs, name)
	}
	sm.active = "idle"
}

func sourceStates() map[string]bool {
	return map[string]bool{
		"airplay": sourceAvailable("airplay"),
		"dlna":    sourceAvailable("dlna"),
	}
}
