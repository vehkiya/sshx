package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProbeAddress(t *testing.T) {
	hosts := []HostItem{
		{Alias: "bastion", AllAliases: []string{"bastion"}, HostName: "bastion.example", Port: 2200},
		{Alias: "inner", AllAliases: []string{"inner"}, HostName: "10.0.0.5", Port: 22, ProxyJump: "bastion,other"},
		{Alias: "direct", AllAliases: []string{"direct"}, Port: 22},
		{Alias: "v6", AllAliases: []string{"v6"}, HostName: "[2001:db8::1]", Port: 2222},
		{Alias: "adhoc", AllAliases: []string{"adhoc"}, HostName: "x", ProxyJump: "ops@jump.example:2022"},
	}
	tests := []struct {
		alias, addr, via string
	}{
		{"inner", "bastion.example:2200", "bastion"},
		{"direct", "direct:22", ""},
		{"v6", "[2001:db8::1]:2222", ""},
		{"adhoc", "jump.example:2022", "ops@jump.example:2022"},
	}
	for _, tc := range tests {
		h, _ := FindHost(hosts, tc.alias)
		addr, via := probeAddress(h, hosts)
		if addr != tc.addr || via != tc.via {
			t.Errorf("probeAddress(%s) = (%q, %q); expected (%q, %q)", tc.alias, addr, via, tc.addr, tc.via)
		}
	}
}

func TestWriteHostTableAlignment(t *testing.T) {
	hosts := []HostItem{
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
