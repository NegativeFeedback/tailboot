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
