// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func handleDSPEQParse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		httpError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	text := strings.TrimSpace(string(raw))
	if text == "" {
		httpError(w, http.StatusBadRequest, "empty request body")
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		name = "imported EQ"
	}

	if strings.HasPrefix(text, "{") {
		var single Preset
		if err := json.Unmarshal(raw, &single); err != nil || strings.TrimSpace(single.Name) == "" {
			httpError(w, http.StatusBadRequest, "this does not look like a native preset JSON (missing name)")
			return
		}
		if err := validatePreset(&single); err != nil {
			httpError(w, http.StatusBadRequest, "invalid preset: "+err.Error())
			return
		}
		warns := []string{}
		if why := presetUnsupported(single); len(why) > 0 {
			warns = append(warns, "this preset uses effects not available on this machine: "+strings.Join(why, "; "))
		}
		writeJSON(w, map[string]any{
			"ok": true, "name": single.Name, "bands": single.Chain,
			"count": len(single.Chain), "warnings": warns,
		})
		return
	}

	pe, perr := parseEqualizerAPO(text)
	if perr != nil && pe == nil {
		httpError(w, http.StatusBadRequest, perr.Error())
		return
	}
	p, warns, err := presetFromAPO(pe, name, bandLimit())
	if err != nil {
		httpError(w, http.StatusBadRequest, "parse failed: "+err.Error())
		return
	}
	if perr != nil {
		warns = append(warns, perr.Error())
	}
	if warns == nil {
		warns = []string{}
	}
	writeJSON(w, map[string]any{
		"ok": true, "name": p.Name, "bands": p.Chain,
		"count": len(p.Chain), "warnings": warns,
	})
}
