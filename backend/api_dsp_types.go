// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

func handleDSPTypes(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		list, err := loadTypes()
		if err != nil {
			httpError(w, http.StatusInternalServerError, "Failed to read types:"+err.Error())
			return
		}

		names := make([]string, 0, 3)
		for _, t := range builtinTypes() {
			names = append(names, t.Name)
		}
		writeJSON(w, map[string]any{"types": list, "builtin_names": names})
	case http.MethodPost:
		var body struct {
			Name    string       `json:"name"`
			Note    string       `json:"note"`
			Effects []TypeEffect `json:"effects"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			httpError(w, http.StatusBadRequest, "Request body is not valid JSON:"+err.Error())
			return
		}
		if strings.TrimSpace(body.Name) == "" {
			httpError(w, http.StatusBadRequest, "Missing name")
			return
		}
		if body.Effects == nil {
			httpError(w, http.StatusBadRequest, "Missing effects (whole effect setup; pass [] explicitly to clear)")
			return
		}
		t := SoundType{Name: body.Name, Note: body.Note, Effects: body.Effects}
		if err := saveUserType(t); err != nil {
			httpError(w, http.StatusBadRequest, "Save failed:"+err.Error())
			return
		}
		log.Printf("type: saved sound type %q (%d cards)", t.Name, len(t.Effects))
		writeJSON(w, map[string]any{"ok": true, "name": t.Name})
	default:
		httpError(w, http.StatusMethodNotAllowed, "Only GET / POST supported")
	}
}

func handleDSPTypeDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "Only POST supported")
		return
	}
	var body struct{ Name string }
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "Request body is not valid JSON:"+err.Error())
		return
	}
	if err := deleteUserType(body.Name); err != nil {
		status := http.StatusNotFound
		if strings.Contains(err.Error(), "built-in") {
			status = http.StatusConflict
		}
		httpError(w, status, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func handleDSPTypeRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "Only POST supported")
		return
	}
	var body struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		httpError(w, http.StatusBadRequest, "Request body is not valid JSON:"+err.Error())
		return
	}
	if strings.TrimSpace(body.From) == "" {
		httpError(w, http.StatusBadRequest, "Missing from (which one to rename)")
		return
	}
	to, err := renameUserType(body.From, body.To)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "not found") || strings.Contains(err.Error(), "built-in type") {
			status = http.StatusNotFound
		}
		httpError(w, status, err.Error())
		return
	}
	log.Printf("type: renamed sound type %q -> %q", body.From, to)
	writeJSON(w, map[string]any{"ok": true, "name": to})
}
