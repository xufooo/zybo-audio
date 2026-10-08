// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	typeFormat    = "zybo-audio-type"
	typeFormatVer = 1
)

var userTypeDir = envOr("ZYBO_TYPE_DIR", "/var/lib/zybo-audio/types")

type TypeEffect struct {
	ID string `json:"id"`

	Name    string             `json:"name,omitempty"`
	Kind    string             `json:"kind,omitempty"`
	Desc    string             `json:"desc,omitempty"`
	Enabled bool               `json:"enabled"`
	Mode    string             `json:"mode,omitempty"`
	Params  map[string]float64 `json:"params,omitempty"`
	Bands   []ChainItem        `json:"bands,omitempty"`
}

type SoundType struct {
	Format  string       `json:"format"`
	Version int          `json:"version"`
	Name    string       `json:"name"`
	Note    string       `json:"note,omitempty"`
	Builtin bool         `json:"builtin,omitempty"`
	Effects []TypeEffect `json:"effects"`

	File string `json:"-"`
}

func validateType(t *SoundType) error {
	if t.Format != "" && t.Format != typeFormat {
		return fmt.Errorf("Not a native type format (format=%q)", t.Format)
	}
	if t.Version != 0 && t.Version != typeFormatVer {
		return fmt.Errorf("Type version %d unsupported (supports %d)", t.Version, typeFormatVer)
	}
	t.Format = typeFormat
	t.Version = typeFormatVer
	if strings.TrimSpace(t.Name) == "" {
		return fmt.Errorf("Type missing name")
	}
	if t.Effects == nil {
		return fmt.Errorf("Type missing effects (whole effect setup; pass [] explicitly to clear)")
	}
	for i, e := range t.Effects {
		if strings.TrimSpace(e.ID) == "" {
			return fmt.Errorf("Effect %d missing id", i+1)
		}

		for j, b := range e.Bands {
			if err := chainItemCheck(b); err != nil {
				return fmt.Errorf("Effect %q section %d: %w", e.ID, j+1, err)
			}
		}
	}
	return nil
}

func builtinTypes() []SoundType {
	return []SoundType{
		{
			Format: typeFormat, Version: typeFormatVer, Name: "Headphones", Builtin: true,
			Note: "In-ear / over-ear: gentle low-end lift, tame harsh highs",
			Effects: []TypeEffect{

				{ID: "bass", Enabled: true, Mode: "light", Bands: []ChainItem{
					{Type: "peq", Enabled: true, Freq: 60, GainDB: 5, Q: 0.7},
					{Type: "peq", Enabled: true, Freq: 150, GainDB: 4.3, Q: 0.8},
					{Type: "peq", Enabled: true, Freq: 400, GainDB: 2, Q: 0.8},
				}},
				{ID: "soft", Enabled: true, Mode: "light", Bands: []ChainItem{
					{Type: "peq", Enabled: true, Freq: 3000, GainDB: -2, Q: 1.0},
					{Type: "peq", Enabled: true, Freq: 6000, GainDB: -2.5, Q: 1.0},
					{Type: "peq", Enabled: true, Freq: 10000, GainDB: -3.5, Q: 0.8},
				}},

				{ID: "spatial", Enabled: false, Params: map[string]float64{
					"fcut_hz": 700, "feed": 60,
				}},

				{ID: "dyn", Enabled: false, Params: map[string]float64{"gain_db": 6}},
				{ID: "limiter", Enabled: true, Mode: "truepeak", Params: map[string]float64{
					"thr_db": 0, "att_ms": 0.09, "rel_ms": 60,
				}},
			},
		},
		{
			Format: typeFormat, Version: typeFormatVer, Name: "Small Speakers", Builtin: true,
			Note: "Desktop / Bluetooth speakers: fill in lows, bring vocals forward",
			Effects: []TypeEffect{
				{ID: "bass", Enabled: true, Mode: "mid", Bands: []ChainItem{
					{Type: "peq", Enabled: true, Freq: 60, GainDB: 8, Q: 0.7},
					{Type: "peq", Enabled: true, Freq: 150, GainDB: 6.8, Q: 0.8},
					{Type: "peq", Enabled: true, Freq: 400, GainDB: 3.2, Q: 0.8},
				}},
				{ID: "vocal", Enabled: true, Mode: "light", Bands: []ChainItem{
					{Type: "peq", Enabled: true, Freq: 400, GainDB: -2, Q: 0.8},
					{Type: "peq", Enabled: true, Freq: 1000, GainDB: 3.6, Q: 1.0},
					{Type: "peq", Enabled: true, Freq: 3000, GainDB: 4, Q: 1.0},
					{Type: "peq", Enabled: true, Freq: 6000, GainDB: 2, Q: 1.0},
				}},

				{ID: "dyn", Enabled: false, Params: map[string]float64{"gain_db": 6}},
				{ID: "limiter", Enabled: true, Mode: "truepeak", Params: map[string]float64{
					"thr_db": 0, "att_ms": 0.09, "rel_ms": 60,
				}},
			},
		},
		{
			Format: typeFormat, Version: typeFormatVer, Name: "Amplifier", Builtin: true,
			Note: "Large speakers / amplifier: no coloration, limiter protection only",
			Effects: []TypeEffect{
				{ID: "bass", Enabled: false, Mode: "mid"},
				{ID: "vocal", Enabled: false, Mode: "mid"},
				{ID: "vocalhd", Enabled: false, Mode: "mid"},
				{ID: "soft", Enabled: false, Mode: "mid"},
				{ID: "night", Enabled: false, Mode: "mid"},

				{ID: "dyn", Enabled: false, Params: map[string]float64{"gain_db": 6}},
				{ID: "limiter", Enabled: true, Mode: "truepeak", Params: map[string]float64{
					"thr_db": 0, "att_ms": 0.09, "rel_ms": 60,
				}},
			},
		},
	}
}

