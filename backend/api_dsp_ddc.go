// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"io"
	"net/http"
	"strconv"
)

func handleDSPDDC(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, ddcView())
	case http.MethodPost:
		if r.URL.Query().Get("off") != "" {
			ddcClear()
			dropChainItem("ddc")
			if err := applyChain(currentUserChain, currentPreampDB, chainGainDB); err != nil {
				httpError(w, http.StatusInternalServerError, "Failed to redeploy after turning DDC off:"+err.Error())
				return
			}
			writeJSON(w, map[string]any{"ok": true, "on": false})
			return
		}
		raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			httpError(w, http.StatusBadRequest, "Failed to read request body:"+err.Error())
			return
		}
		if len(raw) == 0 {
			httpError(w, http.StatusBadRequest, "Empty request body (POST body should be .vdc text; use ?off=1 to turn off)")
			return
		}

		native := r.URL.Query().Get("sections") == ""
		name := r.URL.Query().Get("name")
		var n int
		var fc, dev float64
		var err2 error
		if native {
			n, err2 = setDDCFromVDC(raw, name)
		} else {
			nsec := 4
			if x, e := strconv.Atoi(r.URL.Query().Get("sections")); e == nil && x >= 1 {
				if x > hwMaxSections {
					x = hwMaxSections
				}
				nsec = x
			}
			n, fc, dev, err2 = fitDDCFromVDC(raw, name, nsec)
		}
		if err2 != nil {
			httpError(w, http.StatusBadRequest, "This DDC cannot be read:"+err2.Error())
			return
		}
		_ = dev

		warn := ""
		if err := saveDDCFile(r.URL.Query().Get("name"), raw); err != nil {
			warn = "DDC was not saved to disk (reinstall after reboot):" + err.Error()
		}

		ensureChainItem("ddc", ddcLoadedName())

		setChainItemParam("ddc", "sections", float64(n))
		natFlag := 0.0
		if native {
			natFlag = 1
		}
		setChainItemParam("ddc", "native", natFlag)
		ddcSnap := captureChainState()
		if err := applyChain(currentUserChain, currentPreampDB, chainGainDB); err != nil {
			ddcSnap.restore()
			ddcClear()
			httpError(w, http.StatusInternalServerError, "Failed to redeploy after loading DDC:"+err.Error())
			return
		}
		if warn != "" {
			writeJSON(w, map[string]any{"ok": true, "warning": warn, "view": ddcView()})
			return
		}
		v := ddcView()
		v["ok"] = true
		v["loaded"] = n
		v["fit_fc_hz"] = fc
		v["fit_max_dev_db"] = dev
		writeJSON(w, v)
	default:
		httpError(w, http.StatusMethodNotAllowed, "Only GET / POST supported")
	}
}
