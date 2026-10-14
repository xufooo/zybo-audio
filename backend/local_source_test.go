// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postSourceSelect(t *testing.T, body string) int {
	t.Helper()
	if srcMgr == nil {
		srcMgr = &sourceManager{}
		t.Cleanup(func() { srcMgr = nil })
	}
	savedSource := currentSource
	savedActive := srcMgr.active
	t.Cleanup(func() {
		currentSource = savedSource
		srcMgr.active = savedActive
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/source/select", strings.NewReader(body))
	handleSourceSelect(rec, req)
	return rec.Code
}

func TestLocalSourceSelectable(t *testing.T) {
	code := postSourceSelect(t, `{"source":"local"}`)
	if code == http.StatusBadRequest {
		t.Fatalf("local source must be wired (not 400), got 400 unknown source")
	}
}

func TestSourceStatesHasLocal(t *testing.T) {
	st := sourceStates()
	if _, ok := st["local"]; !ok {
		t.Fatalf(`sourceStates() lacks "local" key, got %v`, st)
	}
}
