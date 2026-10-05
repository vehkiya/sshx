package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
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
		{"v0.3.1+dirty", "v0.3.1", false}, // a modified local build of v0.3.1
		{"v0.3.1+dirty", "v0.3.2", true},
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
		{"0.3.1+dirty", [3]int{0, 3, 1}},
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

func TestTrustedSigningKeysAreValid(t *testing.T) {
	if len(trustedSigningKeys) == 0 {
		t.Fatal("expected at least one trusted signing key")
	}
	for _, k := range trustedSigningKeys {
		pub, err := base64.StdEncoding.DecodeString(k)
		if err != nil || len(pub) != ed25519.PublicKeySize {
			t.Errorf("trusted key %q is not a base64 Ed25519 public key (err %v, %d bytes)", k, err, len(pub))
		}
	}
}

// newSigningKey returns a fresh key pair with the public half base64-encoded, as in trustedSigningKeys.
func newSigningKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

// sign produces a checksums.txt.sig body the way the CD workflow does (base64 of the raw signature).
func sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
}

func TestVerifySignature(t *testing.T) {
	priv, pub := newSigningKey(t)
	otherPriv, otherPub := newSigningKey(t)
	data := []byte("abc  sshx_v1.0.0_linux_amd64.tar.gz\n")

	if err := verifySignature(data, sign(priv, data), []string{pub}); err != nil {
		t.Errorf("expected a valid signature to verify: %v", err)
	}
	if err := verifySignature(data, append(sign(priv, data), '\n'), []string{pub}); err != nil {
		t.Errorf("expected a trailing newline to be ignored: %v", err)
	}
	if err := verifySignature(data, sign(otherPriv, data), []string{pub, otherPub}); err != nil {
		t.Errorf("expected any trusted key to be accepted during rotation: %v", err)
	}
	if err := verifySignature(append(data, 'x'), sign(priv, data), []string{pub}); err == nil {
		t.Errorf("expected tampered data to fail")
	}
	if err := verifySignature(data, sign(otherPriv, data), []string{pub}); err == nil {
		t.Errorf("expected a signature from an untrusted key to fail")
	}
	if err := verifySignature(data, []byte("not-base64!"), []string{pub}); err == nil {
		t.Errorf("expected a malformed signature to fail")
	}
}

func TestFetchVerifiedBinary(t *testing.T) {
	priv, pub := newSigningKey(t)
	otherPriv, _ := newSigningKey(t)
	binary := []byte("new-sshx-binary")

	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			name := "sshx_v1.0.0_" + goos + "_amd64"
			archive := makeTestArchive(t, goos, name, binary)
			archiveName := name + ".tar.gz"
			if goos == "windows" {
				archiveName = name + ".zip"
			}
			checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName))

			tests := []struct {
				name      string
				archive   []byte
				signature []byte
				noSig     bool
				wantErr   string
			}{
				{name: "valid", archive: archive, signature: sign(priv, checksums)},
				{name: "unsigned", archive: archive, noSig: true, wantErr: "not signed"},
				{name: "untrusted key", archive: archive, signature: sign(otherPriv, checksums), wantErr: "signature verification failed"},
				{name: "tampered archive", archive: append(append([]byte(nil), archive...), 0), signature: sign(priv, checksums), wantErr: "checksum verification failed"},
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					files := map[string][]byte{archiveName: tc.archive, checksumsAsset: checksums}
					if !tc.noSig {
						files[signatureAsset] = tc.signature
					}
					rel := serveRelease(t, "v1.0.0", files)

					got, err := fetchVerifiedBinary(rel, goos, "amd64", []string{pub}, io.Discard)
					if tc.wantErr != "" {
						if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
							t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
						}
						return
					}
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					if !bytes.Equal(got, binary) {
						t.Errorf("expected extracted binary %q, got %q", binary, got)
					}
				})
			}
		})
	}
}

