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
	repoOwner       = "vehkiya"
	repoName        = "sshx"
	releasesAPIURL  = "https://api.github.com/repos/vehkiya/sshx/releases/latest"
	updateCacheFile = ".sshx_update_cache"
	cacheTTL        = 6 * time.Hour
)

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

	body, err := io.ReadAll(resp.Body)
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

// CheckLatestReleaseCached checks for updates, using a local cache if checked within cacheTTL.
func CheckLatestReleaseCached(currentVersion, homeDir string) (latestVersion string, isNewer bool, err error) {
	cachePath := filepath.Join(homeDir, ".ssh", updateCacheFile)

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

	// Save to cache
	c := updateCache{
		CheckedAt:     time.Now().Unix(),
		LatestVersion: rel.TagName,
	}
	if cData, err := json.Marshal(c); err == nil {
		_ = AtomicWrite(cachePath, cData, 0600)
	}

	return rel.TagName, newer, nil
}

// isNewerVersion compares semver strings (e.g. v0.3.0 and v0.4.0).
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
	return false
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
func PerformUpdate(currentVersion string, stdout io.Writer, force bool) error {
	_, _ = fmt.Fprintf(stdout, "Checking for latest release from https://github.com/%s/%s...\n", repoOwner, repoName)
	rel, isNewer, err := CheckLatestRelease(currentVersion)
	if err != nil {
		return fmt.Errorf("failed checking for updates: %w", err)
	}

	if !isNewer && !force {
		_, _ = fmt.Fprintf(stdout, "sshx is already up to date (%s)\n", currentVersion)
		return nil
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
		return fmt.Errorf("no release asset found matching platform %s/%s for %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
	}

	_, _ = fmt.Fprintf(stdout, "Downloading %s...\n", assetName)
	archiveData, err := downloadURL(assetURL)
	if err != nil {
		return fmt.Errorf("failed downloading release archive: %w", err)
	}

	if checksumURL != "" {
		_, _ = fmt.Fprintf(stdout, "Verifying sha256 checksum...\n")
		if sumData, err := downloadURL(checksumURL); err == nil {
			if err := verifyChecksum(archiveData, assetName, string(sumData)); err != nil {
				return fmt.Errorf("checksum verification failed: %w", err)
			}
			_, _ = fmt.Fprintf(stdout, "✔ Checksum verified\n")
		}
	}

	_, _ = fmt.Fprintf(stdout, "Extracting binary...\n")
	var binaryBytes []byte
	if runtime.GOOS == "windows" {
		binaryBytes, err = extractBinaryFromZip(archiveData, binName)
	} else {
		binaryBytes, err = extractBinaryFromTarGz(archiveData, binName)
	}
	if err != nil {
		return fmt.Errorf("failed extracting binary: %w", err)
	}

	path, err := ReplaceCurrentExecutable(binaryBytes)
	if err != nil {
		return err
	}

	badge := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#5FD787")).
		Padding(0, 1).
		Render(" UPDATED ")
	_, _ = fmt.Fprintf(stdout, "\n%s Successfully updated sshx to %s at %s\n\n", badge, rel.TagName, path)
	return nil
}

func downloadURL(url string) ([]byte, error) {
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

	return io.ReadAll(resp.Body)
}

func verifyChecksum(data []byte, assetName, checksumsContent string) error {
	expectedHash := ""
	for _, line := range strings.Split(checksumsContent, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			fileName := filepath.Base(fields[1])
			if fileName == assetName {
				expectedHash = strings.ToLower(fields[0])
				break
			}
		}
	}

	if expectedHash == "" {
		return nil
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
				return io.ReadAll(tr)
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
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
			return data, nil
		}
	}
	return nil, fmt.Errorf("binary '%s' not found inside zip archive", binaryName)
}

// ReplaceCurrentExecutable atomically updates the currently running binary.
func ReplaceCurrentExecutable(newBinary []byte) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("failed to locate running executable: %w", err)
	}

	realPath, err := filepath.EvalSymlinks(execPath)
	if err != nil {
		realPath = execPath
	}

	if strings.Contains(realPath, "go-build") {
		return realPath, errors.New("cannot update a temporary binary running under 'go run'")
	}

	dir := filepath.Dir(realPath)
	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return realPath, err
	}
	tmpName := filepath.Join(dir, fmt.Sprintf(".tmp.sshx.%s", hex.EncodeToString(randBytes)))

	// Check if directory is writable
	tmpFile, err := os.OpenFile(filepath.Clean(tmpName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755) //nolint:gosec // updater temp file
	if err != nil {
		if os.IsPermission(err) {
			return realPath, fmt.Errorf("permission denied writing to %s (run with sudo: sudo sshx update)", dir)
		}
		return realPath, fmt.Errorf("failed to create temporary binary: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(newBinary); err != nil {
		return realPath, fmt.Errorf("failed to write new binary: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return realPath, fmt.Errorf("failed to sync new binary: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return realPath, fmt.Errorf("failed to close temporary file: %w", err)
	}

	if err := os.Chmod(tmpName, 0755); err != nil { //nolint:gosec // executable binary requires 0755 permissions
		return realPath, fmt.Errorf("failed to set permissions on new binary: %w", err)
	}

	if runtime.GOOS == "windows" {
		oldPath := realPath + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(realPath, oldPath); err != nil {
			return realPath, fmt.Errorf("failed to rename existing binary on Windows: %w", err)
		}
	}

	if err := os.Rename(tmpName, realPath); err != nil {
		return realPath, fmt.Errorf("failed to replace executable: %w", err)
	}

	success = true
	return realPath, nil
}
