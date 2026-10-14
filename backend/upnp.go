// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/xml"
	"fmt"
	ht "html"
	"io"
	"net/http"
	"strings"
	"time"
)

var cdsBase = ""

func nasConfigured() bool { return cdsBase != "" }

type cdsContainer struct {
	ID    string
	Title string
}

type cdsTrack struct {
	ID    string
	Title string
	URI   string
	MIME  string
}

type cdsResult struct {
	Containers []cdsContainer
	Tracks     []cdsTrack
	Total      int
}

type didl struct {
	XMLName    xml.Name        `xml:"DIDL-Lite"`
	Containers []didlContainer `xml:"container"`
	Items      []didlItem      `xml:"item"`
}

type didlContainer struct {
	ID    string `xml:"id,attr"`
	Title string `xml:"title"`
}

type didlItem struct {
	ID    string    `xml:"id,attr"`
	Title string    `xml:"title"`
	Res   []didlRes `xml:"res"`
}

type didlRes struct {
	Protocol string `xml:"protocolInfo,attr"`
	URL      string `xml:",chardata"`
}

func cdsSOAP(action, inner string) (string, error) {
	env := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` +
		inner + `</s:Body></s:Envelope>`
	req, err := http.NewRequest("POST", cdsBase+"/ContentDirectory/control", strings.NewReader(env))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"urn:schemas-upnp-org:service:ContentDirectory:1#`+action+`"`)
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return "", fmt.Errorf("cds %s: %w", action, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("cds %s: HTTP %d", action, resp.StatusCode)
	}
	return string(body), nil
}

func xmlEscape(s string) string {
	r := strings.ReplaceAll(s, "&", "&amp;")
	r = strings.ReplaceAll(r, "<", "&lt;")
	return strings.ReplaceAll(r, ">", "&gt;")
}

func soapResult(env, tag string) string {
	start := strings.Index(env, "<"+tag+">")
	end := strings.Index(env, "</"+tag+">")
	if start < 0 || end < 0 || end <= start {
		return ""
	}
	return env[start+len(tag)+2 : end]
}

func pickRes(res []didlRes) (uri, mime string) {
	fallback := ""
	fallbackMIME := ""
	for _, r := range res {
		parts := strings.SplitN(r.Protocol, ":", 4)
		m := ""
		if len(parts) >= 3 {
			m = parts[2]
		}
		u := strings.TrimSpace(r.URL)
		if u == "" {
			continue
		}
		if strings.Contains(m, "flac") {
			return u, m
		}
		if fallback == "" {
			fallback, fallbackMIME = u, m
			continue
		}
		if strings.HasPrefix(m, "audio/") && !strings.HasPrefix(fallbackMIME, "audio/") {
			fallback, fallbackMIME = u, m
		}
	}
	return fallback, fallbackMIME
}

func cdsWalkTracks(objectID string) ([]cdsTrack, error) {
	var out []cdsTrack
	var walk func(id string) error
	walk = func(id string) error {
		res, err := cdsBrowse(id)
		if err != nil {
			return err
		}
		out = append(out, res.Tracks...)
		if len(out) > 500 {
			return fmt.Errorf("too many (>500), refusing")
		}
		for _, c := range res.Containers {
			if err := walk(c.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(objectID); err != nil {
		return nil, err
	}
	return out, nil
}

func cdsBrowse(objectID string) (cdsResult, error) {
	var out cdsResult
	inner := `<u:Browse xmlns:u="urn:schemas-upnp-org:service:ContentDirectory:1">` +
		`<ObjectID>` + xmlEscape(objectID) + `</ObjectID>` +
		`<BrowseFlag>BrowseDirectChildren</BrowseFlag><Filter>*</Filter>` +
		`<StartingIndex>0</StartingIndex><RequestedCount>100</RequestedCount>` +
		`<SortCriteria></SortCriteria></u:Browse>`
	env, err := cdsSOAP("Browse", inner)
	if err != nil {
		return out, err
	}
	var total int
	fmt.Sscanf(soapResult(env, "TotalMatches"), "%d", &total)
	out.Total = total
	var dl didl
	if err := xml.Unmarshal([]byte(ht.UnescapeString(soapResult(env, "Result"))), &dl); err != nil {
		return out, fmt.Errorf("cds DIDL parse: %w", err)
	}
	for _, c := range dl.Containers {
		out.Containers = append(out.Containers, cdsContainer{ID: c.ID, Title: c.Title})
	}
	for _, it := range dl.Items {
		u, m := pickRes(it.Res)
		if u == "" {
			continue
		}
		out.Tracks = append(out.Tracks, cdsTrack{ID: it.ID, Title: it.Title, URI: u, MIME: m})
	}
	return out, nil
}
