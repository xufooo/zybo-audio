// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

func stubMPD(t *testing.T, script map[string]string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("stub failed to start: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				fmt.Fprintf(c, "OK MPD 0.24.4\n")
				buf := make([]byte, 1024)
				for {
					n, err := c.Read(buf)
					if err != nil {
						return
					}
					cmd := strings.Fields(string(buf[:n]))
					if len(cmd) == 0 {
						continue
					}
					if cmd[0] == "close" {
						return
					}
					full := strings.TrimSpace(string(buf[:n]))
					resp, ok := script[full]
					if !ok {
						resp, ok = script[cmd[0]]
					}
					if !ok {
						resp = "ACK [5@0] {} unknown command\n"
					}
					fmt.Fprint(c, resp)
				}
			}(c)
		}
	}()
	return ln.Addr().String()
}

func TestMPDStatusParsesState(t *testing.T) {
	addr := stubMPD(t, map[string]string{
		"status": "repeat: 0\nstate: stop\nplaylistlength: 0\nOK\n",
	})
	saved := mpdAddr
	mpdAddr = addr
	t.Cleanup(func() { mpdAddr = saved })
	st, err := mpdStatus()
	if err != nil {
		t.Fatalf("status must succeed, err: %v", err)
	}
	if st.State != "stop" {
		t.Fatalf("state must be stop, got %q", st.State)
	}
}

func TestMPDListFilesParsesEntries(t *testing.T) {
	addr := stubMPD(t, map[string]string{
		"listfiles": "file: a.flac\nsize: 33915194\nLast-Modified: 2026-09-14T09:59:21Z\n" +
			"directory: sub\nLast-Modified: 2026-09-10T03:01:52Z\nOK\n",
	})
	saved := mpdAddr
	mpdAddr = addr
	t.Cleanup(func() { mpdAddr = saved })
	ents, err := mpdListFiles("")
	if err != nil {
		t.Fatalf("listfiles must succeed, err: %v", err)
	}
	if len(ents) != 2 || ents[0].Name != "a.flac" || ents[0].IsDir ||
		ents[1].Name != "sub" || !ents[1].IsDir {
		t.Fatalf("entry parse wrong, got %+v", ents)
	}
	if ents[0].Size != 33915194 {
		t.Fatalf("size must be 33915194, got %d", ents[0].Size)
	}
}

func TestMPDErrorOnACK(t *testing.T) {
	addr := stubMPD(t, map[string]string{})
	saved := mpdAddr
	mpdAddr = addr
	t.Cleanup(func() { mpdAddr = saved })
	if _, err := mpdStatus(); err == nil {
		t.Fatalf("stub without status script must ACK, client did not error")
	}
}

func TestUnitsToStopIncludesSocket(t *testing.T) {
	got := unitsToStop("local")
	hasSvc, hasSock := false, false
	for _, u := range got {
		if u == "mpd.service" {
			hasSvc = true
		}
		if u == "mpd.socket" {
			hasSock = true
		}
	}
	if !hasSvc || !hasSock {
		t.Fatalf("stopping local must also stop mpd.socket (else 6600 wakes it), got %v", got)
	}
	if got := unitsToStop("airplay"); len(got) != 1 || got[0] != "shairport-sync.service" {
		t.Fatalf("airplay stop set must stay, got %v", got)
	}
}

func TestMPDStopAndPlayFile(t *testing.T) {
	addr := stubMPD(t, map[string]string{
		"clear": "OK\n",
		"add":   "OK\n",
		"play":  "OK\n",
		"stop":  "OK\n",
	})
	saved := mpdAddr
	mpdAddr = addr
	t.Cleanup(func() { mpdAddr = saved })
	if err := mpdPlayFile("newdivide-48k.flac"); err != nil {
		t.Fatalf("playfile must succeed, err: %v", err)
	}
	if err := mpdStop(); err != nil {
		t.Fatalf("stop must succeed, err: %v", err)
	}
}
