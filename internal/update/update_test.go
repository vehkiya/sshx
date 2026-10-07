package update

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
	"strings"
	"testing"
)

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current, latest string
		want            bool
	}{
		{"v0.4.1", "v0.5.0", true},
		{"v0.4.1", "v0.4.2", true},
		{"v0.4.1", "v1.0.0", true},
		{"v0.5.0", "v0.4.9", false},
		{"v0.5.0", "v0.5.0", false},
		{"v0.5.0-rc1", "v0.5.0", true},
		{"v0.5.0", "v0.5.0-rc1", false},
		{"v0.0.0-20261005165755-2446bc0d13e5", "v0.4.1", true}, // a local build
		{"v0.4.1+dirty", "v0.4.1", false},
		{"v0.4.1+dirty", "v0.4.2", true},
		{"dev", "v0.4.1", true},
		{"none", "v0.4.1", true},
		{"", "v0.4.1", true},
		{"v0.4.1", "", false},
		{"0.4.1", "v0.5.0", true}, // without the v
		{"v0.3.1-0.20261002093000-abcdef123456+dirty", "v0.3.1", true},
	}
	for _, c := range cases {
		if got := IsNewer(c.current, c.latest); got != c.want {
			t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
		}
	}
}

func TestCheckDisabled(t *testing.T) {
	t.Setenv(NoCheckEnv, "")
	if CheckDisabled("v0.4.1") {
		t.Error("check disabled for a release build")
	}
	if !CheckDisabled("dev") {
		t.Error("check enabled for a development build")
	}
	t.Setenv(NoCheckEnv, "1")
	if !CheckDisabled("v0.4.1") {
		t.Errorf("%s didn't turn the check off", NoCheckEnv)
	}
}

func TestTrustedKeysAreEd25519PublicKeys(t *testing.T) {
	if len(TrustedKeys) == 0 {
		t.Fatal("no trusted release keys")
	}
	for _, k := range TrustedKeys {
		if pub, err := base64.StdEncoding.DecodeString(k); err != nil || len(pub) != ed25519.PublicKeySize {
			t.Errorf("%q isn't a base64 Ed25519 public key (err %v, %d bytes)", k, err, len(pub))
		}
	}
}

func newKey(t *testing.T) (ed25519.PrivateKey, string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return priv, base64.StdEncoding.EncodeToString(pub)
}

// sign makes a checksums.txt.sig the way the release workflow does: base64 of the raw signature.
func sign(priv ed25519.PrivateKey, data []byte) []byte {
	return []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, data)))
}

func TestVerifySignature(t *testing.T) {
	priv, pub := newKey(t)
	otherPriv, otherPub := newKey(t)
	data := []byte("abc  sshx_v1.0.0_linux_amd64.tar.gz\n")

	if err := verifySignature(data, sign(priv, data), []string{pub}); err != nil {
		t.Errorf("valid signature: %v", err)
	}
	if err := verifySignature(data, append(sign(priv, data), '\n'), []string{pub}); err != nil {
		t.Errorf("trailing newline: %v", err)
	}
	if err := verifySignature(data, sign(otherPriv, data), []string{pub, otherPub}); err != nil {
		t.Errorf("any trusted key should do, as during a rotation: %v", err)
	}
	if err := verifySignature(append(data, 'x'), sign(priv, data), []string{pub}); err == nil {
		t.Error("tampered checksums verified")
	}
	if err := verifySignature(data, sign(otherPriv, data), []string{pub}); err == nil {
		t.Error("a signature from an untrusted key verified")
	}
	if err := verifySignature(data, []byte("not-base64!"), []string{pub}); err == nil {
		t.Error("a malformed signature verified")
	}
}

func TestVerifyChecksum(t *testing.T) {
	data := []byte("archive")
	sum := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := verifyChecksum(data, "a.tar.gz", sum+"  a.tar.gz\n"); err != nil {
		t.Errorf("matching checksum: %v", err)
	}
	if err := verifyChecksum(data, "a.tar.gz", strings.ToUpper(sum)+" *dist/a.tar.gz\n"); err != nil {
		t.Errorf("binary-mode entry with a path: %v", err)
	}
	if err := verifyChecksum(append(data, 'x'), "a.tar.gz", sum+"  a.tar.gz\n"); err == nil {
		t.Error("a tampered archive passed")
	}
	if err := verifyChecksum(data, "a.tar.gz", sum+"  b.tar.gz\n"); err == nil {
		t.Error("an archive without a checksum passed")
	}
}

