// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	ht "html"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

var cdsServers []cdsServer
var cdsCurrent = -1

var nasRememberPath = "/var/lib/zybo-audio/nas.json"

func rememberServer() {
	if !nasConfigured() {
		return
	}
	data, err := json.Marshal(cdsServers[cdsCurrent])
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(nasRememberPath), 0755); err != nil {
		return
	}
	_ = os.WriteFile(nasRememberPath, data, 0600)
}

func recallServer() {
	data, err := os.ReadFile(nasRememberPath)
	if err != nil {
		return
	}
	var s cdsServer
	if err := json.Unmarshal(data, &s); err != nil || s.Control == "" {
		return
	}
	cdsServers = []cdsServer{s}
	cdsCurrent = 0
}

func nasConfigured() bool { return cdsCurrent >= 0 && cdsCurrent < len(cdsServers) }

type cdsServer struct {
	Name     string `json:"name"`
	Location string `json:"location"`
	Control  string `json:"control"`
}

var ssdpTarget = "239.255.255.250:1900"
var ssdpTimeout = 6 * time.Second

var (
	ssdpCacheMu sync.Mutex
	ssdpCache   = map[string]time.Time{}
)

func ssdpListen() {
	target, err := net.ResolveUDPAddr("udp", "239.255.255.250:1900")
	if err != nil {
		return
	}
	pc, err := net.ListenMulticastUDP("udp", nil, target)
	if err != nil {
		log.Printf("ssdp listen: %v (servers page falls back to active search)", err)
		return
	}
	defer pc.Close()
	buf := make([]byte, 8192)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		txt := string(buf[:n])
		if !strings.HasPrefix(txt, "NOTIFY") {
			continue
		}
		for _, ln := range strings.Split(txt, "\r\n") {
			if len(ln) > 9 && strings.EqualFold(ln[:9], "location:") {
				loc := strings.TrimSpace(ln[9:])
				ssdpCacheMu.Lock()
				ssdpCache[loc] = time.Now()
				ssdpCacheMu.Unlock()
			}
		}
	}
}

func discoverServers() ([]cdsServer, error) {
	target, err := net.ResolveUDPAddr("udp", ssdpTarget)
	if err != nil {
		return nil, err
	}
	var pc net.PacketConn
	if target.IP.IsMulticast() {
		mc, err := net.ListenMulticastUDP("udp", nil, target)
		if err != nil {
			return nil, fmt.Errorf("ssdp listen: %w", err)
		}
		defer mc.Close()
		pc = mc
		if err := mc.SetReadDeadline(time.Now().Add(ssdpTimeout)); err != nil {
			return nil, err
		}
	} else {
		pc, err = net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		defer pc.Close()
		if err := pc.SetReadDeadline(time.Now().Add(ssdpTimeout)); err != nil {
			return nil, err
		}
	}
	for _, st := range []string{
		"urn:schemas-upnp-org:device:MediaServer:1",
		"ssdp:all",
	} {
		msg := "M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\n" +
			"MAN: \"ns=01\"; ns=01\r\nMX: 3\r\nST: " + st + "\r\n\r\n"
		if _, err := pc.WriteTo([]byte(msg), target); err != nil {
			return nil, err
		}
	}
	locs := []string{}
	seen := map[string]bool{}
	buf := make([]byte, 8192)
	for {
		n, _, err := pc.ReadFrom(buf)
		if err != nil {
			break
		}
		for _, ln := range strings.Split(string(buf[:n]), "\r\n") {
			if len(ln) > 9 && strings.EqualFold(ln[:9], "location:") {
				loc := strings.TrimSpace(ln[9:])
				if !seen[loc] {
					seen[loc] = true
					locs = append(locs, loc)
				}
			}
		}
	}
	var out []cdsServer
	for _, loc := range locs {
		s, err := describeServer(loc)
		if err != nil {
			continue
		}
		out = append(out, s)
	}

	ssdpCacheMu.Lock()
	known := map[string]bool{}
	for _, s := range out {
		known[s.Location] = true
	}
	for loc, at := range ssdpCache {
		if time.Since(at) > 30*time.Minute || known[loc] {
			continue
		}
		if s, err := describeServer(loc); err == nil {
			out = append(out, s)
			known[loc] = true
		}
	}
	ssdpCacheMu.Unlock()
	return out, nil
}

func describeServer(location string) (cdsServer, error) {
	s := cdsServer{Location: location}
	cli := &http.Client{Timeout: 15 * time.Second}
	resp, err := cli.Get(location)
	if err != nil {
		return s, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return s, err
	}
	base, err := url.Parse(location)
	if err != nil {
		return s, err
	}
	type svc struct {
		Type string `xml:"serviceType"`
		Ctrl string `xml:"controlURL"`
	}
	type dev struct {
		Type     string `xml:"deviceType"`
		Name     string `xml:"friendlyName"`
		Services []svc  `xml:"serviceList>service"`
		Devices  []dev  `xml:"device"`
	}
	var root struct {
		Device dev `xml:"device"`
	}
	if err := xml.Unmarshal(body, &root); err != nil {
		return s, err
	}
	var walk func(d dev) bool
	walk = func(d dev) bool {
		if strings.Contains(d.Type, "MediaServer") {
			s.Name = d.Name
			for _, sv := range d.Services {
				if strings.Contains(sv.Type, "ContentDirectory") {
					ref, err := url.Parse(sv.Ctrl)
					if err != nil {
						return false
					}
					s.Control = base.ResolveReference(ref).String()
					return true
				}
			}
		}
		for _, sub := range d.Devices {
			if walk(sub) {
				return true
			}
		}
		return false
	}
	if !walk(root.Device) || s.Control == "" {
		return s, fmt.Errorf("not a media server: %s", location)
	}
	return s, nil
}

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
	if !nasConfigured() {
		return "", fmt.Errorf("nas not configured")
	}
	env := `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" ` +
		`s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>` +
		inner + `</s:Body></s:Envelope>`
	req, err := http.NewRequest("POST", cdsServers[cdsCurrent].Control, strings.NewReader(env))
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
