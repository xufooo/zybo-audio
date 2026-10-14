// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEnqueueDirRecursive(t *testing.T) {
	withStubMPD(t, map[string]string{
		"status":          "state: stop\nplaylistlength: 0\nOK\n",
		`listfiles ""`:    "directory: sub\nLast-Modified: 2026-09-10T03:01:52Z\nfile: a.flac\nsize: 100\nLast-Modified: 2026-09-14T09:59:21Z\nOK\n",
		`listfiles "sub"`: "file: b.flac\nsize: 200\nLast-Modified: 2026-09-14T09:59:21Z\nOK\n",
		"clear":           "OK\n",
		"add":             "OK\n",
		"play":            "OK\n",
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/local/enqueue-dir", strings.NewReader(`{"dir":""}`))
	handleLocalEnqueueDir(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("enqueue-dir must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct{ Added int }
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got.Added != 2 {
		t.Fatalf("root+subdir must enqueue 2, got %+v (err %v)", got, err)
	}
}

func TestEnqueueContainerRecursive(t *testing.T) {
	withStubMPD(t, map[string]string{
		"status": "state: stop\nplaylistlength: 0\nOK\n",
		"clear":  "OK\n",
		"add":    "OK\n",
		"play":   "OK\n",
	})
	saved := cdsBase
	cdsBase = stubCDSLevel(t)
	t.Cleanup(func() { cdsBase = saved })
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/nas/enqueue", strings.NewReader(`{"id":"1"}`))
	handleNasEnqueue(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("nas enqueue must 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	var got struct{ Added int }
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil || got.Added != 2 {
		t.Fatalf("two-level containers must enqueue 2, got %+v (err %v)", got, err)
	}
}

func stubCDSLevel(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		s := string(body)
		var resp string
		switch {
		case strings.Contains(s, "<ObjectID>1</ObjectID>"):
			resp = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><Result>&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"&gt;&lt;container id="2" parentID="1" restricted="1"&gt;&lt;dc:title xmlns:dc="http://purl.org/dc/elements/1.1/"&gt;Sub&lt;/dc:title&gt;&lt;/container&gt;&lt;item id="t0" parentID="1" restricted="1"&gt;&lt;dc:title xmlns:dc="http://purl.org/dc/elements/1.1/"&gt;Root Song&lt;/dc:title&gt;&lt;res protocolInfo="http-get:*:audio/mpeg:*"&gt;http://x/root.mp3&lt;/res&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;</Result><NumberReturned>2</NumberReturned><TotalMatches>2</TotalMatches></u:BrowseResponse></s:Body></s:Envelope>`
		case strings.Contains(s, "<ObjectID>2</ObjectID>"):
			resp = `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:BrowseResponse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1"><Result>&lt;DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/"&gt;&lt;item id="t1" parentID="2" restricted="1"&gt;&lt;dc:title xmlns:dc="http://purl.org/dc/elements/1.1/"&gt;Sub Song&lt;/dc:title&gt;&lt;res protocolInfo="http-get:*:audio/mpeg:*"&gt;http://x/sub.mp3&lt;/res&gt;&lt;/item&gt;&lt;/DIDL-Lite&gt;</Result><NumberReturned>1</NumberReturned><TotalMatches>1</TotalMatches></u:BrowseResponse></s:Body></s:Envelope>`
		default:
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Write([]byte(resp))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