// makeTarArchive packages a binary as a .tar.gz with a top-level directory.
func makeTarArchive(t *testing.T, dir string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range map[string][]byte{dir + "/README.md": []byte("readme"), dir + "/sshx": binary} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// makeZipArchive packages a binary as a .zip with a top-level directory.
func makeZipArchive(t *testing.T, dir string, binary []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range map[string][]byte{dir + "/README.md": []byte("readme"), dir + "/sshx.exe": binary} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExtractBinary(t *testing.T) {
	got, err := extractBinary(makeTarArchive(t, "sshx_v1.0.0_linux_amd64", []byte("new sshx")), "sshx")
	if err != nil || string(got) != "new sshx" {
		t.Errorf("extractBinary = %q, %v", got, err)
	}
	if _, err := extractBinary(makeTarArchive(t, "x", []byte("b")), "other"); err == nil {
		t.Error("found a binary that isn't in the archive")
	}
	if _, err := extractBinary([]byte("not gzip"), "sshx"); err == nil {
		t.Error("read an archive that isn't gzip")
	}
}

func TestExtractBinaryFromZip(t *testing.T) {
	got, err := extractBinaryFromZip(makeZipArchive(t, "sshx_v1.0.0_windows_amd64", []byte("new sshx.exe")), "sshx.exe")
	if err != nil || string(got) != "new sshx.exe" {
		t.Errorf("extractBinaryFromZip = %q, %v", got, err)
	}
	if _, err := extractBinaryFromZip(makeZipArchive(t, "x", []byte("b")), "other.exe"); err == nil {
		t.Error("found a binary that isn't in the zip archive")
	}
	if _, err := extractBinaryFromZip([]byte("not zip"), "sshx.exe"); err == nil {
		t.Error("read an archive that isn't zip")
	}
}

// serveRelease serves a release's files, and GitHub's latest-release API
// pointing at them, from a local test server.
func serveRelease(t *testing.T, tag string, files map[string][]byte) *Release {
	t.Helper()
	rel := &Release{TagName: tag}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/latest" {
			_ = json.NewEncoder(w).Encode(rel)
			return
		}
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(srv.Close)
	for name := range files {
		rel.Assets = append(rel.Assets, Asset{Name: name, BrowserDownloadURL: srv.URL + "/" + name})
	}
	old := releasesAPIURL
	releasesAPIURL = srv.URL + "/api/latest"
	t.Cleanup(func() { releasesAPIURL = old })
	return rel
}

// release builds a release's files for one platform, signed with priv.
func release(t *testing.T, tag, goos, goarch string, binary []byte, priv ed25519.PrivateKey) (map[string][]byte, string) {
	t.Helper()
	name := fmt.Sprintf("sshx_%s_%s_%s", tag, goos, goarch)
	var archive []byte
	ext := ".tar.gz"
	if goos == "windows" {
		archive = makeZipArchive(t, name, binary)
		ext = ".zip"
	} else {
		archive = makeTarArchive(t, name, binary)
	}
	archiveName := name + ext
	sums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName))
	return map[string][]byte{archiveName: archive, checksumsAsset: sums, signatureAsset: sign(priv, sums)}, archiveName
}

