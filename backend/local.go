// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"path"
)

func handleLocalList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	dir := r.URL.Query().Get("dir")

	if dir != path.Clean("/" + dir)[1:] {
		http.Error(w, "bad dir", http.StatusBadRequest)
		return
	}
	ents, err := mpdListFiles(dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if ents == nil {
		ents = []mpdEntry{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ents)
}

func handleLocalPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		File string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.File == "" || body.File != path.Clean("/" + body.File)[1:] {
		http.Error(w, "bad file", http.StatusBadRequest)
		return
	}
	if err := mpdPlayFile(body.File); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleNasList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	if !nasConfigured() {
		http.Error(w, "nas not configured", http.StatusBadGateway)
		return
	}
	id := r.URL.Query().Get("id")
	res, err := cdsBrowse(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if res.Containers == nil {
		res.Containers = []cdsContainer{}
	}
	if res.Tracks == nil {
		res.Tracks = []cdsTrack{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func handleNasPlay(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !nasConfigured() {
		http.Error(w, "nas not configured", http.StatusBadGateway)
		return
	}
	var body struct {
		URL string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	u, err := url.Parse(body.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	if err := mpdPlayFile(body.URL); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleLocalEnqueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		File string
		URL  string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	target := body.File
	if target == "" {
		u, err := url.Parse(body.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			http.Error(w, "bad target", http.StatusBadRequest)
			return
		}
		target = body.URL
	} else if target != path.Clean("/" + target)[1:] {
		http.Error(w, "bad target", http.StatusBadRequest)
		return
	}
	if err := mpdEnqueue(target); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleLocalQueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "GET only", http.StatusMethodNotAllowed)
		return
	}
	q, err := mpdQueue()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if q.Items == nil {
		q.Items = []mpdQueueItem{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

func handleLocalCtl(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Action string
		Pos    int
		ID     int
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var err error
	switch body.Action {
	case "pause":
		err = mpdPause()
	case "stop":
		err = mpdStop()
		err = mpdStop()
	case "next":
		err = mpdNext()
	case "prev":
		err = mpdPrev()
	case "play":

		if body.ID > 0 {
			err = mpdPlayID(body.ID)
		} else {
			_, err = mpdRequest("play")
		}
	default:
		http.Error(w, "unknown action: "+body.Action, http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleLocalEnqueueDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Dir string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Dir != "" && body.Dir != path.Clean("/" + body.Dir)[1:] {
		http.Error(w, "bad dir", http.StatusBadRequest)
		return
	}
	files, err := mpdWalkDir(body.Dir)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := mpdPlayFiles(files); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"added": len(files)})
}

func handleNasEnqueue(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	if !nasConfigured() {
		http.Error(w, "nas not configured", http.StatusBadGateway)
		return
	}
	var body struct {
		ID string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	tracks, err := cdsWalkTracks(body.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	files := make([]string, 0, len(tracks))
	for _, t := range tracks {
		files = append(files, t.URI)
	}
	if err := mpdPlayFiles(files); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"added": len(files)})
}

func handleLocalUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	n, err := mpdUpdate()
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{"updating": n})
}

func handleNasConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"configured": nasConfigured(), "base": cdsBase})
		return
	case http.MethodPost:
	default:
		http.Error(w, "GET/POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Base string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	u, err := url.Parse(body.Base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "bad base", http.StatusBadRequest)
		return
	}
	cdsBase = body.Base
	w.WriteHeader(http.StatusOK)
}