func loadUserTypes() ([]SoundType, error) {
	entries, err := os.ReadDir(userTypeDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []SoundType
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(userTypeDir, e.Name()))
		if err != nil {
			log.Printf("type %s read failed, skipping: %v", e.Name(), err)
			continue
		}
		var t SoundType
		if err := json.Unmarshal(data, &t); err != nil {
			log.Printf("type %s is not valid JSON, skipping: %v", e.Name(), err)
			continue
		}
		if err := validateType(&t); err != nil {
			log.Printf("type %s validation failed, skipping: %v", e.Name(), err)
			continue
		}
		t.Builtin = false
		t.File = e.Name()
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func loadTypes() ([]SoundType, error) {
	user, err := loadUserTypes()
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []SoundType
	for _, t := range user {
		out = append(out, t)
		seen[t.Name] = true
	}
	for _, t := range builtinTypes() {
		if !seen[t.Name] {
			out = append(out, t)
		}
	}
	return out, nil
}

func saveUserType(t SoundType) error {
	if err := validateType(&t); err != nil {
		return err
	}
	t.Builtin = false
	t.File = ""
	if err := os.MkdirAll(userTypeDir, 0o755); err != nil {
		return fmt.Errorf("Failed to create type directory: %w", err)
	}
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(userTypeDir, presetFileName(t.Name))
	tmp, err := os.CreateTemp(userTypeDir, ".tmp-type-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, final)
}

func deleteUserType(name string) error {
	list, err := loadUserTypes()
	if err != nil {
		return err
	}
	for _, t := range list {
		if t.Name == name {
			return os.Remove(filepath.Join(userTypeDir, t.File))
		}
	}
	if isBuiltinType(name) {
		return fmt.Errorf("%q is a built-in type, cannot delete", name)
	}
	return fmt.Errorf("User type %q not found", name)
}

func isBuiltinType(name string) bool {
	for _, t := range builtinTypes() {
		if t.Name == name {
			return true
		}
	}
	return false
}

func renameUserType(from, to string) (string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		return "", fmt.Errorf("New name must not be empty")
	}
	if from == to {
		return to, nil
	}
	if isBuiltinType(to) {
		return "", fmt.Errorf("New name %q clashes with a built-in type, pick another", to)
	}
	list, err := loadUserTypes()
	if err != nil {
		return "", err
	}
	var found *SoundType
	for i := range list {
		if list[i].Name == from {
			found = &list[i]
			break
		}
	}
	if found == nil {
		if isBuiltinType(from) {
			return "", fmt.Errorf("%q is a built-in type, cannot rename; use 'Save As' to copy it", from)
		}
		return "", fmt.Errorf("User type %q not found", from)
	}
	for _, t := range list {
		if t.Name == to {
			return "", fmt.Errorf("A type named %q already exists", to)
		}
	}
	renamed := *found
	renamed.Name = to
	if err := saveUserType(renamed); err != nil {
		return "", err
	}
	if err := os.Remove(filepath.Join(userTypeDir, found.File)); err != nil {
		log.Printf("Type renamed but old file removal failed (new name active): %v", err)
	}
	return to, nil
}

func findType(name string) (SoundType, error) {
	list, err := loadTypes()
	if err != nil {
		return SoundType{}, err
	}
	for _, t := range list {
		if t.Name == name {
			return t, nil
		}
	}
	return SoundType{}, fmt.Errorf("Type %q not found", name)
}