// serveRelease serves files over HTTP and returns release metadata pointing at them.
func serveRelease(t *testing.T, tag string, files map[string][]byte) *ReleaseInfo {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)

	rel := &ReleaseInfo{TagName: tag}
	for name := range files {
		rel.Assets = append(rel.Assets, ReleaseAsset{Name: name, BrowserDownloadURL: srv.URL + "/" + name})
	}
	return rel
}

// makeTestArchive packages binary like the CD workflow: a tar.gz (or zip on Windows) with a top-level directory.
func makeTestArchive(t *testing.T, goos, dir string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	if goos == "windows" {
		zw := zip.NewWriter(&buf)
		w, err := zw.Create(dir + "/sshx.exe")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(binary); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}

	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	if err := tw.WriteHeader(&tar.Header{Name: dir + "/sshx", Mode: 0755, Size: int64(len(binary)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestFetchVerifiedBinaryPicksTheExactArchive(t *testing.T) {
	priv, pub := newSigningKey(t)
	binary := []byte("new-sshx-binary")
	name := "sshx_v1.0.0_linux_amd64"
	archive := makeTestArchive(t, "linux", name, binary)
	checksums := []byte(fmt.Sprintf("%x  %s.tar.gz\n", sha256.Sum256(archive), name))
	files := map[string][]byte{
		name + ".tar.gz":       archive,
		name + "_debug.tar.gz": []byte("a different build that shares the prefix"),
		checksumsAsset:         checksums,
		signatureAsset:         sign(priv, checksums),
	}
	rel := serveRelease(t, "v1.0.0", files)
	// List the look-alike last, where a prefix match would have picked it.
	for i, a := range rel.Assets {
		if strings.HasSuffix(a.Name, "_debug.tar.gz") {
			rel.Assets = append(append(rel.Assets[:i:i], rel.Assets[i+1:]...), a)
			break
		}
	}

	got, err := fetchVerifiedBinary(rel, "linux", "amd64", []string{pub}, io.Discard)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("expected the exact archive, got %q (err %v)", got, err)
	}
}

func TestPerformUpdate(t *testing.T) {
	useTempCacheDir(t)
	priv, pub := newSigningKey(t)
	oldKeys := trustedSigningKeys
	trustedSigningKeys = []string{pub}
	t.Cleanup(func() { trustedSigningKeys = oldKeys })

	// A stand-in for the running binary, so the test never replaces itself.
	exe := filepath.Join(t.TempDir(), "sshx")
	if err := os.WriteFile(exe, []byte("old-sshx"), 0600); err != nil {
		t.Fatal(err)
	}
	oldExe := executablePath
	executablePath = func() (string, error) { return exe, nil }
	t.Cleanup(func() { executablePath = oldExe })

	goos, goarch := runtime.GOOS, runtime.GOARCH
	name := fmt.Sprintf("sshx_v1.0.0_%s_%s", goos, goarch)
	archive := makeTestArchive(t, goos, name, []byte("new-sshx"))
	archiveName := name + ".tar.gz"
	if goos == "windows" {
		archiveName = name + ".zip"
	}
	checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName))
	rel := serveRelease(t, "v1.0.0", map[string][]byte{archiveName: archive, checksumsAsset: checksums, signatureAsset: sign(priv, checksums)})

	// The latest-release API, on its own server.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(rel)
	}))
	t.Cleanup(api.Close)
	oldURL := releasesAPIURL
	releasesAPIURL = api.URL
	t.Cleanup(func() { releasesAPIURL = oldURL })

	updated, err := PerformUpdate("v0.9.0", io.Discard, false)
	if err != nil || !updated {
		t.Fatalf("expected an update, got updated=%v err=%v", updated, err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "new-sshx" { //nolint:gosec // test file read
		t.Errorf("expected the new binary in place, got %q", data)
	}

	// Already current: nothing to do.
	if updated, err := PerformUpdate("v1.0.0", io.Discard, false); err != nil || updated {
		t.Errorf("expected no update when current, got updated=%v err=%v", updated, err)
	}
}
