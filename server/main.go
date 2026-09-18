package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
)

//go:embed static/index.html
var staticFiles embed.FS

// Baked in at image build time via
// -ldflags "-X main.isoName=... -X main.release=... -X main.configOffsetStr=..."
// so the binary always matches the one base ISO packaged alongside it.
var (
	isoName         = "tailboot.iso"
	release         = "dev"
	configOffsetStr = "0"
)

// Overridable in tests; fixed in production.
var basePath = "/data/base.iso"

type wifiConfig struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type staticIPConfig struct {
	Address string   `json:"address"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns,omitempty"`
}

type tailbootConfig struct {
	AuthKey  string          `json:"authKey"`
	Wifi     *wifiConfig     `json:"wifi,omitempty"`
	StaticIP *staticIPConfig `json:"staticIp,omitempty"`
}

func (c tailbootConfig) validate() error {
	if c.AuthKey == "" {
		return errors.New("authKey is required")
	}
	if c.Wifi != nil && (c.Wifi.SSID == "" || c.Wifi.Password == "") {
		return errors.New("wifi requires both ssid and password")
	}
	if c.StaticIP != nil && (c.StaticIP.Address == "" || c.StaticIP.Gateway == "") {
		return errors.New("staticIp requires both address and gateway")
	}
	return nil
}

var (
	configOffset int64
	isoSize      int64
)

func main() {
	offset, err := strconv.ParseInt(configOffsetStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid baked-in config offset %q: %v", configOffsetStr, err)
	}
	configOffset = offset

	info, err := os.Stat(basePath)
	if err != nil {
		log.Fatalf("base ISO not found at %s: %v", basePath, err)
	}
	isoSize = info.Size()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("POST /api/iso", handleGenerateISO)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("tailboot server listening on %s (release %s, iso %s)", addr, release, isoName)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := staticFiles.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(data)
}

func handleGenerateISO(w http.ResponseWriter, r *http.Request) {
	// Config bodies are a few hundred bytes at most (the ISO slot itself caps
	// at 4095 bytes); refuse anything wildly larger up front.
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)

	var cfg tailbootConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if err := cfg.validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Re-marshal into a canonical form rather than forwarding the raw body:
	// this drops unexpected extra fields and any attacker-controlled
	// formatting/whitespace that would otherwise eat into the 4095-byte slot
	// for no reason.
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		http.Error(w, "failed to encode configuration", http.StatusInternalServerError)
		return
	}

	f, err := validateAndOpenISO(basePath, configOffset, configJSON)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrConfigTooLarge) || errors.Is(err, ErrIncompatibleISO) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", isoName))
	w.Header().Set("Content-Length", strconv.FormatInt(isoSize, 10))

	if err := writePatchedISO(w, f, configOffset, configJSON); err != nil {
		// The response is already committed at this point; nothing more to
		// do but log for operators. Never log cfg/configJSON here -- it
		// carries the caller's Tailscale auth key and Wi-Fi password.
		log.Printf("iso stream error: %v", err)
	}
}
