// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const queueScript = "file: a.flac\nTitle: Song A\nPos: 0\nId: 1\n"

func TestMPDQueueParses(t *testing.T) {
	withStubMPD(t, map[string]string{
		"playlistinfo": queueScript + "file: b.flac\nPos: 1\nId: 2\nOK\n",
		"status":       "state: play\nsong: 1\nsongid: 2\nOK\n",
	})
	q, err := mpdQueue()
	if err != nil {
		t.Fatalf("queue must succeed, err: %v", err)
	}
	if len(q.Items) != 2 || q.Items[0].Title != "Song A" || q.Items[1].File != "b.flac" {
		t.Fatalf("queue parse wrong, got %+v", q.Items)
	}
	if q.Current != 1 || q.State != "play" {
		t.Fatalf("wrong position/state, got %+v", q)
	}
}

func TestMPDTransportVerbs(t *testing.T) {
	withStubMPD(t, map[string]string{
		"pause":    "OK\n",
		"next":     "OK\n",
		"previous": "OK\n",
		"playid":   "OK\n",
	})
	if err := mpdPause(); err != nil {
		t.Fatalf("pause must succeed: %v", err)
	}
	if err := mpdNext(); err != nil {
		t.Fatalf("next must succeed: %v", err)
	}
	if err := mpdPrev(); err != nil {
		t.Fatalf("previous must succeed: %v", err)
	}
	if err := mpdPlayID(2); err != nil {
		t.Fatalf("playid must succeed: %v", err)
	}
}

func TestQueueHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"playlistinfo": queueScript + "OK\n",
		"status":       "state: stop\nOK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/local/queue", nil)
	handleLocalQueue(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("queue must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got mpdQueueInfo
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("queue did not return JSON: %v", err)
	}
	if len(got.Items) != 1 || got.State != "stop" {
		t.Fatalf("queue content wrong, got %+v", got)
	}
}

func TestCtlHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"pause":    "OK\n",
		"stop":     "OK\n",
		"next":     "OK\n",
		"previous": "OK\n",
		"playid":   "OK\n",
	})
	for _, body := range []string{`{"action":"pause"}`, `{"action":"stop"}`,
		`{"action":"next"}`, `{"action":"prev"}`, `{"action":"play","ID":2}`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/local/ctl", strings.NewReader(body))
		handleLocalCtl(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("ctl %s must 200, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/ctl", strings.NewReader(`{"action":"rewind"}`))
	handleLocalCtl(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bogus action must 400, got %d", rec.Code)
	}
}
