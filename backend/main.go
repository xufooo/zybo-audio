// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

var (
	srcMgr *sourceManager
)

const appVersion = "0.3.0"

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("ZYBO Audio Web v%s starting...", appVersion)

	if err := dspOpen(); err != nil {
		log.Printf("WARNING: FPGA DSP not available: %v", err)
		log.Printf("  → EQ/DSP control disabled; audio pass-through only")
	} else {
		log.Printf("FPGA DSP registers mapped at 0x43C00000")
		defer dspClose()

		dspInit()

		restoreRuntimeState()
	}

	srcMgr = &sourceManager{}
	if err := srcMgr.init(); err != nil {
		log.Printf("Source manager init: %v", err)
	}

	currentSource = srcMgr.active

	hub := newWSHub()
	go hub.run()

	mux := http.NewServeMux()

	mux.HandleFunc("/api/status", handleStatus)
	mux.HandleFunc("/api/dsp/enable", handleDSPEnable)
	mux.HandleFunc("/api/dsp/bypass", handleDSPBypass)
	mux.HandleFunc("/api/dsp/eq/", handleDSPEQ)
	mux.HandleFunc("/api/dsp/preset", handleDSPPreset)
	mux.HandleFunc("/api/dsp/limiter", handleDSPLimiter)
	mux.HandleFunc("/api/dsp/loudness", handleDSPLoudness)
	mux.HandleFunc("/api/dsp/check", handleDSPCheck)
	mux.HandleFunc("/api/dsp/import", handleEQImport)

	mux.HandleFunc("/api/dsp/capabilities", handleDSPCapabilities)

	mux.HandleFunc("/api/dsp/budget", handleDSPBudget)
	mux.HandleFunc("/api/dsp/chain", handleDSPChain)
	mux.HandleFunc("/api/dsp/presets", handleDSPPresets)
	mux.HandleFunc("/api/dsp/presets/apply", handleDSPPresetApply)
	mux.HandleFunc("/api/dsp/presets/delete", handleDSPPresetDelete)
	mux.HandleFunc("/api/dsp/presets/rename", handleDSPPresetRename)

	mux.HandleFunc("/api/dsp/types", handleDSPTypes)
	mux.HandleFunc("/api/dsp/types/delete", handleDSPTypeDelete)
	mux.HandleFunc("/api/dsp/types/rename", handleDSPTypeRename)
	mux.HandleFunc("/api/dsp/presets/export", handleDSPPresetExport)
	mux.HandleFunc("/api/dsp/presets/import", handleDSPPresetImport)
	mux.HandleFunc("/api/dsp/eq/parse", handleDSPEQParse)
	mux.HandleFunc("/api/dsp/ir", handleDSPIR)
	mux.HandleFunc("/api/dsp/ddc", handleDSPDDC)
	mux.HandleFunc("/api/dsp/backup", handleDSPBackup)
	mux.HandleFunc("/api/dsp/restore", handleDSPRestore)
	mux.HandleFunc("/api/dsp/export", handleEQExport)
	mux.HandleFunc("/api/volume", handleVolume)
	mux.HandleFunc("/api/source/select", handleSourceSelect)
	mux.HandleFunc("/api/reboot", handleReboot)
	mux.HandleFunc("/api/shutdown", handleShutdown)

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWS(hub, w, r)
	})

	mux.Handle("/", http.FileServer(http.Dir("/var/www/zybo-audio")))

	go statusPusher(hub, srcMgr)

	port := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		port = ":" + p
	}

	server := &http.Server{
		Addr:    port,
		Handler: mux,
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		<-sig
		log.Println("Shutting down...")

		if err := flushPendingState(); err != nil {
			log.Printf("before exitpersist failed:%v", err)
		}
		server.Close()
	}()

	fmt.Printf(`
╔══════════════════════════════════════════╗
║   ZYBO Audio Web v%-8s           ║
║   Listening on %-22s ║
║   WebUI: http://zybo-audio.local%-5s ║
╚══════════════════════════════════════════╝
`, appVersion, port, port)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
	log.Println("Server stopped")
}
