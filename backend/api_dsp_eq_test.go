// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func countFiles(t *testing.T, dir string) int {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range ents {
		if !e.IsDir() {
			n++
		}
	}
	return n
}

func eqParse(t *testing.T, method, query, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, "/api/dsp/eq/parse"+query, strings.NewReader(body))
	handleDSPEQParse(rec, req)
	return rec
}

func decodeParse(t *testing.T, rec *httptest.ResponseRecorder) (int, []ChainItem) {
	t.Helper()
	var got struct {
		OK    bool        `json:"ok"`
		Count int         `json:"count"`
		Bands []ChainItem `json:"bands"`
		Warns []string    `json:"warnings"`
		Name  string      `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("did not return JSON: %v (%s)", err, rec.Body.String())
	}
	if got.Count != len(got.Bands) {
		t.Fatalf("count(%d) does not match bands(%d)", got.Count, len(got.Bands))
	}
	return got.Count, got.Bands
}

func TestEQParseREWTextGivesBands(t *testing.T) {
	withTempPresetDirs(t)
	body := "Filter 1: ON PK Fc 100 Hz Gain 3.0 dB Q 1.0\n" +
		"Filter 2: ON PK Fc 1000 Hz Gain -2.5 dB Q 2.0\n"
	rec := eqParse(t, http.MethodPost, "?name=probe", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("parse REW text should 200,actual %d:%s", rec.Code, rec.Body.String())
	}
	n, bands := decodeParse(t, rec)
	if n == 0 {
		t.Fatal("should parse sections,actual 0 sections")
	}
	found := false
	for _, b := range bands {
		if b.Freq > 90 && b.Freq < 110 {
			found = true
		}
	}
	if !found {
		t.Fatalf("100 Hz stage is missing: %+v", bands)
	}
}

func TestEQParsePresetJSONGivesItsChain(t *testing.T) {
	withTempPresetDirs(t)
	body := `{"name":"probe","chain":[{"type":"peq","enabled":true,"freq":250,"gain_db":1.5,"q":1.0}]}`
	rec := eqParse(t, http.MethodPost, "", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("parse preset JSON should 200,actual %d:%s", rec.Code, rec.Body.String())
	}
	n, bands := decodeParse(t, rec)
	if n != 1 || len(bands) != 1 || bands[0].Freq != 250 {
		t.Fatalf("should hand back its chain as-is, actual: %+v", bands)
	}
}

func TestEQParseWritesNothingAndAppliesNothing(t *testing.T) {
	_, dir := withTempPresetDirs(t)
	userDir := filepath.Join(dir, "user")
	before := countFiles(t, userDir)

	coefsBefore := append([]int32{}, lastPlanCoefs...)

	rec := eqParse(t, http.MethodPost, "?name=probe",
		"Filter 1: ON PK Fc 200 Hz Gain 4.0 dB Q 1.0\n")
	if rec.Code != http.StatusOK {
		t.Fatalf("should 200,actual %d:%s", rec.Code, rec.Body.String())
	}
	if after := countFiles(t, userDir); after != before {
		t.Fatalf("parse endpoint must not store: user preset dir %d -> %d files", before, after)
	}
	if len(lastPlanCoefs) != len(coefsBefore) {
		t.Fatalf("parse endpoint should not dispatch chain:coefficient plan %d -> %d items", len(coefsBefore), len(lastPlanCoefs))
	}
}

func TestEQParseRejectsBadInput(t *testing.T) {
	withTempPresetDirs(t)
	cases := []struct {
		name, method, body string
		want               int
	}{
		{"empty body", http.MethodPost, "   ", http.StatusBadRequest},
		{"garbage text", http.MethodPost, "hello world, this is not EQ", http.StatusBadRequest},
		{"partial JSON", http.MethodPost, `{"chain":[]}`, http.StatusBadRequest},
		{"wrong method", http.MethodGet, "", http.StatusMethodNotAllowed},
	}
	for _, c := range cases {
		rec := eqParse(t, c.method, "", c.body)
		if rec.Code != c.want {
			t.Errorf("%s:expected %d,actual %d(%s)", c.name, c.want, rec.Code, rec.Body.String())
		}
	}
}
