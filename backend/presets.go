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
	presetFormat        = "zybo-audio-preset"
	presetLibraryFormat = "zybo-audio-preset-library"
	presetFormatVersion = 1
)

var (
	presetLibraryPath = envOr("ZYBO_PRESET_LIB", "/usr/share/zybo-audio/presets/library.json")
	userPresetDir     = envOr("ZYBO_PRESET_DIR", "/var/lib/zybo-audio/presets")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

type Preset struct {
	Format   string      `json:"format"`
	Version  int         `json:"version"`
	Name     string      `json:"name"`
	Author   string      `json:"author,omitempty"`
	Note     string      `json:"note,omitempty"`
	Target   string      `json:"target,omitempty"`
	Stage    string      `json:"stage,omitempty"`
	Requires []string    `json:"requires,omitempty"`
	Chain    []ChainItem `json:"chain"`

	PreampDB float64 `json:"preamp_db"`
	VolumeDB float64 `json:"volume_db,omitempty"`

	Builtin bool `json:"builtin,omitempty"`

	File string `json:"-"`
}

type presetLibrary struct {
	Format  string   `json:"format"`
	Version int      `json:"version"`
	Note    string   `json:"note,omitempty"`
	Presets []Preset `json:"presets"`
}

func validatePreset(p *Preset) error {
	if p.Format != "" && p.Format != presetFormat {
		return fmt.Errorf("not a native preset format (format=%q)", p.Format)
	}
	if p.Version != 0 && p.Version != presetFormatVersion {
		return fmt.Errorf("preset version %d not supported (this machine supports %d)", p.Version, presetFormatVersion)
	}
	p.Format = presetFormat
	p.Version = presetFormatVersion
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("preset missing name")
	}
	for i, it := range p.Chain {
		nt := normalizeChainType(it.Type)
		if nt == "" {
			return fmt.Errorf("item %d missing type", i+1)
		}
		if _, known := chainTypeToSlot[nt]; known {
			continue
		}
		if _, planned := chainPlannedType[nt]; planned {
			continue
		}
		if _, fx := chainEffectType[nt]; fx {

			continue
		}
		return fmt.Errorf("item %d type=%q unknown", i+1, it.Type)
	}

	if len(p.Requires) > 0 {
		declared := map[string]bool{}
		for _, r := range p.Requires {
			declared[normalizeChainType(r)] = true
		}
		for _, need := range chainRequires(p.Chain) {
			if !declared[need] {
				return fmt.Errorf("requires is missing chain-used %q", need)
			}
		}
	}
	return nil
}

func presetUnsupported(p Preset) []string {
	bad := chainUnsupported(p.Chain)
	if len(bad) == 0 {
		return nil
	}
	out := make([]string, 0, len(bad))
	for t, why := range bad {
		out = append(out, t+": "+why)
	}
	sort.Strings(out)
	return out
}

func loadPresetLibrary() ([]Preset, error) {
	data, err := os.ReadFile(presetLibraryPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var lib presetLibrary
	if err := json.Unmarshal(data, &lib); err != nil {
		return nil, fmt.Errorf("failed to parse built-in library (%s): %w", presetLibraryPath, err)
	}
	if lib.Format != "" && lib.Format != presetLibraryFormat {
		return nil, fmt.Errorf("built-in library format=%q is not %q", lib.Format, presetLibraryFormat)
	}
	if lib.Version != 0 && lib.Version != presetFormatVersion {
		return nil, fmt.Errorf("built-in library version %d unsupported", lib.Version)
	}
	out := make([]Preset, 0, len(lib.Presets))
	for i := range lib.Presets {
		p := lib.Presets[i]
		if err := validatePreset(&p); err != nil {
			return nil, fmt.Errorf("built-in library entry %d (%s): %w", i+1, p.Name, err)
		}
		p.Builtin = true
		out = append(out, p)
	}
	return out, nil
}

func presetFileName(name string) string {
	bad := func(r rune) bool {
		return r == '/' || r == '\\' || r == 0 || r < 0x20 || r == ':'
	}
	s := strings.Map(func(r rune) rune {
		if bad(r) {
			return '_'
		}
		return r
	}, strings.TrimSpace(name))
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "preset"
	}
	return s + ".json"
}

func loadUserPresets() ([]Preset, error) {
	entries, err := os.ReadDir(userPresetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Preset, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(userPresetDir, e.Name()))
		if err != nil {
			continue
		}
		var p Preset
		if err := json.Unmarshal(data, &p); err != nil {
			continue
		}
		if err := validatePreset(&p); err != nil {
			continue
		}
		p.File = e.Name()
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func saveUserPreset(p Preset) error {
	if err := validatePreset(&p); err != nil {
		return err
	}
	p.Builtin = false
	p.File = ""
	if err := os.MkdirAll(userPresetDir, 0o755); err != nil {
		return fmt.Errorf("failed to create preset directory: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(userPresetDir, presetFileName(p.Name))
	tmp, err := os.CreateTemp(userPresetDir, ".tmp-preset-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, final)
}

func deleteUserPreset(name string) error {
	list, err := loadUserPresets()
	if err != nil {
		return err
	}
	for _, p := range list {
		if p.Name == name {
			return os.Remove(filepath.Join(userPresetDir, p.File))
		}
	}
	return fmt.Errorf("user preset %q not found", name)
}

func renameUserPreset(from, to string) (string, error) {
	from = strings.TrimSpace(from)
	to = strings.TrimSpace(to)
	if to == "" {
		return "", fmt.Errorf("new name must not be empty")
	}
	if from == to {
		return to, nil
	}
	list, err := loadUserPresets()
	if err != nil {
		return "", err
	}
	var found *Preset
	for i := range list {
		if list[i].Name == from {
			found = &list[i]
			break
		}
	}
	if found == nil {

		if lib, lerr := loadPresetLibrary(); lerr == nil {
			for _, p := range lib {
				if p.Name == from {
					return "", fmt.Errorf("%q is a built-in preset and cannot be renamed; use 'Save as effect' to make a copy", from)
				}
			}
		}
		return "", fmt.Errorf("user preset %q not found", from)
	}

	if lib, lerr := loadPresetLibrary(); lerr == nil {
		for _, p := range lib {
			if p.Name == to {
				return "", fmt.Errorf("new name %q collides with a built-in preset name, choose another", to)
			}
		}
	}

	for _, p := range list {
		if p.Name == to {
			return "", fmt.Errorf("a user preset named %q already exists", to)
		}
	}
	renamed := *found
	renamed.Name = to
	if err := saveUserPreset(renamed); err != nil {
		return "", err
	}
	if err := os.Remove(filepath.Join(userPresetDir, found.File)); err != nil {

		log.Printf("preset: failed to delete old file after rename (new name already in effect): %v", err)
	}
	return to, nil
}

func findPreset(name string) (Preset, error) {
	if list, err := loadUserPresets(); err == nil {
		for _, p := range list {
			if p.Name == name {
				return p, nil
			}
		}
	}
	if list, err := loadPresetLibrary(); err == nil {
		for _, p := range list {
			if p.Name == name {
				return p, nil
			}
		}
	}
	return Preset{}, fmt.Errorf("preset %q not found", name)
}

func applyPreset(p Preset) error {
	return applyChain(p.Chain, p.PreampDB, chainGainDB)
}
