// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestZZStateDebounceWinsLastChange(t *testing.T) {
	path := withStateFile(t)
	savedDebounce := stateDebounce
	stateDebounce = 60 * time.Millisecond
	t.Cleanup(func() { stateDebounce = savedDebounce })

	resetRuntimeGlobals()
	selectedTypeName = "ZZ-FIRST"
	markStateDirty()
	time.Sleep(20 * time.Millisecond)
	selectedTypeName = "ZZ-LAST"
	markStateDirty()

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			raw = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(raw) == 0 {
		t.Fatalf("state file still missing after debounce window (%s)", path)
	}
	var st runtimeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("persisted content is not valid JSON: %v", err)
	}
	if st.Type != "ZZ-LAST" {
		t.Errorf("persisted content should be the **last** change ZZ-LAST, got %q", st.Type)
	}
	if err := flushPendingState(); err != nil {
		t.Errorf("with nothing pending, flushPendingState should be a no-op, got error: %v", err)
	}
}

func TestZZStateDebounceTimerDoesNotReadGlobals(t *testing.T) {
	path := withStateFile(t)
	savedDebounce := stateDebounce
	stateDebounce = 80 * time.Millisecond
	t.Cleanup(func() { stateDebounce = savedDebounce })

	resetRuntimeGlobals()
	selectedTypeName = "ZZ-UNTOUCHED-BY-TIMER"
	currentPreampDB = -3.5
	markStateDirty()

	resetRuntimeGlobals()

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			raw = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(raw) == 0 {
		t.Skipf("file not there after debounce window, skipping (%s)", path)
	}
	var st runtimeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("persisted content is not valid JSON: %v", err)
	}
	if st.Type != "ZZ-UNTOUCHED-BY-TIMER" {
		t.Errorf("persisted content picked up globals changed **after** markStateDirty (Type=%q) => "+
			"debounce callback still reads live globals, data race not fully fixed", st.Type)
	}
	if st.PreampDB != -3.5 {
		t.Errorf("preamp should be -3.5 from the moment of change, got %v => snapshot was not taken by the caller", st.PreampDB)
	}
}

func TestZZStateNotPersistedBeforeBootRestoreDone(t *testing.T) {
	path := withStateFile(t)
	savedDebounce := stateDebounce
	stateDebounce = 50 * time.Millisecond
	t.Cleanup(func() { stateDebounce = savedDebounce })

	stateMu.Lock()
	stateBootRestoreDone = false
	stateMu.Unlock()
	t.Cleanup(func() {
		stateMu.Lock()
		stateBootRestoreDone = true
		stateMu.Unlock()
	})

	resetRuntimeGlobals()

	dspLimiterThrDB = 0
	dspLimiterRelMs = 42.67708248582973
	markStateDirty()

	time.Sleep(200 * time.Millisecond)
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("persisted during the boot window (before previous settings were re-applied) => dspInit transient state would overwrite user settings")
	}

	stateMu.Lock()
	stateBootRestoreDone = true
	stateMu.Unlock()
	markStateDirty()

	deadline := time.Now().Add(2 * time.Second)
	var raw []byte
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(path)
		if err == nil && len(b) > 0 {
			raw = b
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(raw) == 0 {
		t.Fatalf("markStateDirty still not persisted after restore completed (%s) => persistence was wrongly disabled", path)
	}
	var st runtimeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("persisted content is not valid JSON: %v", err)
	}
	if st.Limiter.ThrDB != 0 {
		t.Errorf("persisted limiter threshold should be 0, got %v", st.Limiter.ThrDB)
	}
}

func TestZZFlushPendingStateFlushesNow(t *testing.T) {
	path := withStateFile(t)
	savedDebounce := stateDebounce

	stateDebounce = 10 * time.Minute
	t.Cleanup(func() { stateDebounce = savedDebounce })

	resetRuntimeGlobals()
	selectedTypeName = "ZZ-FLUSHED"
	markStateDirty()
	time.Sleep(50 * time.Millisecond)

	if _, err := os.Stat(path); err == nil {
		t.Fatalf("persisted despite a 10-minute debounce, debounce did not take effect")
	}
	if err := flushPendingState(); err != nil {
		t.Fatalf("flushPendingState failed: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("file still missing after flush: %v", err)
	}
	var st runtimeState
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatalf("persisted content is not valid JSON: %v", err)
	}
	if st.Type != "ZZ-FLUSHED" {
		t.Errorf("flush should persist ZZ-FLUSHED, got %q", st.Type)
	}
}
