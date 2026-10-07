package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/vehkiya/sshx/internal/sshconfig"
)

func TestWriteHostTableAlignment(t *testing.T) {
	hosts := []sshconfig.HostItem{
		{Alias: "a-really-long-alias-name", HostName: "h.lan", User: "root", Port: 22, PubkeyAuth: true},
		{Alias: "b", Port: 2222, PubkeyAuth: true, IdentityFile: "~/.ssh/id_ed25519"},
	}
	var buf bytes.Buffer
	writeHostTable(&buf, hosts)
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected header, rule and 2 rows, got:\n%s", buf.String())
	}
	for _, col := range []struct {
		header string
		values []string
	}{
		{"TARGET", []string{"root@h.lan", "b"}},
		{"PORT", []string{"22", "2222"}},
		{"AUTH", []string{"Default", "id_ed25519"}},
	} {
		at := strings.Index(lines[0], col.header)
		for i, v := range col.values {
			if !strings.HasPrefix(lines[2+i][at:], v) {
				t.Errorf("expected %q under %s at column %d, got:\n%s", v, col.header, at, buf.String())
			}
		}
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, ".ssh"), 0700); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"lst"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for an unknown command, got %d", code)
	}
	if code := run([]string{"--bogus"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for an unknown option, got %d", code)
	}
	if code := run([]string{"rm"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for missing arguments, got %d", code)
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
