// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type sourceManager struct {
	mu     sync.Mutex
	active string
	procs  map[string]*exec.Cmd
}

var sourceUnits = map[string][]string{
	"airplay":   {"shairport-sync.service"},
	"bluetooth": {"bluealsa.service", "bluealsa-aplay.service"},
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
		log.Printf("detected the system already running AirPlay (shairport-sync.service)")
	}
	return nil
}

func sourceAvailable(name string) bool {
	if units, ok := sourceUnits[name]; ok {
		for _, u := range units {
			if unitExists(u) {
				return true
			}
		}
		return false
	}
	if cmd, ok := sourceCommands[name]; ok {
		_, err := exec.LookPath(cmd)
		return err == nil
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

func (sm *sourceManager) startAirPlay() error   { return sm.startUnitSource("airplay") }
func (sm *sourceManager) startBluetooth() error { return sm.startUnitSource("bluetooth") }

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

func (sm *sourceManager) startDLNA() error {
	cmdName := sourceCommands["dlna"]
	if _, err := exec.LookPath(cmdName); err != nil {
		return fmt.Errorf("%s not in PATH (this image has no DLNA renderer installed)", cmdName)
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.stopAllLocked()

	cmd := exec.Command(cmdName,
		"-f", "ZYBO Audio",
		"-p", "49494",
		"-u", "00000000-0000-0000-0000-000000000001",
	)
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
	sm.active = "dlna"
	log.Printf("Source started: dlna (%s, pid %d)", cmdName, cmd.Process.Pid)
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
		"airplay":   sourceAvailable("airplay"),
		"dlna":      sourceAvailable("dlna"),
		"bluetooth": sourceAvailable("bluetooth"),
	}
}
