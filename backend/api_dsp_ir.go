// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	irMu      sync.Mutex
	irBank    [2][]int32
	irMeta    *IRInfo
	irName    string
	irDirty   bool
	irLoading bool
)

var irDir = envOr("ZYBO_IR_DIR", "/var/lib/zybo-audio/irs")

func irNameSafe(name string) string {
	n := strings.TrimSpace(name)
	n = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			return '_'
		}
		return r
	}, n)
	n = strings.TrimLeft(n, ".")
	if n == "" {
		n = "imported"
	}
	lower := strings.ToLower(n)

	if !strings.HasSuffix(lower, ".irs") && !strings.HasSuffix(lower, ".wav") &&
		!strings.HasSuffix(lower, ".vdc") {
		n += ".irs"
	}
	return n
}

func loadIRInto(body []byte, name string) (IRInfo, error) {
	banks, info, err := loadIR(body)
	if err != nil {
		return IRInfo{}, err
	}

	irMu.Lock()
	defer irMu.Unlock()
	for c := 0; c < 2; c++ {
		irBank[c] = append(irBank[c][:0], banks[c]...)
	}
	info.Taps = len(banks[0])
	irMeta = &info
	irName = irNameSafe(name)
	irDirty = true
	return info, nil
}

func irPlanCoefs() []int32 {
	irMu.Lock()
	defer irMu.Unlock()
	if !irDirty || len(irBank[0]) == 0 {
		return nil
	}
	out := make([]int32, 0, 2*len(irBank[0]))
	out = append(out, irBank[0]...)
	if len(irBank[1]) > 0 {
		out = append(out, irBank[1]...)
	}
	return out
}

func irClearDirty() {
	irMu.Lock()
	irDirty = false
	irMu.Unlock()
}

func irView() map[string]any {
	irMu.Lock()
	defer irMu.Unlock()
	v := map[string]any{
		"available": firAvailable(),
		"name":      irName,
		"dirty":     irDirty,
		"max_taps":  firTaps,

		"in_chain": currentFIR != nil,
	}
	if irMeta != nil {
		v["info"] = *irMeta
		v["loaded_taps"] = len(irBank[0])
	}
	return v
}

func saveIRFile(name string, body []byte) error {
	if err := os.MkdirAll(irDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(irDir, irNameSafe(name))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func irUnload() {
	irMu.Lock()
	irBank = [2][]int32{}
	irMeta = nil
	irName = ""
	irDirty = false
	irMu.Unlock()
	currentFIR = nil
	dropChainItem("convolver")
}

func applyChainForIR() error {
	irMu.Lock()
	irLoading = true
	irMu.Unlock()
	if err := applyChain(currentUserChain, currentPreampDB, chainGainDB); err != nil {
		irMu.Lock()
		irLoading = false
		irMu.Unlock()
		return fmt.Errorf("Loading IR (bypass stage) failed: %w", err)
	}
	irMu.Lock()
	irLoading = false
	irMu.Unlock()
	if err := applyChain(currentUserChain, currentPreampDB, chainGainDB); err != nil {
		return fmt.Errorf("Loading IR (apply stage) failed: %w", err)
	}
	return nil
}

func handleDSPIR(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, irView())
	case http.MethodPost:

		if r.URL.Query().Get("off") != "" {
			irUnload()
			if err := applyChain(currentUserChain, currentPreampDB, chainGainDB); err != nil {
				httpError(w, http.StatusInternalServerError, "Re-deploy after removing convolution failed: "+err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "on": false})
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
		if err != nil {
			httpError(w, http.StatusBadRequest, "Failed to read request body: "+err.Error())
			return
		}
		if len(raw) == 0 {
			httpError(w, http.StatusBadRequest, "Empty request body (POST body should be a WAV file)")
			return
		}
		if !firAvailable() {
			httpError(w, http.StatusConflict,
				"This bitstream has no FIR slot (CAP1 bit5): convolution needs a 0.2 bitstream")
			return
		}
		name := r.URL.Query().Get("name")
		info, err := loadIRInto(raw, name)
		if err != nil {
			httpError(w, http.StatusBadRequest, "This IR cannot be read: "+err.Error())
			return
		}
		warn := ""
		if err := saveIRFile(name, raw); err != nil {
			warn = "IR was not saved to disk (must be reloaded after reboot): " + err.Error()
		}

		ensureChainItem("convolver", irNameSafe(name))
		if err := applyChainForIR(); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp := map[string]any{"ok": true, "name": irNameSafe(name), "info": info}
		if warn != "" {
			resp["warning"] = warn
		}
		writeJSON(w, resp)
	default:
		httpError(w, http.StatusMethodNotAllowed, "Only GET / POST supported")
	}
}

func irLoadedName() string {
	irMu.Lock()
	defer irMu.Unlock()
	return irName
}

func loadIRNamed(name string) error {
	b, err := os.ReadFile(filepath.Join(irDir, irNameSafe(name)))
	if err != nil {
		return fmt.Errorf("IR %q not found (install it first with POST /api/dsp/ir): %w", name, err)
	}
	_, err = loadIRInto(b, name)
	return err
}

var ddcDir = envOr("ZYBO_DDC_DIR", "/var/lib/zybo-audio/ddc")

func saveDDCFile(name string, body []byte) error {
	if err := os.MkdirAll(ddcDir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(ddcDir, irNameSafe(name))
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func loadDDCNamed(name string, sections int, native bool) error {
	b, err := os.ReadFile(filepath.Join(ddcDir, irNameSafe(name)))
	if err != nil {
		return fmt.Errorf("DDC %q not found (install it first with POST /api/dsp/ddc): %w", name, err)
	}
	if native {
		_, err = setDDCFromVDC(b, name)
		return err
	}
	_, _, _, err = fitDDCFromVDC(b, name, sections)
	return err
}

func ensureChainItem(typ, name string) {

	want := typ
	for i := range currentUserChain {
		if k, ok := chainEffectType[normalizeChainType(currentUserChain[i].Type)]; ok {
			if k == want || (want == "convolver" && k == "fir") {
				currentUserChain[i].Enabled = true
				currentUserChain[i].Name = name
				return
			}
		}
	}
	currentUserChain = append(currentUserChain, ChainItem{
		Type: typ, Enabled: true, Name: name,
	})
}

func setChainItemParam(typ, key string, val float64) {
	for i := range currentUserChain {
		if k, ok := chainEffectType[normalizeChainType(currentUserChain[i].Type)]; ok {
			if k == typ || (typ == "convolver" && k == "fir") {
				if currentUserChain[i].Params == nil {
					currentUserChain[i].Params = map[string]float64{}
				}
				currentUserChain[i].Params[key] = val
				return
			}
		}
	}
	currentUserChain = append(currentUserChain, ChainItem{
		Type: typ, Enabled: true, Params: map[string]float64{key: val},
	})
}

func chainItemParam(typ, key string, def float64) float64 {
	for i := range currentUserChain {
		if k, ok := chainEffectType[normalizeChainType(currentUserChain[i].Type)]; ok {
			if k == typ || (typ == "convolver" && k == "fir") {
				if v, ok := currentUserChain[i].Params[key]; ok {
					return v
				}
			}
		}
	}
	return def
}

func dropChainItem(typ string) {
	out := currentUserChain[:0]
	for _, it := range currentUserChain {
		if k, ok := chainEffectType[normalizeChainType(it.Type)]; ok &&
			(k == typ || (typ == "convolver" && k == "fir")) {
			continue
		}
		out = append(out, it)
	}
	currentUserChain = out
}
