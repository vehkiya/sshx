package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"testing"

	"github.com/vehkiya/sshx/internal/sshconfig"
	"github.com/vehkiya/sshx/internal/wizard"
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

func TestRunUpdateUsage(t *testing.T) {
	tmpDir := t.TempDir()
	if code := run([]string{"update", "extra"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for update with extra arguments, got %d", code)
	}
	if code := run([]string{"check-update", "extra"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for check-update with extra arguments, got %d", code)
	}
}

func TestAccessibleNeverOpensBrowser(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(sshDir, "config")
	if err := os.WriteFile(cfg, []byte("Host test\n    HostName test.lan\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// run(nil) with ACCESSIBLE set should run cmdLs and never enter runTUILoop.
	code := run(nil, tmpDir)
	if code != 0 {
		t.Fatalf("run(nil) under ACCESSIBLE exit status = %d, want 0", code)
	}
}

func TestLsJSONOutput(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	configDir := filepath.Join(sshDir, "config.d")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}

	mainConfig := `Include config.d/*

Host keyhost
    HostName key.lan
    User deploy
    Port 22
    IdentityFile ~/.ssh/id_ed25519

Host pwhost
    HostName pw.lan
    User admin
    Port 2222
    PubkeyAuthentication no

Host defaulthost
    HostName def.lan
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(mainConfig), 0600); err != nil {
		t.Fatal(err)
	}

	extraPath := filepath.Join(configDir, "extra.conf")
	extraConfig := `Host inchost
    HostName inc.lan
    User worker
    Port 2200
`
	if err := os.WriteFile(extraPath, []byte(extraConfig), 0600); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	code := cmdLs([]string{"--json"}, &buf, tmpDir)
	if code != 0 {
		t.Fatalf("cmdLs --json exited with %d, want 0", code)
	}

	var hosts []hostJSON
	if err := json.Unmarshal(buf.Bytes(), &hosts); err != nil {
		t.Fatalf("failed to parse JSON output: %v\nOutput:\n%s", err, buf.String())
	}

	if len(hosts) != 4 {
		t.Fatalf("expected 4 hosts, got %d: %+v", len(hosts), hosts)
	}

	byAlias := make(map[string]hostJSON)
	for _, h := range hosts {
		byAlias[h.Alias] = h
	}

	// 1. keyhost
	kh, ok := byAlias["keyhost"]
	if !ok || kh.HostName != "key.lan" || kh.User != "deploy" || kh.Port != 22 ||
		kh.IdentityFile != "~/.ssh/id_ed25519" || kh.Auth != "key" || kh.ConfigFile != filepath.Join(sshDir, "config") {
		t.Errorf("keyhost JSON = %+v", kh)
	}

	// 2. pwhost
	ph, ok := byAlias["pwhost"]
	if !ok || ph.HostName != "pw.lan" || ph.User != "admin" || ph.Port != 2222 ||
		ph.Auth != "password" || ph.ConfigFile != filepath.Join(sshDir, "config") {
		t.Errorf("pwhost JSON = %+v", ph)
	}

	// 3. defaulthost
	dh, ok := byAlias["defaulthost"]
	if !ok || dh.HostName != "def.lan" || dh.Auth != "default" || dh.ConfigFile != filepath.Join(sshDir, "config") {
		t.Errorf("defaulthost JSON = %+v", dh)
	}

	// 4. inchost from included file
	ih, ok := byAlias["inchost"]
	if !ok || ih.HostName != "inc.lan" || ih.User != "worker" || ih.Port != 2200 ||
		ih.ConfigFile != extraPath {
		t.Errorf("inchost JSON = %+v", ih)
	}
}

func TestLsJSONEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	var buf bytes.Buffer
	code := cmdLs([]string{"--json"}, &buf, tmpDir)
	if code != 0 {
		t.Fatalf("cmdLs --json exited with %d, want 0", code)
	}
	if strings.TrimSpace(buf.String()) != "[]" {
		t.Errorf("expected empty array '[]', got %q", buf.String())
	}
}

func TestLsUsage(t *testing.T) {
	tmpDir := t.TempDir()
	if code := run([]string{"ls", "--bogus"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for ls --bogus, got %d", code)
	}
	if code := run([]string{"ls", "extra"}, tmpDir); code != 2 {
		t.Errorf("expected exit code 2 for ls extra, got %d", code)
	}
}

func TestAddCLIWithMultipleAliases(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// Answers in ACCESSIBLE mode:
	// HostName: keep default (192.168.1.50)
	// Aliases: keep default (srv1 srv2 192.168.1.50)
	// User: keep default
	// Port: keep default
	// ProxyJump: keep default
	// Auth: 2 (password)
	// Save: y
	in := strings.Join([]string{"", "", "", "", "", "2", "y"}, "\n") + "\n"
	wizard.SetIO(strings.NewReader(in), io.Discard)
	defer wizard.ResetIO()

	// Run `sshx add 192.168.1.50 srv1 srv2`
	code := run([]string{"add", "192.168.1.50", "srv1", "srv2"}, tmpDir)
	if code != 0 {
		t.Fatalf("run add exited with %d, want 0", code)
	}

	hosts, err := sshconfig.LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	for _, expected := range []string{"srv1", "srv2", "192.168.1.50"} {
		if !slices.Contains(h.AllAliases, expected) {
			t.Errorf("expected %q in AllAliases %v", expected, h.AllAliases)
		}
	}
}
