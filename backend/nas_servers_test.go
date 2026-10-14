// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNasRequiresSelection(t *testing.T) {
	savedServers, savedCurrent := cdsServers, cdsCurrent
	cdsServers, cdsCurrent = nil, -1
	t.Cleanup(func() { cdsServers, cdsCurrent = savedServers, savedCurrent })
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
			t.Fatalf("%s %s must 502 when nothing selected, got %d", tc.method, tc.path, rec.Code)
		}
	}
}

func TestNasServersAndUse(t *testing.T) {
	httpStub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".xml") {
			w.Write([]byte(fakeDesc))
			return
		}
		w.Write([]byte(cdsBrowseResp))
	}))
	defer httpStub.Close()
	savedTarget, savedTimeout := ssdpTarget, ssdpTimeout
	ssdpTarget = stubSSDP(t, httpStub.URL+"/desc.xml")
	ssdpTimeout = 3 * time.Second
	t.Cleanup(func() { ssdpTarget, ssdpTimeout = savedTarget, savedTimeout })
	savedServers, savedCurrent := cdsServers, cdsCurrent
	t.Cleanup(func() { cdsServers, cdsCurrent = savedServers, savedCurrent })

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/nas/servers", nil)
	handleNasServers(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("servers must 200, got %d", rec.Code)
	}
	var servers []cdsServer
	if err := json.NewDecoder(rec.Body).Decode(&servers); err != nil || len(servers) != 1 {
		t.Fatalf("must discover 1 server, got %+v (err %v)", servers, err)
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/nas/use",
		strings.NewReader(`{"location":"`+servers[0].Location+`"}`))
	handleNasUse(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("use must 200, got %d (%s)", rec2.Code, rec2.Body.String())
	}
	if !nasConfigured() {
		t.Fatalf("nasConfigured must be true after use")
	}
	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodPost, "/api/nas/use", strings.NewReader(`{"location":""}`))
	handleNasUse(rec3, req3)
	if rec3.Code == http.StatusOK {
		t.Fatalf("empty location must be refused")
	}
}

func TestRememberedServerSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	savedPath := nasRememberPath
	nasRememberPath = dir + "/nas.json"
	t.Cleanup(func() { nasRememberPath = savedPath })
	savedServers, savedCurrent := cdsServers, cdsCurrent
	t.Cleanup(func() { cdsServers, cdsCurrent = savedServers, savedCurrent })
	cdsServers = []cdsServer{{Name: "FakeNAS", Location: "http://x/desc.xml", Control: "http://x/ctl"}}
	cdsCurrent = 0
	rememberServer()
	cdsServers, cdsCurrent = nil, -1
	recallServer()
	if !nasConfigured() || cdsServers[cdsCurrent].Name != "FakeNAS" {
		t.Fatalf("remembered server must survive restart, got %+v/%d", cdsServers, cdsCurrent)
	}
}
