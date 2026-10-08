// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSystemIsPlaying(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{
			name: "playing (real content measured on board)",
			body: "state: RUNNING\n" +
				"owner_pid   : 4537\n" +
				"trigger_time: 1749.668276857\n" +
				"tstamp      : 1845.837296166\n" +
				"delay       : 19025\n" +
				"avail       : 46511\n",
			want: true,
		},
		{name: "driver writes closed when device is not open", body: "closed\n", want: false},
		{name: "SETUP / PREPARED not sounding yet", body: "state: PREPARED\nowner_pid   : 100\n", want: false},
		{name: "no such file (no sound card / wrong path)", body: "", want: false},
		{name: "empty file", body: "   \n", want: false},
	}

	orig := pcmStatusPath
	defer func() { pcmStatusPath = orig }()

	for _, c := range cases {
		if c.body == "" {
			pcmStatusPath = filepath.Join(t.TempDir(), "does-not-exist")
		} else {
			f := filepath.Join(t.TempDir(), "status")
			if err := os.WriteFile(f, []byte(c.body), 0o644); err != nil {
				t.Fatal(err)
			}
			pcmStatusPath = f
		}
		if got := systemIsPlaying(); got != c.want {
			t.Errorf("%s: systemIsPlaying() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestMetadataClearsWhenGone(t *testing.T) {
	origPath, origSrc := metadataPath, currentSource
	defer func() { metadataPath, currentSource = origPath, origSrc }()

	currentSource = "airplay"
	metadataPath = filepath.Join(t.TempDir(), "does-not-exist")
	currentTitle, currentArtist, currentAlbum = "old-title", "old-artist", "old-album"
	collectMetadata()
	if currentTitle != "" || currentArtist != "" || currentAlbum != "" {
		t.Errorf("metadata should be cleared when file is gone, got = %q/%q/%q", currentTitle, currentArtist, currentAlbum)
	}
}

func TestParseMetadataDropsMissingFields(t *testing.T) {
	origSrc := currentSource
	defer func() { currentSource = origSrc }()
	currentSource = "airplay"

	f := filepath.Join(t.TempDir(), "md")
	metadataPath = f

	if err := os.WriteFile(f, []byte("title=first-song\nartist=A\nalbum=album-one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	collectMetadata()
	if currentTitle != "first-song" || currentArtist != "A" || currentAlbum != "album-one" {
		t.Fatalf("parse failed: %q/%q/%q", currentTitle, currentArtist, currentAlbum)
	}

	if err := os.WriteFile(f, []byte("title=second-song\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	collectMetadata()
	if currentTitle != "second-song" || currentArtist != "" || currentAlbum != "" {
		t.Errorf("missing fields should be cleared: %q/%q/%q", currentTitle, currentArtist, currentAlbum)
	}
}
