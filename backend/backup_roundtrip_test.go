// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withBackupDirs(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	oldT, oldP, oldI, oldD := userTypeDir, userPresetDir, irDir, ddcDir
	userTypeDir = filepath.Join(base, "types")
	userPresetDir = filepath.Join(base, "presets")
	irDir = filepath.Join(base, "irs")
	ddcDir = filepath.Join(base, "ddc")
	t.Cleanup(func() {
		userTypeDir, userPresetDir, irDir, ddcDir = oldT, oldP, oldI, oldD
	})
	withStateFile(t)
	resetRuntimeGlobals()
}

func TestZZBackupShape(t *testing.T) {
	withBackupDirs(t)

	if err := saveUserType(SoundType{Name: "ZZ-card", Effects: []TypeEffect{
		{ID: "u_1", Name: "ZZ-card", Kind: "combo", Enabled: true, Desc: `{"eq":{"on":true}}`},
	}}); err != nil {
		t.Fatalf("failed to save type: %v", err)
	}
	if err := saveUserPreset(Preset{Name: "ZZ-preset", Chain: []ChainItem{
		{Type: "peq", Enabled: true, Freq: 1000, GainDB: 1, Q: 1},
	}}); err != nil {
		t.Fatalf("failed to save preset: %v", err)
	}
	if err := saveIRFile("zz.irs", []byte("RIFFxxxx")); err != nil {
		t.Fatalf("failed to save IR: %v", err)
	}
	if err := saveDDCFile("zz.vdc", []byte("1 2 3\n")); err != nil {
		t.Fatalf("failed to save DDC: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/dsp/backup", nil)
	rec := httptest.NewRecorder()
	handleDSPBackup(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backup should be 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var bf backupFile
	if err := json.Unmarshal(rec.Body.Bytes(), &bf); err != nil {
		t.Fatalf("backup is not valid JSON: %v", err)
	}
	if bf.Format != backupFormat || bf.Version != backupVersion {
		t.Errorf("bad backup header: %q/%d", bf.Format, bf.Version)
	}
	if len(bf.Types) != 1 || bf.Types[0].Name != "ZZ-card" {
		t.Errorf("wrong types in backup: %+v", bf.Types)
	}
	if len(bf.Presets) != 1 || bf.Presets[0].Name != "ZZ-preset" {
		t.Errorf("wrong presets in backup")
	}
	if len(bf.IRs) != 1 || bf.IRs[0].Name != "zz.irs" {
		t.Errorf("wrong IRs in backup: %+v", bf.IRs)
	}
	if len(bf.DDCs) != 1 || bf.DDCs[0].Text != "1 2 3\n" {
		t.Errorf("wrong DDCs in backup: %+v", bf.DDCs)
	}

	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "zybo-backup-") {
		t.Errorf("bad download filename: %q", cd)
	}
}

func TestZZRestoreRejects(t *testing.T) {
	withBackupDirs(t)
	cases := map[string]string{
		"invalid JSON":          `{`,
		"bad format":            `{"format":"nope","version":1}`,
		"bad version":           `{"format":"zybo-backup","version":999}`,
		"builtin type mixed in": `{"format":"zybo-backup","version":1,"types":[{"id":"x","name":"bad","builtin":true}]}`,
		"IR data corrupt":       `{"format":"zybo-backup","version":1,"irs":[{"name":"a.irs","wav_base64":"!!!"}]}`,
	}
	for name, body := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/dsp/restore", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		handleDSPRestore(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s should be 400, got %d", name, rec.Code)
		}
	}

	ents, _ := os.ReadDir(userTypeDir)
	if len(ents) != 0 {
		t.Errorf("type dir should be empty after rejection, got %d files", len(ents))
	}
}

func TestZZRestoreFilesRoundtrip(t *testing.T) {
	withBackupDirs(t)

	if err := saveUserType(SoundType{Name: "ZZ-card", Effects: []TypeEffect{
		{ID: "u_1", Name: "ZZ-card", Kind: "combo", Enabled: true, Desc: `{}`},
	}}); err != nil {
		t.Fatalf("failed to save type: %v", err)
	}
	if err := saveIRFile("zz.irs", []byte("RIFFxxxx")); err != nil {
		t.Fatalf("failed to save IR: %v", err)
	}
	getReq := httptest.NewRequest(http.MethodGet, "/api/dsp/backup", nil)
	getRec := httptest.NewRecorder()
	handleDSPBackup(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("backup failed: %d", getRec.Code)
	}

	os.RemoveAll(userTypeDir)
	os.RemoveAll(irDir)

	postReq := httptest.NewRequest(http.MethodPost, "/api/dsp/restore", bytes.NewReader(getRec.Body.Bytes()))
	postRec := httptest.NewRecorder()
	handleDSPRestore(postRec, postReq)

	if postRec.Code != http.StatusBadRequest {
		t.Fatalf("offline restore should reach download failure 400, got %d: %s", postRec.Code, postRec.Body.String())
	}
	types, err := loadUserTypes()
	if err != nil || len(types) != 1 || types[0].Name != "ZZ-card" {
		t.Errorf("types not written back: %v %+v", err, types)
	}
	if b, err := os.ReadFile(filepath.Join(irDir, "zz.irs")); err != nil || string(b) != "RIFFxxxx" {
		t.Errorf("IR not written back: %v", err)
	}
}
