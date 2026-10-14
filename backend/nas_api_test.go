// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNasListHandler(t *testing.T) {
	saved := cdsBase
	cdsBase = stubCDS(t)
	t.Cleanup(func() { cdsBase = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nas/list?id=26", nil)
	handleNasList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nas list must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got cdsResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("nas list did not return JSON: %v", err)
	}
	if len(got.Containers) != 1 || len(got.Tracks) != 1 {
		t.Fatalf("nas list entries wrong, got %+v", got)
	}
}

func TestNasPlayHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"clear": "OK\n",
		"add":   "OK\n",
		"play":  "OK\n",
	})
	saved := cdsBase
	cdsBase = "http://198.51.100.200:50001"
	t.Cleanup(func() { cdsBase = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/nas/play",
		strings.NewReader(`{"url":"http://198.51.100.200:50002/x.wav"}`))
	handleNasPlay(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nas play must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestNasPlayRejectsBadURL(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/nas/play",
		strings.NewReader(`{"url":"file:///etc/passwd"}`))
	handleNasPlay(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("non-http(s) must be refused, got 200")
	}
}
