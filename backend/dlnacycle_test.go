// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os/exec"
	"testing"
)

func TestDlnaIfaceOrder(t *testing.T) {
	names := []string{"eth0", "wlx085700a58fd0", "lo"}
	if got := defaultIface(names); got != "wlx085700a58fd0" {
		t.Errorf("default must prefer wireless, got %q", got)
	}
	if got := defaultIface([]string{"eth0"}); got != "eth0" {
		t.Errorf("no wireless: default must be first usable, got %q", got)
	}
	if got := defaultIface(nil); got != "" {
		t.Errorf("no interfaces: default must be empty, got %q", got)
	}
}

func TestDlnaNextIface(t *testing.T) {
	names := []string{"eth0", "wlx085700a58fd0"}
	if got := nextIface(names, ""); got != "eth0" {
		t.Errorf("unknown current must start from head, got %q", got)
	}
	if got := nextIface(names, "eth0"); got != "wlx085700a58fd0" {
		t.Errorf("must advance eth0 -> wlan, got %q", got)
	}
	if got := nextIface(names, "wlx085700a58fd0"); got != "eth0" {
		t.Errorf("must wrap wlan -> eth0, got %q", got)
	}
	if got := nextIface(nil, "eth0"); got != "" {
		t.Errorf("empty list must stay empty, got %q", got)
	}
}

func TestDlnaIfaceLabel(t *testing.T) {

	for _, name := range []string{"", "wlx085700a58fd0", "wlan0", "eth0", "enp3s0", "usb0"} {
		sm := &sourceManager{procs: map[string]*exec.Cmd{}}
		if name != "" {
			sm.active = "dlna"
			sm.dlnaIface = name
		}
		if got := sm.dlnaIfaceLabel(); got != name {
			t.Errorf("dlnaIfaceLabel() = %q, want %q", got, name)
		}
	}
}

func TestDlnaSelectNeedsRenderer(t *testing.T) {
	savedLister := ifaceLister
	defer func() { ifaceLister = savedLister }()
	ifaceLister = func() []ifaceInfo {
		return []ifaceInfo{{name: "eth0"}, {name: "wlx085700a58fd0"}}
	}
	sm := &sourceManager{procs: map[string]*exec.Cmd{}}
	if _, err := sm.selectDLNA(); err == nil {
		t.Error("without gmediarender in PATH, selectDLNA must fail (this box has none)")
	}
}

func TestDlnaNeedsNetwork(t *testing.T) {
	savedLister := ifaceLister
	defer func() { ifaceLister = savedLister }()

	ifaceLister = func() []ifaceInfo { return nil }
	if sourceAvailable("dlna") {
		t.Error("dlna must be unavailable with zero usable interfaces")
	}
}
func TestAirplayNeedsNetwork(t *testing.T) {
	savedLister := ifaceLister
	defer func() { ifaceLister = savedLister }()

	ifaceLister = func() []ifaceInfo { return nil }
	if sourceAvailable("airplay") {
		t.Error("airplay must be unavailable with zero usable interfaces")
	}
}

func TestHealDlnaBindingToIdle(t *testing.T) {
	savedLister := ifaceLister
	defer func() { ifaceLister = savedLister }()

	ifaceLister = func() []ifaceInfo { return nil }
	sm := &sourceManager{procs: map[string]*exec.Cmd{}, active: "dlna", dlnaIface: "wlx085700a58fd0"}
	sm.healDlnaBinding()
	if sm.active != "idle" {
		t.Errorf("dead binding with no interfaces must fall back to idle, got %q", sm.active)
	}
	if sm.dlnaIface != "" {
		t.Errorf("dead binding must clear dlnaIface, got %q", sm.dlnaIface)
	}
}

func TestDlnaSingleIfaceNoRespawn(t *testing.T) {
	savedLister := ifaceLister
	defer func() { ifaceLister = savedLister }()
	ifaceLister = func() []ifaceInfo {
		return []ifaceInfo{{name: "eth0"}}
	}
	sm := &sourceManager{procs: map[string]*exec.Cmd{}, active: "dlna", dlnaIface: "eth0"}

	iface, err := sm.selectDLNA()
	if err != nil {
		t.Fatalf("single-iface no-op must not fail: %v", err)
	}
	if iface != "eth0" {
		t.Errorf("single-iface no-op must keep eth0, got %q", iface)
	}
}
