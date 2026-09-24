package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// setupVariants builds two distinguishable fixture ISOs (different prefix
// bytes so the two variants' output can never be confused for each other)
// and points variantFull/variantNoWifi at them, restoring the previous
// values on cleanup.
func setupVariants(t *testing.T) {
	t.Helper()

	fullPath, fullOffset, _ := buildFixtureISO(t, 100, 100)
	noWifiPath, noWifiOffset, _ := buildFixtureISO(t, 50, 50)

	prevFull, prevNoWifi := variantFull, variantNoWifi
	variantFull = variant{path: fullPath, isoName: "full.iso", configOffset: fullOffset, isoSize: int64(100 + configSlotSize + 100)}
	variantNoWifi = variant{path: noWifiPath, isoName: "no-wifi.iso", configOffset: noWifiOffset, isoSize: int64(50 + configSlotSize + 50)}
	t.Cleanup(func() { variantFull, variantNoWifi = prevFull, prevNoWifi })
}

func postISO(t *testing.T, body string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/iso", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	handleGenerateISO(w, req)
	return w.Result()
}

// patchedConfig extracts and unmarshals the config slot from a successful
// /api/iso response, using whichever variant is currently selected.
func patchedConfig(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	v := variantFull
	if len(body) == int(variantNoWifi.isoSize) {
		v = variantNoWifi
	}
	slot := body[v.configOffset : v.configOffset+configSlotSize]
	var cfg map[string]any
	if err := json.Unmarshal(bytes.TrimRight(slot, " \n"), &cfg); err != nil {
		t.Fatalf("unmarshal patched config: %v", err)
	}
	return cfg
}

func TestGenerateISOUsesFullVariantWhenWifiProvided(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test","wifi":{"ssid":"office","password":"hunter2"}}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="full.iso"` {
		t.Fatalf("Content-Disposition = %q, want full.iso", got)
	}
	body, _ := io.ReadAll(res.Body)
	if int64(len(body)) != variantFull.isoSize {
		t.Fatalf("body length %d, want %d (variantFull size)", len(body), variantFull.isoSize)
	}
}

func TestGenerateISOUsesNoWifiVariantWhenWifiAbsent(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test"}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="no-wifi.iso"` {
		t.Fatalf("Content-Disposition = %q, want no-wifi.iso", got)
	}
	body, _ := io.ReadAll(res.Body)
	if int64(len(body)) != variantNoWifi.isoSize {
		t.Fatalf("body length %d, want %d (variantNoWifi size)", len(body), variantNoWifi.isoSize)
	}
}

func TestGenerateISOUsesNoWifiVariantWithStaticIPOnly(t *testing.T) {
	setupVariants(t)

	// staticIp alone, no wifi, must still select the no-wifi variant -- the
	// two settings are independent.
	res := postISO(t, `{"authKey":"tskey-test","staticIp":{"address":"192.168.1.50/24","gateway":"192.168.1.1"}}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	if got := res.Header.Get("Content-Disposition"); got != `attachment; filename="no-wifi.iso"` {
		t.Fatalf("Content-Disposition = %q, want no-wifi.iso", got)
	}

	var cfg map[string]any
	body, _ := io.ReadAll(res.Body)
	slot := body[variantNoWifi.configOffset : variantNoWifi.configOffset+configSlotSize]
	if err := json.Unmarshal(bytes.TrimRight(slot, " \n"), &cfg); err != nil {
		t.Fatalf("unmarshal patched config: %v", err)
	}
	if _, ok := cfg["staticIp"]; !ok {
		t.Fatal("patched config missing staticIp")
	}
}

func TestGenerateISODerivesHostnameFromClientName(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test","clientName":"Acme Corp! & Sons"}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	cfg := patchedConfig(t, res)
	if got := cfg["hostname"]; got != "dropbox-acme-corp-sons" {
		t.Fatalf("hostname = %v, want dropbox-acme-corp-sons", got)
	}
}

func TestGenerateISOFilenameNeedsBothClientNameAndKillDate(t *testing.T) {
	setupVariants(t)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"both present", `{"authKey":"tskey-test","clientName":"Acme","killDate":"2026-12-31"}`, `attachment; filename="dropbox-Acme-2026-12-31.iso"`},
		{"clientName only", `{"authKey":"tskey-test","clientName":"Acme"}`, `attachment; filename="no-wifi.iso"`},
		{"killDate only", `{"authKey":"tskey-test","killDate":"2026-12-31"}`, `attachment; filename="no-wifi.iso"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := postISO(t, c.body)
			if res.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				t.Fatalf("status %d: %s", res.StatusCode, body)
			}
			if got := res.Header.Get("Content-Disposition"); got != c.want {
				t.Fatalf("Content-Disposition = %q, want %q", got, c.want)
			}
		})
	}
}

func TestGenerateISOCallerCannotSetHostnameDirectly(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test","hostname":"attacker-controlled"}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	cfg := patchedConfig(t, res)
	if _, present := cfg["hostname"]; present {
		t.Fatalf("hostname must only be set from clientName, got %v", cfg["hostname"])
	}
}

func TestGenerateISOScopeSplitsByType(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test","scope":[
		{"type":"internal","value":"10.0.0.0/8"},
		{"type":"external","value":"203.0.113.0/24"},
		{"type":"internal","value":"172.16.0.0/12"}
	]}`)
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d: %s", res.StatusCode, body)
	}
	cfg := patchedConfig(t, res)
	scope, ok := cfg["scope"].([]any)
	if !ok || len(scope) != 3 {
		t.Fatalf("scope = %v, want 3 entries", cfg["scope"])
	}
}

func TestGenerateISORejectsInvalidScopeType(t *testing.T) {
	setupVariants(t)

	res := postISO(t, `{"authKey":"tskey-test","scope":[{"type":"dmz","value":"10.0.0.0/8"}]}`)
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("status %d, want 400: %s", res.StatusCode, body)
	}
}

func TestSanitizeHostnameLabel(t *testing.T) {
	cases := map[string]string{
		"Acme Corp":              "acme-corp",
		"Acme Corp! & Sons":      "acme-corp-sons",
		"---leading-trailing---": "leading-trailing",
		"already-safe-123":       "already-safe-123",
	}
	for input, want := range cases {
		if got := sanitizeHostnameLabel(input); got != want {
			t.Errorf("sanitizeHostnameLabel(%q) = %q, want %q", input, got, want)
		}
	}
}
