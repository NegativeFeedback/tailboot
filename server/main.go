package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

//go:embed static/index.html
var staticFiles embed.FS

// Baked in at image build time via
// -ldflags "-X main.isoName=... -X main.isoNameNoWifi=... -X main.release=...
//
//	-X main.configOffsetStr=... -X main.configOffsetNoWifiStr=..."
//
// so the binary always matches the two base ISOs packaged alongside it.
var (
	isoName               = "tailboot.iso"
	isoNameNoWifi         = "tailboot-no-wifi.iso"
	release               = "dev"
	configOffsetStr       = "0"
	configOffsetNoWifiStr = "0"
)

// Overridable in tests; fixed in production.
var (
	basePathFull   = "/data/base.iso"
	basePathNoWifi = "/data/base-nowifi.iso"
)

// variant pairs a base ISO on disk with the metadata needed to patch and
// serve it. The full variant carries every wireless chipset's firmware; the
// no-wifi variant strips it entirely for a meaningfully smaller download.
// handleGenerateISO picks between them based on whether the request
// includes Wi-Fi credentials -- there's no reason to ship wireless firmware
// to a request that can't use it.
type variant struct {
	path         string
	isoName      string
	configOffset int64
	isoSize      int64
}

var (
	variantFull   variant
	variantNoWifi variant
)

func loadVariant(path, name, offsetStr string) variant {
	offset, err := strconv.ParseInt(offsetStr, 10, 64)
	if err != nil {
		log.Fatalf("invalid baked-in config offset %q for %s: %v", offsetStr, name, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		log.Fatalf("base ISO not found at %s: %v", path, err)
	}
	return variant{path: path, isoName: name, configOffset: offset, isoSize: info.Size()}
}

type wifiConfig struct {
	SSID     string `json:"ssid"`
	Password string `json:"password"`
}

type staticIPConfig struct {
	Address string   `json:"address"`
	Gateway string   `json:"gateway"`
	DNS     []string `json:"dns,omitempty"`
}

// scopeEntry.Name is a free-form scope-list name (e.g. Ghostwriter's
// ProjectScope.name -- "Internal", "CDE", "External IPs", anything).
// tailboot-engagement groups entries by the slugified name and writes one
// /root/scripts/<slug>.csv per distinct name on boot.
type scopeEntry struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type tailbootConfig struct {
	AuthKey    string          `json:"authKey"`
	Wifi       *wifiConfig     `json:"wifi,omitempty"`
	StaticIP   *staticIPConfig `json:"staticIp,omitempty"`
	ClientName string          `json:"clientName,omitempty"`
	Date       string          `json:"date,omitempty"`
	KillDate   string          `json:"killDate,omitempty"`
	Scope      []scopeEntry    `json:"scope,omitempty"`
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
	for _, s := range c.Scope {
		if s.Name == "" {
			return errors.New("scope entries require a name")
		}
		if s.Value == "" {
			return errors.New("scope entries require a value")
		}
		if slugifyScopeName(s.Name) == "" {
			return fmt.Errorf("scope entry name %q has no characters usable in a filename", s.Name)
		}
	}
	return nil
}

// devicePayload is what actually gets patched into the ISO: the request
// config plus a server-computed hostname. hostname is never accepted
// directly from callers -- it's always derived from clientName so the
// device name is predictable (dropbox-<client>) regardless of what a
// caller might otherwise put there.
type devicePayload struct {
	tailbootConfig
	Hostname string `json:"hostname,omitempty"`
}

var (
	hostnameUnsafe  = regexp.MustCompile(`[^a-z0-9-]+`)
	hostnameHyphens = regexp.MustCompile(`-{2,}`)
	filenameUnsafe  = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

// sanitizeHostnameLabel makes s safe as an RFC 1123 hostname label: lowercase
// alphanumeric and hyphens only, no leading/trailing hyphen, short enough to
// leave room for the "dropbox-" prefix within the 63-byte label limit.
func sanitizeHostnameLabel(s string) string {
	s = hostnameUnsafe.ReplaceAllString(strings.ToLower(s), "-")
	s = hostnameHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	const maxLen = 55
	if len(s) > maxLen {
		s = strings.Trim(s[:maxLen], "-")
	}
	return s
}

// sanitizeFilenameComponent makes s safe to sit between hyphens in a
// Content-Disposition filename: no path separators or other characters that
// would confuse a browser or shell.
func sanitizeFilenameComponent(s string) string {
	return strings.Trim(filenameUnsafe.ReplaceAllString(s, "-"), "-")
}

// slugifyScopeName turns a scope-list name into the base filename
// tailboot-engagement writes it under (<slug>.csv). Same rules as
// sanitizeHostnameLabel (lowercase alphanumeric and hyphens only, collapsed,
// trimmed) but no length cap -- filenames aren't limited to 63 bytes.
func slugifyScopeName(s string) string {
	s = hostnameUnsafe.ReplaceAllString(strings.ToLower(s), "-")
	s = hostnameHyphens.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func main() {
	variantFull = loadVariant(basePathFull, isoName, configOffsetStr)
	variantNoWifi = loadVariant(basePathNoWifi, isoNameNoWifi, configOffsetNoWifiStr)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("POST /api/iso", handleGenerateISO)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	addr := ":" + port
	log.Printf("tailboot server listening on %s (release %s, full %s, no-wifi %s)",
		addr, release, variantFull.isoName, variantNoWifi.isoName)
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

	v := variantFull
	if cfg.Wifi == nil {
		v = variantNoWifi
	}

	payload := devicePayload{tailbootConfig: cfg}
	if cfg.ClientName != "" {
		payload.Hostname = "dropbox-" + sanitizeHostnameLabel(cfg.ClientName)
	}

	// Re-marshal into a canonical form rather than forwarding the raw body:
	// this drops unexpected extra fields and any attacker-controlled
	// formatting/whitespace that would otherwise eat into the 4095-byte slot
	// for no reason.
	configJSON, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "failed to encode configuration", http.StatusInternalServerError)
		return
	}

	f, err := validateAndOpenISO(v.path, v.configOffset, configJSON)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrConfigTooLarge) || errors.Is(err, ErrIncompatibleISO) {
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}

	filename := v.isoName
	if cfg.ClientName != "" && cfg.KillDate != "" {
		filename = fmt.Sprintf("dropbox-%s-%s.iso",
			sanitizeFilenameComponent(cfg.ClientName), sanitizeFilenameComponent(cfg.KillDate))
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	w.Header().Set("Content-Length", strconv.FormatInt(v.isoSize, 10))

	if err := writePatchedISO(w, f, v.configOffset, configJSON); err != nil {
		// The response is already committed at this point; nothing more to
		// do but log for operators. Never log cfg/configJSON here -- it
		// carries the caller's Tailscale auth key and Wi-Fi password.
		log.Printf("iso stream error: %v", err)
	}
}
