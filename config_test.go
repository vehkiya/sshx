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
