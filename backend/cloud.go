// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

var rcloneConfPath = "/etc/rclone/rclone.conf"

func handleCloudConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		_, err := os.Stat(rcloneConfPath)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"configured": err == nil})
		return
	case http.MethodPost:
	default:
		http.Error(w, "GET/POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Text string
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if body.Text == "" || len(body.Text) > 65536 {
		http.Error(w, "bad config", http.StatusBadRequest)
		return
	}
	if err := os.MkdirAll(filepath.Dir(rcloneConfPath), 0700); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := os.WriteFile(rcloneConfPath, []byte(body.Text), 0600); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleCloudMount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		On bool
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	verb := "stop"
	if body.On {
		verb = "start"
	}
	if out, err := systemctl(verb, "rclone-cloud.service"); err != nil {
		http.Error(w, "systemctl "+verb+" rclone-cloud: "+out, http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
}
