// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const cdsBrowseResp = `<?xml version="1.0"?>
<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>
<u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">
<Result>&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"&gt;&lt;container id="26$1" parentID="26" restricted="1"&gt;&lt;dc:title&gt;Frédéric Chopin. Chopin Complete Edition&lt;/dc:title&gt;&lt;/container&gt;&lt;item id="26$1$@75" parentID="26$1" restricted="1"&gt;&lt;dc:title&gt;Piano Concerto No. 1&lt;/dc:title&gt;&lt;res protocolInfo="http-get:*:audio/wav:DLNA.ORG_PN=WAV"&gt;http://198.51.100.200:50002/transcoder/x.wav&lt;/res&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;</Result>
<NumberReturned>2</NumberReturned><TotalMatches>2</TotalMatches>
</u:BrowseResponse></s:Body></s:Envelope>`

func stubCDS(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		if strings.Contains(string(body), "GetSearchCapabilities") {
			w.Write([]byte(`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:GetSearchCapabilitiesResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><SearchCaps>dc:title</SearchCaps></u:GetSearchCapabilitiesResponse></s:Body></s:Envelope>`))
			return
		}
		w.Write([]byte(cdsBrowseResp))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestCDSBrowseParsesContainersAndTracks(t *testing.T) {
	saved := cdsBase
	cdsBase = stubCDS(t)
	t.Cleanup(func() { cdsBase = saved })
	res, err := cdsBrowse("26")
	if err != nil {
		t.Fatalf("browse must succeed, err: %v", err)
	}
	if len(res.Containers) != 1 || res.Containers[0].ID != "26$1" {
		t.Fatalf("container parse wrong, got %+v", res.Containers)
	}
	if len(res.Tracks) != 1 || !strings.HasPrefix(res.Tracks[0].URI, "http://198.51.100.200:50002/") {
		t.Fatalf("entry parse wrong, got %+v", res.Tracks)
	}
	if res.Tracks[0].MIME != "audio/wav" {
		t.Fatalf("mime must be audio/wav, got %q", res.Tracks[0].MIME)
	}
}

func TestCDSBrowseFailsCleanly(t *testing.T) {
	saved := cdsBase
	cdsBase = "http://127.0.0.1:1"
	t.Cleanup(func() { cdsBase = saved })
	if _, err := cdsBrowse("0"); err == nil {
		t.Fatalf("must error when unreachable")
	}
}
