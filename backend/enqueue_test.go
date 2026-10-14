// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnqueuePlaysWhenIdle(t *testing.T) {
	withStubMPD(t, map[string]string{
		"status": "state: stop\nplaylistlength: 0\nOK\n",
		"clear":  "OK\n",
		"add":    "OK\n",
		"play":   "OK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/enqueue", strings.NewReader(`{"file":"a.flac"}`))
	handleLocalEnqueue(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("idle enqueue must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestEnqueueAppendsWhenBusy(t *testing.T) {
	withStubMPD(t, map[string]string{
		"status": "state: play\nplaylistlength: 1\nOK\n",
		"add":    "OK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/enqueue", strings.NewReader(`{"url":"http://x/y.wav"}`))
	handleLocalEnqueue(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("busy enqueue must 200 (add only), got %d (%s)", rec.Code, rec.Body.String())
	}
}
