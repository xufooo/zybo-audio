// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestUpdateHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"update": "updating_db: 7\nOK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/update", nil)
	handleLocalUpdate(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got map[string]int
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got["updating"] != 7 {
		t.Fatalf("update must return {updating:7}, got %+v (err %v)", got, err)
	}
}

func TestUpdateFailsCleanly(t *testing.T) {
	saved := mpdAddr
	mpdAddr = "127.0.0.1:1"
	t.Cleanup(func() { mpdAddr = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/update", nil)
	handleLocalUpdate(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("update must not 200 when MPD is down")
	}
}
