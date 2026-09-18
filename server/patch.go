package main

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// configCapacity and the placeholder format mirror ../tailboot-iso-core.ts
// exactly: a fixed 4096-byte slot ("TAILBOOT_CONFIG_V1" padded with '~' to
// 4095 bytes, plus a trailing '\n') that a patched ISO always carries. A
// patch replaces those 4095 bytes with raw JSON, space-padded, keeping the
// final '\n' byte -- so the wire format is delimiter-free and every reader
// (jq at boot, in this case) just parses past the trailing whitespace.
const (
	configCapacity = 4095
	configSlotSize = configCapacity + 1
)

var (
	ErrConfigTooLarge  = errors.New("configuration exceeds the 4095-byte ISO slot")
	ErrIncompatibleISO = errors.New("this is not a compatible Tailboot ISO: the configuration slot does not match the release offset")
)

func configPlaceholder() []byte {
	slot := bytes.Repeat([]byte{'~'}, configSlotSize)
	copy(slot, []byte("TAILBOOT_CONFIG_V1"))
	slot[configSlotSize-1] = '\n'
	return slot
}

// validateAndOpenISO opens isoPath and checks, without writing anything,
// that configJSON fits the slot and that the existing bytes at configOffset
// match the known placeholder. The caller owns closing the returned file.
func validateAndOpenISO(isoPath string, configOffset int64, configJSON []byte) (*os.File, error) {
	if int64(len(configJSON)) > configCapacity {
		return nil, ErrConfigTooLarge
	}

	f, err := os.Open(isoPath)
	if err != nil {
		return nil, err
	}

	existing := make([]byte, configSlotSize)
	if _, err := f.ReadAt(existing, configOffset); err != nil {
		f.Close()
		return nil, err
	}
	if !bytes.Equal(existing, configPlaceholder()) {
		f.Close()
		return nil, ErrIncompatibleISO
	}

	return f, nil
}

// writePatchedISO streams f to w, replacing the configOffset..configOffset+
// configSlotSize range with configJSON (space-padded, trailing '\n'). It
// closes f. Bytes outside the slot are copied through unchanged, so the
// total output length always equals the input length.
func writePatchedISO(w io.Writer, f *os.File, configOffset int64, configJSON []byte) error {
	defer f.Close()

	slot := bytes.Repeat([]byte{' '}, configSlotSize)
	copy(slot, configJSON)
	slot[configSlotSize-1] = '\n'

	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.CopyN(w, f, configOffset); err != nil {
		return err
	}
	if _, err := w.Write(slot); err != nil {
		return err
	}
	if _, err := f.Seek(configOffset+configSlotSize, io.SeekStart); err != nil {
		return err
	}
	_, err := io.Copy(w, f)
	return err
}