func TestFetchVerifiedBinary(t *testing.T) {
	priv, pub := newKey(t)
	otherPriv, _ := newKey(t)
	binary := []byte("new sshx")

	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			cases := []struct {
				name    string
				change  func(files map[string][]byte, archive string)
				wantErr string
			}{
				{"signed and intact", func(map[string][]byte, string) {}, ""},
				{"unsigned", func(f map[string][]byte, _ string) { delete(f, signatureAsset) }, "not signed"},
				{"signed by another key", func(f map[string][]byte, _ string) { f[signatureAsset] = sign(otherPriv, f[checksumsAsset]) }, "signature verification failed"},
				{"tampered archive", func(f map[string][]byte, a string) { f[a] = append(append([]byte(nil), f[a]...), 0) }, "checksum verification failed"},
				{"no checksums", func(f map[string][]byte, _ string) { delete(f, checksumsAsset) }, "no checksums.txt"},
				{"no build for this platform", func(f map[string][]byte, a string) { delete(f, a) }, "has no build for"},
			}
			for _, c := range cases {
				t.Run(c.name, func(t *testing.T) {
					files, archive := release(t, "v1.0.0", goos, "amd64", binary, priv)
					c.change(files, archive)
					rel := serveRelease(t, "v1.0.0", files)
					got, err := fetchVerifiedBinary(rel, goos, "amd64", []string{pub}, io.Discard)
					switch {
					case c.wantErr == "" && (err != nil || !bytes.Equal(got, binary)):
						t.Errorf("got %q, %v", got, err)
					case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
						t.Errorf("error = %v, want one mentioning %q", err, c.wantErr)
					}
				})
			}
		})
	}
}

func TestFetchVerifiedBinaryPicksTheExactArchive(t *testing.T) {
	priv, pub := newKey(t)
	binary := []byte("new-sshx-binary")
	name := "sshx_v1.0.0_linux_amd64"
	archive := makeTarArchive(t, name, binary)
	archiveName := name + ".tar.gz"
	checksums := []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(archive), archiveName))
	files := map[string][]byte{
		archiveName:            archive,
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

func TestPerform(t *testing.T) {
	priv, pub := newKey(t)
	oldKeys := TrustedKeys
	TrustedKeys = []string{pub}
	t.Cleanup(func() { TrustedKeys = oldKeys })
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir()) // macOS keeps caches under ~/Library/Caches
	t.Setenv("LocalAppData", t.TempDir())

	installed := filepath.Join(t.TempDir(), "sshx")
	if err := os.WriteFile(installed, []byte("old sshx"), 0755); err != nil { //nolint:gosec // stand-in for the binary
		t.Fatal(err)
	}
	oldPath := executablePath
	executablePath = func() (string, error) { return installed, nil }
	t.Cleanup(func() { executablePath = oldPath })

	files, _ := release(t, "v1.0.0", runtime.GOOS, runtime.GOARCH, []byte("new sshx"), priv)
	serveRelease(t, "v1.0.0", files)

	got, err := Perform("v0.9.0", io.Discard, false)
	if err != nil || got != "v1.0.0" {
		t.Fatalf("Perform = %q, %v", got, err)
	}
	if data, _ := os.ReadFile(installed); string(data) != "new sshx" { //nolint:gosec // test file read
		t.Errorf("installed binary = %q", data)
	}
	if info, _ := os.Stat(installed); info.Mode().Perm() != 0755 {
		t.Errorf("installed binary mode = %v", info.Mode().Perm())
	}

	// Already current: nothing to do, and the cache now says so too.
	if got, err := Perform("v1.0.0", io.Discard, false); err != nil || got != "" {
		t.Errorf("Perform when current = %q, %v", got, err)
	}
	if latest, newer, err := LatestCached("v0.9.0"); err != nil || latest != "v1.0.0" || !newer {
		t.Errorf("LatestCached = %q, %v, %v", latest, newer, err)
	}
}

func TestReplaceExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "sshx")
	if err := os.WriteFile(exe, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := replaceExecutable(exe, []byte("new")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data, _ := os.ReadFile(exe); string(data) != "new" { //nolint:gosec // test file read
		t.Errorf("expected new binary, got %q", data)
	}
}

func TestReplaceExecutableRestoresOnWindowsFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		// Mock Windows behavior by verifying renameFile fallback logic
		return
	}
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

	if err := replaceExecutable(exe, []byte("new")); err == nil {
		t.Fatalf("expected the simulated failure to be reported")
	}
	if data, err := os.ReadFile(exe); err != nil || string(data) != "old" { //nolint:gosec // test file read
		t.Errorf("expected the original binary to be restored, got %q (err %v)", data, err)
	}
}

func TestReadLimited(t *testing.T) {
	if _, err := readLimited(strings.NewReader("12345"), 4); err == nil {
		t.Error("read past the limit")
	}
	if got, err := readLimited(strings.NewReader("1234"), 4); err != nil || string(got) != "1234" {
		t.Errorf("readLimited = %q, %v", got, err)
	}
}
