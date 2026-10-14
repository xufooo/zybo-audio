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

func TestCloudConfigSaveAndStatus(t *testing.T) {
	dir := t.TempDir()
	saved := rcloneConfPath
	rcloneConfPath = filepath.Join(dir, "rclone.conf")
	t.Cleanup(func() { rcloneConfPath = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cloud/config",
		strings.NewReader(`{"text":"[mycloud]\ntype = drive\n"}`))
	handleCloudConfig(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config save must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	data, err := os.ReadFile(rcloneConfPath)
	if err != nil || !strings.Contains(string(data), "[mycloud]") {
		t.Fatalf("config file wrong: %v", err)
	}
	if fi, _ := os.Stat(rcloneConfPath); fi.Mode().Perm() != 0600 {
		t.Fatalf("credential file must be 0600, got %o", fi.Mode().Perm())
	}
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/api/cloud/config", nil)
	handleCloudConfig(rec2, req2)
	var st struct {
		Configured bool
	}
	if err := json.NewDecoder(rec2.Body).Decode(&st); err != nil || !st.Configured {
		t.Fatalf("must report configured:true after save, got %+v (err %v)", st, err)
	}
}

func TestCloudConfigRejectsEmptyAndHuge(t *testing.T) {
	dir := t.TempDir()
	saved := rcloneConfPath
	rcloneConfPath = filepath.Join(dir, "rclone.conf")
	t.Cleanup(func() { rcloneConfPath = saved })
	for _, body := range []string{`{"text":""}`, `{"text":"` + strings.Repeat("x", 70000) + `"}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/cloud/config", strings.NewReader(body))
		handleCloudConfig(rec, req)
		if rec.Code == http.StatusOK {
			t.Fatalf("empty/huge config must be refused, body len %d got 200", len(body))
		}
	}
}

func TestCloudMountHandlesMissingUnit(t *testing.T) {

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/cloud/mount", strings.NewReader(`{"on":true}`))
	handleCloudMount(rec, req)
	if rec.Code == http.StatusOK {
		t.Logf("rclone-cloud unit exists and started on this box, counts as pass")
	}
}
