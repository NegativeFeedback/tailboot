package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildFixtureISO writes a fake ISO: prefix bytes, the placeholder slot at
// offset, then suffix bytes. It returns the offset and the full expected
// unpatched content, for comparing "bytes outside the slot" afterward.
func buildFixtureISO(t *testing.T, prefixLen, suffixLen int) (path string, offset int64, original []byte) {
	t.Helper()

	prefix := bytes.Repeat([]byte{0xAA}, prefixLen)
	suffix := bytes.Repeat([]byte{0xBB}, suffixLen)
	original = append(append(append([]byte{}, prefix...), configPlaceholder()...), suffix...)

	path = filepath.Join(t.TempDir(), "fixture.iso")
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path, int64(prefixLen), original
}

func patch(t *testing.T, path string, offset int64, configJSON []byte) ([]byte, error) {
	t.Helper()
	f, err := validateAndOpenISO(path, offset, configJSON)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := writePatchedISO(&out, f, offset, configJSON); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func TestPatchRoundTrip(t *testing.T) {
	path, offset, original := buildFixtureISO(t, 5000, 7000)
	configJSON := []byte(`{"authKey":"tskey-auth-test"}`)

	out, err := patch(t, path, offset, configJSON)
	if err != nil {
		t.Fatalf("patch: %v", err)
	}

	if len(out) != len(original) {
		t.Fatalf("output length %d, want %d (total size must never change)", len(out), len(original))
	}
	if !bytes.Equal(out[:offset], original[:offset]) {
		t.Fatal("bytes before the slot were modified")
	}
	if !bytes.Equal(out[offset+configSlotSize:], original[offset+configSlotSize:]) {
		t.Fatal("bytes after the slot were modified")
	}

	slot := out[offset : offset+configSlotSize]
	if !bytes.HasPrefix(slot, configJSON) {
		t.Fatalf("slot does not start with the config JSON: %q", slot[:len(configJSON)])
	}
	if slot[configSlotSize-1] != '\n' {
		t.Fatal("slot's last byte must be a newline")
	}
	for _, b := range slot[len(configJSON) : configSlotSize-1] {
		if b != ' ' {
			t.Fatal("slot padding after the JSON must be spaces")
		}
	}
}

func TestPatchExactCapacitySucceeds(t *testing.T) {
	path, offset, _ := buildFixtureISO(t, 10, 10)
	configJSON := bytes.Repeat([]byte{'x'}, configCapacity)

	if _, err := patch(t, path, offset, configJSON); err != nil {
		t.Fatalf("exact-capacity config should succeed, got: %v", err)
	}
}

func TestPatchOverCapacityRejected(t *testing.T) {
	path, offset, _ := buildFixtureISO(t, 10, 10)
	configJSON := bytes.Repeat([]byte{'x'}, configCapacity+1)

	if _, err := patch(t, path, offset, configJSON); err != ErrConfigTooLarge {
		t.Fatalf("expected ErrConfigTooLarge, got: %v", err)
	}
}

func TestPatchMarkerMismatchRejected(t *testing.T) {
	path, offset, original := buildFixtureISO(t, 10, 10)
	// Corrupt one byte of the placeholder to simulate an incompatible/already
	// patched ISO.
	corrupted := append([]byte{}, original...)
	corrupted[offset] = 'X'
	if err := os.WriteFile(path, corrupted, 0o600); err != nil {
		t.Fatalf("write corrupted fixture: %v", err)
	}

	if _, err := patch(t, path, offset, []byte(`{}`)); err != ErrIncompatibleISO {
		t.Fatalf("expected ErrIncompatibleISO, got: %v", err)
	}
}

func TestPatchDoesNotWriteOnValidationFailure(t *testing.T) {
	path, offset, _ := buildFixtureISO(t, 10, 10)

	if _, err := validateAndOpenISO(path, offset, bytes.Repeat([]byte{'x'}, configCapacity+1)); err != ErrConfigTooLarge {
		t.Fatalf("expected ErrConfigTooLarge, got: %v", err)
	}
	// validateAndOpenISO must not have left the source file altered.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if !bytes.Equal(got[offset:offset+configSlotSize], configPlaceholder()) {
		t.Fatal("source ISO was modified despite a validation failure")
	}
}
