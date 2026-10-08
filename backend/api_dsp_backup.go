// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const backupFormat = "zybo-backup"
const backupVersion = 1

type backupIR struct {
	Name string `json:"name"`

	WAVBase64 string `json:"wav_base64"`
}

type backupDDC struct {
	Name string `json:"name"`

	Text string `json:"text"`
}

type backupFile struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	SavedAt string       `json:"saved_at"`
	App     string       `json:"app"`
	State   runtimeState `json:"state"`
	Types   []SoundType  `json:"types"`
	Presets []Preset     `json:"presets"`
	IRs     []backupIR   `json:"irs"`
	DDCs    []backupDDC  `json:"ddcs"`
}

func handleDSPBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpError(w, http.StatusMethodNotAllowed, "GET only")
		return
	}
	bf := backupFile{
		Format:  backupFormat,
		Version: backupVersion,
		SavedAt: time.Now().Format(time.RFC3339),
		App:     appVersion,
		State:   snapshotRuntimeState(),
	}
	types, err := loadUserTypes()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to read user types: "+err.Error())
		return
	}

	for _, t := range types {
		if !t.Builtin {
			bf.Types = append(bf.Types, t)
		}
	}
	if bf.Types == nil {
		bf.Types = []SoundType{}
	}
	presets, err := loadUserPresets()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to read user presets: "+err.Error())
		return
	}
	if presets == nil {
		presets = []Preset{}
	}
	bf.Presets = presets

	if names, err := listIRFiles(); err == nil {
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(irDir, irNameSafe(n)))
			if err != nil {
				continue
			}
			bf.IRs = append(bf.IRs, backupIR{Name: n, WAVBase64: base64.StdEncoding.EncodeToString(b)})
		}
	}
	if bf.IRs == nil {
		bf.IRs = []backupIR{}
	}
	if names, err := listDDCFiles(); err == nil {
		for _, n := range names {
			b, err := os.ReadFile(filepath.Join(ddcDir, irNameSafe(n)))
			if err != nil {
				continue
			}
			bf.DDCs = append(bf.DDCs, backupDDC{Name: n, Text: string(b)})
		}
	}
	if bf.DDCs == nil {
		bf.DDCs = []backupDDC{}
	}
	data, err := json.MarshalIndent(bf, "", "  ")
	if err != nil {
		httpError(w, http.StatusInternalServerError, "packing failed: "+err.Error())
		return
	}
	name := "zybo-backup-" + time.Now().Format("20060102-150405") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+name+"\"")
	w.Write(data)
	log.Printf("backup: packed (%d types / %d presets / %d IRs / %d DDCs, %d bytes total)",
		len(bf.Types), len(bf.Presets), len(bf.IRs), len(bf.DDCs), len(data))
}

func listIRFiles() ([]string, error) {
	return listDataFiles(irDir)
}

func listDDCFiles() ([]string, error) {
	return listDataFiles(ddcDir)
}

func listDataFiles(dir string) ([]string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range ents {
		if e.IsDir() || len(e.Name()) == 0 || e.Name()[0] == '.' {
			continue
		}
		out = append(out, e.Name())
	}
	return out, nil
}

func handleDSPRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpError(w, http.StatusMethodNotAllowed, "POST only")
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		httpError(w, http.StatusBadRequest, "failed to read request body: "+err.Error())
		return
	}
	var bf backupFile
	if err := json.Unmarshal(raw, &bf); err != nil {
		httpError(w, http.StatusBadRequest, "not a valid backup file: "+err.Error())
		return
	}
	if bf.Format != backupFormat {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("not a zybo backup (format=%q)", bf.Format))
		return
	}
	if bf.Version != backupVersion {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("backup version %d unsupported (this machine supports %d)", bf.Version, backupVersion))
		return
	}

	irBlobs := make([][2]string, 0, len(bf.IRs))
	for _, ir := range bf.IRs {
		b, err := base64.StdEncoding.DecodeString(ir.WAVBase64)
		if err != nil || len(b) == 0 {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("IR %q data is corrupt", ir.Name))
			return
		}
		irBlobs = append(irBlobs, [2]string{ir.Name, string(b)})
	}

	for _, t := range bf.Types {
		if t.Builtin {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("backup contains built-in type %q (rejected)", t.Name))
			return
		}
	}

	cur, err := loadUserTypes()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to read existing types: "+err.Error())
		return
	}
	for _, t := range cur {
		if !t.Builtin {
			if err := deleteUserType(t.Name); err != nil {
				httpError(w, http.StatusInternalServerError, "failed to clear old types: "+err.Error())
				return
			}
		}
	}
	for _, t := range bf.Types {
		if err := saveUserType(t); err != nil {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("failed to write back type %q: %v", t.Name, err))
			return
		}
	}

	curP, err := loadUserPresets()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "failed to read existing presets: "+err.Error())
		return
	}
	for _, p := range curP {
		if err := deleteUserPreset(p.Name); err != nil {
			httpError(w, http.StatusInternalServerError, "failed to clear old presets: "+err.Error())
			return
		}
	}
	for _, p := range bf.Presets {
		if err := saveUserPreset(p); err != nil {
			httpError(w, http.StatusBadRequest, fmt.Sprintf("failed to write back preset %q: %v", p.Name, err))
			return
		}
	}

	for _, ib := range irBlobs {
		if err := saveIRFile(ib[0], []byte(ib[1])); err != nil {
			httpError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write back IR %q: %v", ib[0], err))
			return
		}
	}
	for _, dd := range bf.DDCs {
		if err := saveDDCFile(dd.Name, []byte(dd.Text)); err != nil {
			httpError(w, http.StatusInternalServerError, fmt.Sprintf("failed to write back DDC %q: %v", dd.Name, err))
			return
		}
	}

	st := bf.State
	applyRuntimeState(st, dynBassAvailable(), crossfeedAvailable(), surroundAvailable(),
		exciterAvailable(), colorfulAvailable())
	if err := setSystemVolume(currentVolume); err != nil {
		log.Printf("restore: failed to restore volume: %v", err)
	}
	if err := dspSetLimiter(dspLimiterEnabled, dspLimiterThrDB); err != nil {
		log.Printf("restore: failed to restore limiter: %v", err)
	}
	if err := dspSetLimiterTimes(dspLimiterAttMs, dspLimiterRelMs); err != nil {
		log.Printf("restore: failed to restore limiter time constants: %v", err)
	}
	if err := applyChain(st.Chain, st.PreampDB, st.GainDB); err != nil {
		httpError(w, http.StatusBadRequest, "failed to download restored chain: "+err.Error())
		return
	}
	if err := dspSetBypass(st.Bypass); err != nil {
		log.Printf("restore: failed to restore bypass state: %v", err)
	}
	markStateDirty()
	if err := flushPendingState(); err != nil {
		log.Printf("restore: failed to persist state: %v", err)
	}
	log.Printf("restore: done (%d types / %d presets / %d IRs / %d DDCs, chain %d sections)",
		len(bf.Types), len(bf.Presets), len(irBlobs), len(bf.DDCs), len(st.Chain))
	writeJSON(w, map[string]any{"ok": true})
}
