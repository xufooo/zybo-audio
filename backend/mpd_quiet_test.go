// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"testing"
)

func TestQuietMPDStopsPlayback(t *testing.T) {
	withStubMPD(t, map[string]string{"stop": "OK\n"})
	if err := quietMPD(); err != nil {
		t.Fatalf("quietMPD must succeed when stop is OK, err: %v", err)
	}
}

func TestQuietMPDFailsOnDeadPort(t *testing.T) {
	saved := mpdAddr
	mpdAddr = "127.0.0.1:1"
	t.Cleanup(func() { mpdAddr = saved })
	if err := quietMPD(); err == nil {
		t.Fatalf("quietMPD must error on dead port (callers only log)")
	}
}

func TestStopAllQuietsMPDWithoutExploding(t *testing.T) {

	saved := mpdAddr
	mpdAddr = "127.0.0.1:1"
	t.Cleanup(func() { mpdAddr = saved })
	sm := &sourceManager{active: "local"}
	sm.stopAll()
	if sm.active != "idle" {
		t.Fatalf("must be idle after stopAll, got %q", sm.active)
	}
}
