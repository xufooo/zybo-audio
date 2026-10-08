// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"strings"
	"testing"
)

func TestVersionFileMatchesAppVersion(t *testing.T) {
	b, err := os.ReadFile("../VERSION")
	if err != nil {
		t.Fatalf("read not to repo store root VERSION: %v (test must from backend/ run)", err)
	}
	want := strings.TrimSpace(string(b))
	if want == "" {
		t.Fatal("VERSION is empty")
	}
	if appVersion != want {
		t.Errorf("version drift: VERSION=%q but main.go appVersion=%q - "+
			"release script uses VERSION pack, panel uses appVersion display, both must match",
			want, appVersion)
	}
}
