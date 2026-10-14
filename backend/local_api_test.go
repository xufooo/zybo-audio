// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withStubMPD(t *testing.T, script map[string]string) {
	t.Helper()
	saved := mpdAddr
	mpdAddr = stubMPD(t, script)
	t.Cleanup(func() { mpdAddr = saved })
}

func TestLocalListHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"listfiles": "file: a.flac\nsize: 100\nLast-Modified: 2026-09-14T09:59:21Z\n" +
			"directory: sub\nLast-Modified: 2026-09-10T03:01:52Z\nOK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/local/list?dir=", nil)
	handleLocalList(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got []mpdEntry
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("list did not return JSON: %v", err)
	}
	if len(got) != 2 || got[0].Name != "a.flac" || !got[1].IsDir {
		t.Fatalf("list entries wrong, got %+v", got)
	}
}

func TestLocalPlayHandler(t *testing.T) {
	withStubMPD(t, map[string]string{
		"clear": "OK\n",
		"add":   "OK\n",
		"play":  "OK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/play", strings.NewReader(`{"file":"a.flac"}`))
	handleLocalPlay(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("play must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestLocalMetadataFromCurrentSong(t *testing.T) {
	withStubMPD(t, map[string]string{
		"currentsong": "file: newdivide-48k.flac\nTitle: New Divide\nArtist: Linkin Park\nAlbum: New Divide\nOK\n",
	})
	savedT, savedA, savedB := currentTitle, currentArtist, currentAlbum
	t.Cleanup(func() { currentTitle, currentArtist, currentAlbum = savedT, savedA, savedB })
	savedSrc := currentSource
	currentSource = "local"
	t.Cleanup(func() { currentSource = savedSrc })
	collectMetadata()
	if currentTitle != "New Divide" || currentArtist != "Linkin Park" {
		t.Fatalf("local metadata wrong, got %q/%q", currentTitle, currentArtist)
	}
}

func TestLocalMetadataFallsBackToFilename(t *testing.T) {
	withStubMPD(t, map[string]string{
		"currentsong": "file: test.wav\nOK\n",
	})
	savedT, savedA, savedB := currentTitle, currentArtist, currentAlbum
	t.Cleanup(func() { currentTitle, currentArtist, currentAlbum = savedT, savedA, savedB })
	savedSrc := currentSource
	currentSource = "local"
	t.Cleanup(func() { currentSource = savedSrc })
	collectMetadata()
	if currentTitle != "test.wav" {
		t.Fatalf("must fall back to filename without title, got %q", currentTitle)
	}
}
