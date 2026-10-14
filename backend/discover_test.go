// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const fakeDesc = `<?xml version="1.0"?><root xmlns="urn:schemas-upnp-org:device-1-0">` +
	`<device><deviceType>urn:schemas-upnp-org:device:MediaServer:1</deviceType>` +
	`<friendlyName>FakeNAS</friendlyName>` +
	`<serviceList><service><serviceType>urn:schemas-upnp-org:service:ContentDirectory:1</serviceType>` +
	`<controlURL>/CDS/control</controlURL></service></serviceList>` +
	`</device></root>`

func stubSSDP(t *testing.T, location string) string {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("UDP stub failed to start: %v", err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 2048)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if !strings.Contains(string(buf[:n]), "M-SEARCH") {
				continue
			}
			resp := "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=90\r\n" +
				"LOCATION: " + location + "\r\n" +
				"ST: urn:schemas-upnp-org:device:MediaServer:1\r\n\r\n"
			pc.WriteTo([]byte(resp), addr)
		}
	}()
	return pc.LocalAddr().String()
}

func TestDiscoverFindsStubServer(t *testing.T) {
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
	savedSel := cdsCurrent
	t.Cleanup(func() { cdsCurrent = savedSel })
	servers, err := discoverServers()
	if err != nil {
		t.Fatalf("discovery must succeed: %v", err)
	}
	if len(servers) != 1 || servers[0].Name != "FakeNAS" {
		t.Fatalf("must discover FakeNAS, got %+v", servers)
	}
	if !strings.HasSuffix(servers[0].Control, "/CDS/control") {
		t.Fatalf("wrong control URL, got %+v", servers[0])
	}
}
