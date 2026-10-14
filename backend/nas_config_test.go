// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNasRequiresConfig(t *testing.T) {
	saved := cdsBase
	cdsBase = ""
	t.Cleanup(func() { cdsBase = saved })
	for _, tc := range []struct {
		method, path, body string
		h                  func(http.ResponseWriter, *http.Request)
	}{
		{"GET", "/api/nas/list?id=0", "", handleNasList},
		{"POST", "/api/nas/play", `{"url":"http://198.51.100.200/x.wav"}`, handleNasPlay},
		{"POST", "/api/nas/enqueue", `{"id":"0"}`, handleNasEnqueue},
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		tc.h(rec, req)
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("%s %s must 502 when unconfigured, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestNasConfigSaveAndStatus(t *testing.T) {
	saved := cdsBase
	t.Cleanup(func() { cdsBase = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/nas/config",
		strings.NewReader(`{"base":"http://198.51.100.200:50001"}`))
	handleNasConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nas config save must 200, got %d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/nas/config", nil)
	handleNasConfig(rec2, req2)
	var st struct {
		Configured bool
		Base       string
	}
	if err := json.NewDecoder(rec2.Body).Decode(&st); err != nil || !st.Configured {
		t.Fatalf("must report configured:true after save, got %+v (err %v)", st, err)
	}
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/nas/config",
		strings.NewReader(`{"base":"ftp://x/"}`))
	handleNasConfig(rec3, req3)
	if rec3.Code == http.StatusOK {
		t.Fatalf("non-http(s) must be refused")
	}
}
