package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsNewerVersion(t *testing.T) {
	tests := []struct {
		current  string
		latest   string
		expected bool
	}{
		{"v0.1.0", "v0.2.0", true},
		{"v0.2.0", "v0.3.0", true},
		{"v0.3.0", "v0.3.1", true},
		{"v0.3.0", "v1.0.0", true},
		{"v0.3.0", "v0.3.0", false},
		{"v0.3.1", "v0.3.0", false},
		{"v1.0.0", "v0.9.9", false},
		{"dev", "v0.3.0", true},
		{"none", "v0.3.0", true},
		{"", "v0.3.0", true},
		{"v0.3.0", "", false},
		{"v0.3.0-rc1", "v0.3.1", true},
	}

	for _, tc := range tests {
		got := isNewerVersion(tc.current, tc.latest)
		if got != tc.expected {
			t.Errorf("isNewerVersion(%q, %q) = %v; expected %v", tc.current, tc.latest, got, tc.expected)
		}
	}
}

func TestParseSemVerParts(t *testing.T) {
	tests := []struct {
		input    string
		expected [3]int
	}{
		{"1.2.3", [3]int{1, 2, 3}},
		{"0.3.0", [3]int{0, 3, 0}},
		{"0.3.0-beta.1", [3]int{0, 3, 0}},
		{"2", [3]int{2, 0, 0}},
		{"", [3]int{0, 0, 0}},
	}

	for _, tc := range tests {
		got := parseSemVerParts(tc.input)
		if got != tc.expected {
			t.Errorf("parseSemVerParts(%q) = %v; expected %v", tc.input, got, tc.expected)
		}
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("binary-test-content-12345")
	hash := fmt.Sprintf("%x", sha256.Sum256(data))

	checksums := fmt.Sprintf("%s  sshx_v0.3.0_linux_amd64.tar.gz\n11223344  other.tar.gz\n", hash)

	// Valid checksum
	if err := verifyChecksum(data, "sshx_v0.3.0_linux_amd64.tar.gz", checksums); err != nil {
		t.Errorf("expected checksum to verify, got: %v", err)
	}

	// Mismatched checksum
	corrupted := []byte("different-data")
	if err := verifyChecksum(corrupted, "sshx_v0.3.0_linux_amd64.tar.gz", checksums); err == nil {
		t.Errorf("expected checksum mismatch error, got nil")
	}

	// Asset not in checksums (skipped)
	if err := verifyChecksum(data, "unknown_file.tar.gz", checksums); err != nil {
		t.Errorf("expected nil error for missing checksum entry, got: %v", err)
	}
}

func TestExtractBinaryFromTarGz(t *testing.T) {
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)

	content := []byte("#!/bin/sh\necho hello\n")
	hdr := &tar.Header{
		Name:     "dist/sshx_v0.3.0_linux_amd64/sshx",
		Mode:     0755,
		Size:     int64(len(content)),
		Typeflag: tar.TypeReg,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_ = gzw.Close()

	extracted, err := extractBinaryFromTarGz(buf.Bytes(), "sshx")
	if err != nil {
		t.Fatalf("unexpected extract error: %v", err)
	}
	if string(extracted) != string(content) {
		t.Errorf("expected %q, got %q", string(content), string(extracted))
	}

	// Not found
	_, err = extractBinaryFromTarGz(buf.Bytes(), "nonexistent")
	if err == nil {
		t.Errorf("expected error for nonexistent binary, got nil")
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	content := []byte("windows-binary-mock")
	w, err := zw.Create("sshx_v0.3.0_windows_amd64/sshx.exe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(content); err != nil {
		t.Fatal(err)
	}
	_ = zw.Close()

	extracted, err := extractBinaryFromZip(buf.Bytes(), "sshx.exe")
	if err != nil {
		t.Fatalf("unexpected extract error: %v", err)
	}
	if string(extracted) != string(content) {
		t.Errorf("expected %q, got %q", string(content), string(extracted))
	}
}

func TestCheckLatestReleaseCachedLocal(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	cacheFile := filepath.Join(sshDir, updateCacheFile)
	c := updateCache{
		CheckedAt:     time.Now().Unix(),
		LatestVersion: "v0.9.0",
	}
	cData, _ := json.Marshal(c)
	if err := os.WriteFile(cacheFile, cData, 0600); err != nil {
		t.Fatal(err)
	}

	latest, newer, err := CheckLatestReleaseCached("v0.3.0", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if latest != "v0.9.0" {
		t.Errorf("expected cached version v0.9.0, got %q", latest)
	}
	if !newer {
		t.Errorf("expected newer=true for v0.3.0 vs v0.9.0")
	}
}
