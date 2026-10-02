package main

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const (
	repoOwner      = "vehkiya"
	repoName       = "sshx"
	releasesAPIURL = "https://api.github.com/repos/vehkiya/sshx/releases/latest"
	cacheTTL       = 6 * time.Hour

	// legacyUpdateCacheFile is where older versions kept the cache, inside ~/.ssh.
	legacyUpdateCacheFile = ".sshx_update_cache"

	// noUpdateCheckEnv disables the TUI's background update check when set to any value.
	noUpdateCheckEnv = "SSHX_NO_UPDATE_CHECK"

	maxReleaseInfoSize = 1 << 20
	maxChecksumsSize   = 1 << 20
	maxArchiveSize     = 64 << 20
	maxBinarySize      = 128 << 20
)

// renameFile is os.Rename, replaceable in tests to simulate failures.
var renameFile = os.Rename

// ReleaseAsset represents a single downloadable file in a GitHub release.
type ReleaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// ReleaseInfo represents GitHub release metadata.
type ReleaseInfo struct {
	TagName string         `json:"tag_name"`
	Name    string         `json:"name"`
	Body    string         `json:"body"`
	HTMLURL string         `json:"html_url"`
	Assets  []ReleaseAsset `json:"assets"`
}

// updateCache stores the last check timestamp and version to respect GitHub API rate limits.
type updateCache struct {
	CheckedAt     int64  `json:"checked_at"`
	LatestVersion string `json:"latest_version"`
}

// CheckLatestRelease queries GitHub API for the latest release.
func CheckLatestRelease(currentVersion string) (*ReleaseInfo, bool, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest(http.MethodGet, releasesAPIURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", fmt.Sprintf("sshx/%s (%s/%s)", currentVersion, runtime.GOOS, runtime.GOARCH))

	resp, err := client.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub API returned HTTP %d", resp.StatusCode)
	}

	body, err := readLimited(resp.Body, maxReleaseInfoSize)
	if err != nil {
		return nil, false, err
	}

	var rel ReleaseInfo
	if err := json.Unmarshal(body, &rel); err != nil {
		return nil, false, err
	}

	isNewer := isNewerVersion(currentVersion, rel.TagName)
	return &rel, isNewer, nil
}

// updateCachePath is the update-check cache in the user's cache directory.
func updateCachePath(homeDir string) string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "sshx", "update-check.json")
	}
	return filepath.Join(homeDir, ".cache", "sshx", "update-check.json")
}

// updateCheckDisabled reports whether the background update check should be skipped:
// when the user opted out, or for development builds that have no release to compare with.
func updateCheckDisabled(currentVersion string) bool {
	return os.Getenv(noUpdateCheckEnv) != "" || currentVersion == "dev"
}

func writeUpdateCache(homeDir, latestVersion string) {
	c := updateCache{
		CheckedAt:     time.Now().Unix(),
		LatestVersion: latestVersion,
	}
	if cData, err := json.Marshal(c); err == nil {
		_ = AtomicWrite(updateCachePath(homeDir), cData, 0600)
	}
}

// CheckLatestReleaseCached checks for updates, using a local cache if checked within cacheTTL.
func CheckLatestReleaseCached(currentVersion, homeDir string) (latestVersion string, isNewer bool, err error) {
	cachePath := updateCachePath(homeDir)
	_ = os.Remove(filepath.Join(homeDir, ".ssh", legacyUpdateCacheFile))

	// Check cache
	if data, err := os.ReadFile(filepath.Clean(cachePath)); err == nil { //nolint:gosec // user update cache file
		var c updateCache
		if json.Unmarshal(data, &c) == nil {
			if time.Since(time.Unix(c.CheckedAt, 0)) < cacheTTL && c.LatestVersion != "" {
				return c.LatestVersion, isNewerVersion(currentVersion, c.LatestVersion), nil
			}
		}
	}

	rel, newer, err := CheckLatestRelease(currentVersion)
	if err != nil {
		return "", false, err
	}

	writeUpdateCache(homeDir, rel.TagName)
	return rel.TagName, newer, nil
}

// isNewerVersion compares semver strings (e.g. v0.3.0 and v0.4.0). A release
// is newer than a pre-release of the same version, such as v0.3.0-rc1 or a Go
// pseudo-version like v0.3.0-0.20261002093000-abcdef123456.
func isNewerVersion(current, latest string) bool {
	curr := strings.TrimPrefix(strings.TrimSpace(current), "v")
	lat := strings.TrimPrefix(strings.TrimSpace(latest), "v")

	if lat == "" {
		return false
	}
	if curr == "dev" || curr == "none" || curr == "" {
		return true
	}

	currParts := parseSemVerParts(curr)
	latParts := parseSemVerParts(lat)

	for i := 0; i < 3; i++ {
		if latParts[i] > currParts[i] {
			return true
		}
		if latParts[i] < currParts[i] {
			return false
		}
	}
	return isPrerelease(curr) && !isPrerelease(lat)
}

