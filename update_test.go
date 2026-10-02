package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
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
		{"v0.3.0-rc1", "v0.3.0", true},
		{"v0.3.1-0.20261002093000-abcdef123456", "v0.3.0", false},
		{"v0.3.1-0.20261002093000-abcdef123456+dirty", "v0.3.1", true},
		{"v0.3.0", "v0.3.0-rc2", false},
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

	// Asset not in checksums: refuse rather than install unverified
	if err := verifyChecksum(data, "unknown_file.tar.gz", checksums); err == nil {
		t.Errorf("expected an error for a missing checksum entry")
	}

	// Binary-mode entries from sha256sum -b
	binaryMode := fmt.Sprintf("%s *sshx_v0.3.0_linux_amd64.tar.gz\n", hash)
	if err := verifyChecksum(data, "sshx_v0.3.0_linux_amd64.tar.gz", binaryMode); err != nil {
		t.Errorf("expected binary-mode checksum entry to verify, got: %v", err)
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

// useTempCacheDir points os.UserCacheDir at a temporary directory on every platform.
func useTempCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("LocalAppData", dir)
	return dir
}

func TestCheckLatestReleaseCachedLocal(t *testing.T) {
	useTempCacheDir(t)
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(sshDir, legacyUpdateCacheFile)
	if err := os.WriteFile(legacy, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}

	c := updateCache{
		CheckedAt:     time.Now().Unix(),
		LatestVersion: "v0.9.0",
	}
	cData, _ := json.Marshal(c)
	if err := AtomicWrite(updateCachePath(tmpDir), cData, 0600); err != nil {
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
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Errorf("expected the legacy cache in ~/.ssh to be removed")
	}
}

func TestUpdateCheckDisabled(t *testing.T) {
	t.Setenv(noUpdateCheckEnv, "")
	if updateCheckDisabled("v1.0.0") {
		t.Errorf("expected update checks to be enabled by default")
	}
	if !updateCheckDisabled("dev") {
		t.Errorf("expected development builds to skip the background check")
	}
	t.Setenv(noUpdateCheckEnv, "1")
	if !updateCheckDisabled("v1.0.0") {
		t.Errorf("expected %s to disable update checks", noUpdateCheckEnv)
	}
}

func TestReadLimited(t *testing.T) {
	if _, err := readLimited(bytes.NewReader(make([]byte, 11)), 10); err == nil {
		t.Errorf("expected an error for oversized input")
	}
	if data, err := readLimited(bytes.NewReader(make([]byte, 10)), 10); err != nil || len(data) != 10 {
		t.Errorf("expected input at the limit to be read, got %d bytes (err %v)", len(data), err)
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sshx")
	if err := os.WriteFile(exe, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(exe, []byte("new"), "linux"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "new" { //nolint:gosec // test file read
		t.Errorf("expected new binary, got %q", data)
	}

	if err := replaceExecutable(exe, []byte("newer"), "windows"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data, _ := os.ReadFile(exe + ".old"); string(data) != "new" { //nolint:gosec // test file read
		t.Errorf("expected the previous binary to be kept as .old, got %q", data)
	}
}

func TestReplaceExecutableRestoresOnWindowsFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sshx.exe")
	if err := os.WriteFile(exe, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	renameFile = func(from, to string) error {
		if to == exe && filepath.Base(from) != "sshx.exe.old" {
			return errors.New("simulated failure")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { renameFile = os.Rename })

	if err := replaceExecutable(exe, []byte("new"), "windows"); err == nil {
		t.Fatalf("expected the simulated failure to be reported")
	}
	if data, err := os.ReadFile(exe); err != nil || string(data) != "old" { //nolint:gosec // test file read
		t.Errorf("expected the original binary to be restored, got %q (err %v)", data, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected temporary files to be cleaned up, got %v", entries)
	}
}

func TestApplyBuildInfo(t *testing.T) {
	saved := [3]string{Version, Commit, BuildDate}
	t.Cleanup(func() { Version, Commit, BuildDate = saved[0], saved[1], saved[2] })

	Version, Commit, BuildDate = "dev", "none", "unknown"
	applyBuildInfo(&debug.BuildInfo{
		Main: debug.Module{Version: "v1.2.3"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.time", Value: "2026-10-02T09:00:00Z"},
		},
	})
	if Version != "v1.2.3" || Commit != "0123456" || BuildDate != "2026-10-02T09:00:00Z" {
		t.Errorf("unexpected build info: %s %s %s", Version, Commit, BuildDate)
	}

	Version = "v9.9.9"
	applyBuildInfo(&debug.BuildInfo{Main: debug.Module{Version: "(devel)"}})
	if Version != "v9.9.9" {
		t.Errorf("expected -ldflags versions to take precedence, got %s", Version)
	}
}
