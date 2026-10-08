// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestZZBypassInSnapshot(t *testing.T) {
	path := withStateFile(t)
	savedBypass := dspBypass
	t.Cleanup(func() { dspBypass = savedBypass })

	resetRuntimeGlobals()
	dspBypass = true
	markStateDirty()
	if err := flushPendingState(); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	raw := readBypassStateFile(t, path)
	var st runtimeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("written content is not valid JSON: %v", err)
	}
	if !st.Bypass {
		t.Errorf("dspBypass=true when fast in Bypass should be true")
	}

	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if _, ok := m["bypass"]; !ok {
		t.Errorf("write to disk JSON in missing bypass key (omitempty swallowed？)")
	}

	resetRuntimeGlobals()
	dspBypass = false
	markStateDirty()
	if err := flushPendingState(); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	raw = readBypassStateFile(t, path)
	var st2 runtimeState
	if err := json.Unmarshal(raw, &st2); err != nil {
		t.Fatalf("written content is not valid JSON: %v", err)
	}
	if st2.Bypass {
		t.Errorf("dspBypass=false when fast in Bypass should be false")
	}
}

func readBypassStateFile(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) == 0 {
		t.Fatalf("state file read not to (%s): %v", path, err)
	}
	return raw
}

func TestZZBypassMissingMeansOff(t *testing.T) {

	var st runtimeState
	if err := json.Unmarshal([]byte(`{"format":1,"chain":[]}`), &st); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if st.Bypass {
		t.Errorf("when key missing Bypass should be false (off)")
	}
}