func isPrerelease(v string) bool {
	if idx := strings.Index(v, "+"); idx != -1 {
		v = v[:idx]
	}
	return strings.Contains(v, "-")
}

func parseSemVerParts(v string) [3]int {
	var parts [3]int
	if idx := strings.Index(v, "-"); idx != -1 {
		v = v[:idx]
	}
	segments := strings.Split(v, ".")
	for i := 0; i < len(segments) && i < 3; i++ {
		if p, err := strconv.Atoi(segments[i]); err == nil {
			parts[i] = p
		}
	}
	return parts
}

// PerformUpdate downloads the matching release asset, verifies its checksum, and replaces the current binary.
// Returns (true, nil) if binary was updated, (false, nil) if already up to date, or an error.
func PerformUpdate(currentVersion string, stdout io.Writer, force bool) (bool, error) {
	_, _ = fmt.Fprintf(stdout, "Checking for latest release from https://github.com/%s/%s...\n", repoOwner, repoName)
	rel, isNewer, err := CheckLatestRelease(currentVersion)
	if err != nil {
		return false, fmt.Errorf("failed checking for updates: %w", err)
	}

	if !isNewer && !force {
		_, _ = fmt.Fprintf(stdout, "sshx is already up to date (%s)\n", currentVersion)
		return false, nil
	}

	_, _ = fmt.Fprintf(stdout, "Latest release is %s (current: %s)\n", rel.TagName, currentVersion)

	expectedExt := ".tar.gz"
	binName := "sshx"
	if runtime.GOOS == "windows" {
		expectedExt = ".zip"
		binName = "sshx.exe"
	}

	expectedPrefix := fmt.Sprintf("sshx_%s_%s_%s", rel.TagName, runtime.GOOS, runtime.GOARCH)
	var assetURL string
	var assetName string
	var checksumURL string

	for _, a := range rel.Assets {
		if a.Name == "checksums.txt" {
			checksumURL = a.BrowserDownloadURL
		}
		if strings.HasPrefix(a.Name, expectedPrefix) && strings.HasSuffix(a.Name, expectedExt) {
			assetURL = a.BrowserDownloadURL
			assetName = a.Name
		}
	}

	if assetURL == "" {
		return false, fmt.Errorf("no release asset found matching platform %s/%s for %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	if checksumURL == "" {
		return false, fmt.Errorf("release %s has no checksums.txt; refusing to install an unverified binary", rel.TagName)
	}

	_, _ = fmt.Fprintf(stdout, "Downloading %s...\n", assetName)
	archiveData, err := downloadURL(assetURL, maxArchiveSize)
	if err != nil {
		return false, fmt.Errorf("failed downloading release archive: %w", err)
	}

	_, _ = fmt.Fprintf(stdout, "Verifying sha256 checksum...\n")
	sumData, err := downloadURL(checksumURL, maxChecksumsSize)
	if err != nil {
		return false, fmt.Errorf("failed downloading checksums: %w", err)
	}
	if err := verifyChecksum(archiveData, assetName, string(sumData)); err != nil {
		return false, fmt.Errorf("checksum verification failed: %w", err)
	}
	_, _ = fmt.Fprintf(stdout, "✔ Checksum verified\n")

	_, _ = fmt.Fprintf(stdout, "Extracting binary...\n")
	var binaryBytes []byte
	if runtime.GOOS == "windows" {
		binaryBytes, err = extractBinaryFromZip(archiveData, binName)
	} else {
		binaryBytes, err = extractBinaryFromTarGz(archiveData, binName)
	}
	if err != nil {
		return false, fmt.Errorf("failed extracting binary: %w", err)
	}

	path, err := ReplaceCurrentExecutable(binaryBytes)
	if err != nil {
		return false, err
	}

	// Update local cache so next startup has fresh information
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		writeUpdateCache(homeDir, rel.TagName)
	}

	updatedBadge := badge(" UPDATED ", colorBlack, colorGreen)
	infoStyle := lipgloss.NewStyle().
		Foreground(colorCyan).
		Bold(true)
	_, _ = fmt.Fprintf(stdout, "\n%s Successfully updated sshx to %s at %s\n\n", updatedBadge, rel.TagName, path)
	_, _ = fmt.Fprintf(stdout, "%s Restart sshx to apply the update.\n\n", infoStyle.Render("➜"))
	return true, nil
}

// readLimited reads all of r, failing if it holds more than limit bytes.
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}

func downloadURL(url string, limit int64) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "sshx-updater")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned HTTP %d for %s", resp.StatusCode, url)
	}

	return readLimited(resp.Body, limit)
}

