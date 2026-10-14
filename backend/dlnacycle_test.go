// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"testing"
)

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
