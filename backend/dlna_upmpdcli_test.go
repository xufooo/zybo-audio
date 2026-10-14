// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"strings"
	"testing"
)

func TestDlnaUsesUpmpdcliService(t *testing.T) {
	units, ok := sourceUnits["dlna"]

	if !ok || len(units) != 2 || units[0] != "mpd.service" || units[1] != "upmpdcli.service" {
		t.Fatalf(`source table dlna must be ["mpd.service" "upmpdcli.service"], got %v (present: %v)`, units, ok)
	}
	if got := unitsToStop("dlna"); !hasUnit(got, "mpd.socket") {
		t.Fatalf("stopping dlna must also stop mpd.socket (anti-wakeup on 6600), got %v", got)
	}

}

func hasUnit(units []string, want string) bool {
	for _, u := range units {
		if u == want {
			return true
		}
	}
	return false
}

func TestSelectDlnaGoesThroughUnits(t *testing.T) {
	sm := &sourceManager{}
	if _, err := sm.selectDLNA(); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("no upmpdcli unit on dev box, selectDLNA must report unavailable, got %v", err)
	}
}

func TestDlnaStillNeedsIface(t *testing.T) {

	if !needsIface("dlna") {
		t.Fatalf("dlna required an iface before retirement, gate must survive the switch")
	}
	if !needsIface("airplay") {
		t.Fatalf("airplay iface gate must stay")
	}
	if needsIface("local") {
		t.Fatalf("local plays offline, must have no iface gate")
	}
}
