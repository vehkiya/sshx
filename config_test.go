package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadAllHosts(t *testing.T) {
	tmpDir := t.TempDir()

	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	configContent := `
Host fortress 10.10.1.218
    HostName fortress.kerrlab.app
    User vehkiya
    Port 2222
    IdentityFile ~/.ssh/id_ed25519_synology

Host router
    HostName 192.168.1.1
    User root
    PubkeyAuthentication no
    PreferredAuthentications password,keyboard-interactive

# Wildcard defaults
Host *
    AddKeysToAgent yes
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	hosts, err := LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatalf("LoadAllHosts failed: %v", err)
	}

	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	// Hosts are sorted alphabetically: fortress, router
	if hosts[0].Alias != "fortress" {
		t.Errorf("expected first host 'fortress', got %q", hosts[0].Alias)
	}
	if hosts[0].Port != 2222 {
		t.Errorf("expected port 2222, got %d", hosts[0].Port)
	}
	if !hosts[0].PubkeyAuth {
		t.Errorf("expected PubkeyAuth true for fortress")
	}

	if hosts[1].Alias != "router" {
		t.Errorf("expected second host 'router', got %q", hosts[1].Alias)
	}
	if hosts[1].PubkeyAuth {
		t.Errorf("expected PubkeyAuth false for router")
	}
	if !hosts[1].PasswordAuth {
		t.Errorf("expected PasswordAuth true for router")
	}
}

func TestInsertAndRemoveHost(t *testing.T) {
	initial := `Host alpha
    HostName alpha.lan

Host *
    AddKeysToAgent yes
`
	newEntry := HostEntry{
		Alias:    "beta",
		HostName: "beta.lan",
		User:     "admin",
	}

	withBeta := InsertHost(initial, newEntry.Format())
	if !HasHost(withBeta, "beta") {
		t.Errorf("expected 'beta' to be present")
	}

	// Check beta is before Host *
	betaIdx := strings.Index(withBeta, "Host beta")
	wildIdx := strings.Index(withBeta, "Host *")
	if betaIdx >= wildIdx {
		t.Errorf("expected beta before Host *")
	}

	stripped := RemoveHost(withBeta, "alpha")
	if HasHost(stripped, "alpha") {
		t.Errorf("expected alpha to be removed")
	}
	if !HasHost(stripped, "beta") {
		t.Errorf("expected beta to be preserved")
	}
}

func TestResolveAndDeleteHostFromConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	configDDir := filepath.Join(sshDir, "config.d")
	if err := os.MkdirAll(configDDir, 0700); err != nil {
		t.Fatal(err)
	}

	mainConfig := filepath.Join(sshDir, "config")
	if err := os.WriteFile(mainConfig, []byte("Host mainhost\n    HostName main.lan\n"), 0600); err != nil {
		t.Fatal(err)
	}

	workConfig := filepath.Join(configDDir, "work.conf")
	workContent := "Host workhost 10.0.0.1\n    HostName work.lan\n\nHost sharednode\n    HostName shared.lan\n"
	if err := os.WriteFile(workConfig, []byte(workContent), 0600); err != nil {
		t.Fatal(err)
	}

	// 1. Resolve host in main config
	file, err := ResolveHostConfigFile("mainhost", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error resolving mainhost: %v", err)
	}
	if file != mainConfig {
		t.Errorf("expected %s, got %s", mainConfig, file)
	}

	// 2. Resolve primary alias in sub-config
	file, err = ResolveHostConfigFile("workhost", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error resolving workhost: %v", err)
	}
	if file != workConfig {
		t.Errorf("expected %s, got %s", workConfig, file)
	}

	// 3. Resolve secondary alias in sub-config
	file, err = ResolveHostConfigFile("10.0.0.1", tmpDir)
	if err != nil {
		t.Fatalf("unexpected error resolving secondary alias 10.0.0.1: %v", err)
	}
	if file != workConfig {
		t.Errorf("expected %s, got %s", workConfig, file)
	}

	// 4. Resolve nonexistent host returns error
	_, err = ResolveHostConfigFile("nonexistent", tmpDir)
	if err == nil {
		t.Errorf("expected error resolving nonexistent host, got nil")
	}

	// 5. Delete host from sub-config
	if err := DeleteHostFromConfigFile("workhost", workConfig); err != nil {
		t.Fatalf("failed to delete workhost from %s: %v", workConfig, err)
	}

	// 6. Verify workhost is removed from sub-config, but sharednode remains
	updatedData, err := os.ReadFile(filepath.Clean(workConfig)) //nolint:gosec // test file read
	if err != nil {
		t.Fatal(err)
	}
	if HasHost(string(updatedData), "workhost") {
		t.Errorf("workhost was not deleted from sub-config")
	}
	if !HasHost(string(updatedData), "sharednode") {
		t.Errorf("sharednode should have been preserved in sub-config")
	}

	// 7. Verify file permissions remain 0600
	fi, err := os.Stat(workConfig)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("expected 0600 permissions, got %o", fi.Mode().Perm())
	}
}

func TestHostEntryFormatWithProxyJump(t *testing.T) {
	entry := HostEntry{
		Alias:        "internal-srv",
		HostName:     "10.0.1.50",
		User:         "ubuntu",
		Port:         22,
		ProxyJump:    "bastion.example.com",
		IdentityFile: "~/.ssh/id_ed25519",
	}

	formatted := entry.Format()
	if !strings.Contains(formatted, "ProxyJump bastion.example.com") {
		t.Errorf("expected ProxyJump in formatted block, got:\n%s", formatted)
	}
	if !strings.Contains(formatted, "Host internal-srv") {
		t.Errorf("expected Host header, got:\n%s", formatted)
	}
}

func TestLoadAllHostsWithNotes(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	configContent := `# Production Database Cluster
# Primary PostgreSQL node
Host db-prod
    HostName db.example.internal
    User postgres
`
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	hosts, err := LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatalf("LoadAllHosts failed: %v", err)
	}
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	if !strings.Contains(hosts[0].Notes, "Production Database Cluster") {
		t.Errorf("expected notes to contain 'Production Database Cluster', got %q", hosts[0].Notes)
	}
	if !strings.Contains(hosts[0].FilterValue(), "PostgreSQL") {
		t.Errorf("expected FilterValue to contain 'PostgreSQL', got %q", hosts[0].FilterValue())
	}
}