// verifyChecksum checks data against the sha256sum-format entry for assetName.
// A missing entry is an error: the updater never installs an unverified binary.
func verifyChecksum(data []byte, assetName, checksumsContent string) error {
	expectedHash := ""
	for _, line := range strings.Split(checksumsContent, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			// sha256sum marks binary-mode entries with a leading '*'.
			fileName := filepath.Base(strings.TrimPrefix(fields[1], "*"))
			if fileName == assetName {
				expectedHash = strings.ToLower(fields[0])
				break
			}
		}
	}

	if expectedHash == "" {
		return fmt.Errorf("no checksum listed for %s", assetName)
	}

	actualHash := fmt.Sprintf("%x", sha256.Sum256(data))
	if actualHash != expectedHash {
		return fmt.Errorf("expected %s, got %s", expectedHash, actualHash)
	}
	return nil
}

func extractBinaryFromTarGz(archiveData []byte, binaryName string) ([]byte, error) {
	gzr, err := gzip.NewReader(bytes.NewReader(archiveData))
	if err != nil {
		return nil, err
	}
	defer func() { _ = gzr.Close() }()

	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}

		if header.Typeflag == tar.TypeReg {
			base := filepath.Base(header.Name)
			if base == binaryName {
				return readLimited(tr, maxBinarySize)
			}
		}
	}
	return nil, fmt.Errorf("binary '%s' not found inside archive", binaryName)
}

func extractBinaryFromZip(archiveData []byte, binaryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archiveData), int64(len(archiveData)))
	if err != nil {
		return nil, err
	}

	for _, file := range zr.File {
		base := filepath.Base(file.Name)
		if strings.EqualFold(base, binaryName) {
			rc, err := file.Open()
			if err != nil {
				return nil, err
			}
			data, err := readLimited(rc, maxBinarySize)
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("binary '%s' not found inside zip archive", binaryName)
}

// currentExecutablePath returns the running binary's path with symlinks resolved.
func currentExecutablePath() (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to locate running executable: %w", err)
	}
	if realPath, err := filepath.EvalSymlinks(execPath); err == nil {
		return realPath, nil
	}
	return execPath, nil
}

// RemoveStaleBinary deletes the previous executable that a Windows self-update
// leaves behind, since a running .exe cannot be deleted during the update.
func RemoveStaleBinary() {
	if runtime.GOOS != "windows" {
		return
	}
	if realPath, err := currentExecutablePath(); err == nil {
		_ = os.Remove(realPath + ".old")
	}
}

// ReplaceCurrentExecutable atomically updates the currently running binary.
func ReplaceCurrentExecutable(newBinary []byte) (string, error) {
	realPath, err := currentExecutablePath()
	if err != nil {
		return "", err
	}
	if strings.Contains(realPath, "go-build") {
		return realPath, errors.New("cannot update a temporary binary running under 'go run'")
	}
	return realPath, replaceExecutable(realPath, newBinary, runtime.GOOS)
}

// replaceExecutable swaps the file at realPath for newBinary. Windows cannot
// overwrite a running executable, so there the old binary is first moved aside
// to realPath+".old" and moved back if installing the new one fails.
func replaceExecutable(realPath string, newBinary []byte, goos string) error {
	dir := filepath.Dir(realPath)
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return err
	}
	tmpName := filepath.Join(dir, fmt.Sprintf(".tmp.sshx.%s", hex.EncodeToString(randBytes)))

	// Check if directory is writable
	tmpFile, err := os.OpenFile(filepath.Clean(tmpName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755) //nolint:gosec // updater temp file
	if err != nil {
		if os.IsPermission(err) {
			return fmt.Errorf("permission denied writing to %s (run with sudo: sudo sshx update)", dir)
		}
		return fmt.Errorf("failed to create temporary binary: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(newBinary); err != nil {
		return fmt.Errorf("failed to write new binary: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync new binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := os.Chmod(tmpName, 0755); err != nil { //nolint:gosec // executable binary requires 0755 permissions
		return fmt.Errorf("failed to set permissions on new binary: %w", err)
	}

	oldPath := ""
	if goos == "windows" {
		oldPath = realPath + ".old"
		_ = os.Remove(oldPath)
		if err := renameFile(realPath, oldPath); err != nil {
			return fmt.Errorf("failed to rename existing binary on Windows: %w", err)
		}
	}

	if err := renameFile(tmpName, realPath); err != nil {
		if oldPath != "" {
			if restoreErr := renameFile(oldPath, realPath); restoreErr != nil {
				return fmt.Errorf("failed to replace executable: %w (restoring the previous binary also failed: %v; it is at %s)", err, restoreErr, oldPath)
			}
		}
		return fmt.Errorf("failed to replace executable: %w", err)
	}

	success = true
	return nil
}
