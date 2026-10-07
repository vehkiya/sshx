package sshconfig

import (
	"os"
	"path/filepath"
	"runtime"
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
	if runtime.GOOS == "windows" {
		return
	}
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

func TestParseTargetIPv6(t *testing.T) {
	// 1. IPv6 bracketed with port
	u, h, p := ParseTarget("root@[2001:db8::1]:8022")
	if u != "root" || h != "2001:db8::1" || p != 8022 {
		t.Errorf("expected root, 2001:db8::1, 8022, got %q, %q, %d", u, h, p)
	}

	// 2. IPv6 bracketed without port
	u, h, p = ParseTarget("[2001:db8::1]")
	if u != "" || h != "2001:db8::1" || p != 0 {
		t.Errorf("expected empty, 2001:db8::1, 0, got %q, %q, %d", u, h, p)
	}

	// 3. IPv6 raw without brackets
	u, h, p = ParseTarget("2001:db8::1")
	if u != "" || h != "2001:db8::1" || p != 0 {
		t.Errorf("expected empty, 2001:db8::1, 0, got %q, %q, %d", u, h, p)
	}

	// 4. Standard host with port
	u, h, p = ParseTarget("admin@10.0.0.1:2222")
	if u != "admin" || h != "10.0.0.1" || p != 2222 {
		t.Errorf("expected admin, 10.0.0.1, 2222, got %q, %q, %d", u, h, p)
	}
}

func TestHasHostAndRemoveHostCaseInsensitive(t *testing.T) {
	config := `Host ProdServer
    HostName 10.0.0.1
    User ubuntu

Host db-backup
    HostName 10.0.0.2
`
	if !HasHost(config, "prodserver") {
		t.Errorf("expected case-insensitive HasHost match for prodserver")
	}
	if !HasHost(config, "PRODSERVER") {
		t.Errorf("expected case-insensitive HasHost match for PRODSERVER")
	}

	cleaned := RemoveHost(config, "prodserver")
	if HasHost(cleaned, "prodserver") {
		t.Errorf("expected ProdServer to be removed")
	}
	if !HasHost(cleaned, "db-backup") {
		t.Errorf("expected db-backup to be preserved")
	}
}

func TestFindConfigFilesWithInclude(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	customDir := filepath.Join(sshDir, "custom")
	if err := os.MkdirAll(customDir, 0700); err != nil {
		t.Fatal(err)
	}

	mainConfig := filepath.Join(sshDir, "config")
	mainContent := `Include ~/.ssh/custom/*.conf
Host main-node
    HostName main.lan
`
	if err := os.WriteFile(mainConfig, []byte(mainContent), 0600); err != nil {
		t.Fatal(err)
	}

	customConfig := filepath.Join(customDir, "work.conf")
	if err := os.WriteFile(customConfig, []byte("Host work-node\n    HostName work.lan\n"), 0600); err != nil {
		t.Fatal(err)
	}

	files := FindConfigFiles(tmpDir)
	foundCustom := false
	for _, f := range files {
		if f == customConfig {
			foundCustom = true
			break
		}
	}
	if !foundCustom {
		t.Errorf("expected customConfig to be found via Include directive, got files: %v", files)
	}

	hosts, err := LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Errorf("expected 2 hosts (main-node and work-node), got %d", len(hosts))
	}
}

func TestLoadAllHostsMultipleWithComments(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}

	configContent := `Host srv1
    HostName srv1.internal

# Production Database Cluster
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
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}

	var dbHost, srvHost *HostItem
	for i := range hosts {
		switch hosts[i].Alias {
		case "db-prod":
			dbHost = &hosts[i]
		case "srv1":
			srvHost = &hosts[i]
		}
	}

	if dbHost == nil || srvHost == nil {
		t.Fatalf("could not find db-prod or srv1")
	}

	if srvHost.Notes != "" {
		t.Errorf("expected srv1 to have empty notes, got %q", srvHost.Notes)
	}
	if !strings.Contains(dbHost.Notes, "Production Database Cluster") {
		t.Errorf("expected dbHost to contain 'Production Database Cluster', got %q", dbHost.Notes)
	}
	rawDb := strings.Join(dbHost.RawLines, "\n")
	if !strings.Contains(rawDb, "# Production Database Cluster") {
		t.Errorf("expected rawDb to contain comments, got:\n%s", rawDb)
	}
}

const sharedConfig = `Host fortress 10.10.1.218 *.fort.lan
    HostName fortress.example
    User admin
    ForwardAgent yes
    LocalForward 8080 localhost:80

# Notes for beta: primary DB
Host beta
    HostName beta.lan

# Wildcard defaults
Host *
    AddKeysToAgent yes
`

func TestParseDirective(t *testing.T) {
	tests := []struct {
		line, key, value string
	}{
		{"    HostName example.com", "hostname", "example.com"},
		{"HostName=example.com", "hostname", "example.com"},
		{"  User = bob  ", "user", "bob"},
		{"Port\t2222", "port", "2222"},
		{"IdentityFile \"~/My Keys/id\"", "identityfile", "\"~/My Keys/id\""},
		{"Host", "host", ""},
		{"   # comment", "", ""},
		{"", "", ""},
	}
	for _, tc := range tests {
		key, value := parseDirective(tc.line)
		if key != tc.key || value != tc.value {
			t.Errorf("parseDirective(%q) = (%q, %q); expected (%q, %q)", tc.line, key, value, tc.key, tc.value)
		}
	}
}

func TestParseHostsSyntaxAndPrecedence(t *testing.T) {
	content := `Host eq
    HostName=eq.example
    Port = 2200
    User "bob smith"
    User ignored
    IdentityFile "~/My Keys/id"
`
	hosts := parseHosts(content, "config")
	if len(hosts) != 1 {
		t.Fatalf("expected 1 host, got %d", len(hosts))
	}
	h := hosts[0]
	if h.HostName != "eq.example" || h.Port != 2200 || h.User != "bob smith" || h.IdentityFile != "~/My Keys/id" {
		t.Errorf("unexpected parse result: %+v", h)
	}
}

func TestParseHostsNotesAndSectionBounds(t *testing.T) {
	content := `# File header, not a note

# Web tier
Host web
    HostName web.lan
# Port 2222 was retired
    User deploy

Host db
    HostName db.lan
`
	hosts := parseHosts(content, "config")
	if len(hosts) != 2 {
		t.Fatalf("expected 2 hosts, got %d", len(hosts))
	}
	web := hosts[0]
	if web.Notes != "Web tier" {
		t.Errorf("expected notes 'Web tier', got %q", web.Notes)
	}
	if web.User != "deploy" {
		t.Errorf("expected directive after an unindented comment to stay in the section, got user %q", web.User)
	}
	if hosts[1].Notes != "" {
		t.Errorf("expected no notes for db, got %q", hosts[1].Notes)
	}
}

func TestRemoveHostKeepsOtherSections(t *testing.T) {
	out := RemoveHost(sharedConfig, "fortress 10.10.1.218")

	for _, want := range []string{"Host *.fort.lan", "ForwardAgent yes", "# Notes for beta: primary DB", "Host beta", "# Wildcard defaults"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q to survive removal, got:\n%s", want, out)
		}
	}
	if HasHost(out, "fortress") || HasHost(out, "10.10.1.218") {
		t.Errorf("expected fortress aliases to be removed, got:\n%s", out)
	}
}

func TestRemoveHostDropsSectionAndNotes(t *testing.T) {
	out := RemoveHost(sharedConfig, "beta")
	if strings.Contains(out, "Notes for beta") || HasHost(out, "beta") {
		t.Errorf("expected beta and its notes to be removed, got:\n%s", out)
	}
	if !strings.Contains(out, "LocalForward 8080 localhost:80\n\n# Wildcard defaults\nHost *") {
		t.Errorf("expected neighbouring sections to stay intact, got:\n%s", out)
	}

	single := RemoveHost("Host only\n    HostName only.lan\n\nHost next\n    HostName next.lan\n", "only")
	if single != "Host next\n    HostName next.lan\n" {
		t.Errorf("unexpected result removing the first section: %q", single)
	}
}

func TestRemoveHostSingleAliasOfShared(t *testing.T) {
	out := RemoveHost(sharedConfig, "10.10.1.218")
	if !strings.Contains(out, "Host fortress *.fort.lan\n    HostName fortress.example") {
		t.Errorf("expected only the 10.10.1.218 alias to be removed, got:\n%s", out)
	}
}

func TestUpdateHostNoOpIsIdentity(t *testing.T) {
	prev := parseHosts(sharedConfig, "config")[0].Entry()
	out, err := UpdateHost(sharedConfig, "fortress", prev, prev)
	if err != nil {
		t.Fatal(err)
	}
	if out != sharedConfig {
		t.Errorf("expected unchanged content, got:\n%s", out)
	}
}

func TestUpdateHostEditsInPlace(t *testing.T) {
	prev := parseHosts(sharedConfig, "config")[0].Entry()
	next := prev
	next.Alias = "fort"
	next.HostName = "new.example"
	next.User = ""
	next.Port = 2222
	next.ProxyJump = "bastion"

	out, err := UpdateHost(sharedConfig, "fortress", prev, next)
	if err != nil {
		t.Fatal(err)
	}
	expected := `Host fort 10.10.1.218 *.fort.lan
    HostName new.example
    ForwardAgent yes
    LocalForward 8080 localhost:80
    Port 2222
    ProxyJump bastion

# Notes for beta: primary DB
Host beta
    HostName beta.lan

# Wildcard defaults
Host *
    AddKeysToAgent yes
`
	if out != expected {
		t.Errorf("unexpected update result:\n%s\nexpected:\n%s", out, expected)
	}
}

func TestUpdateHostSwitchesAuth(t *testing.T) {
	content := "Host k\n    HostName k.lan\n    IdentityFile ~/.ssh/id_k\n    IdentitiesOnly yes\n"
	prev := parseHosts(content, "config")[0].Entry()
	next := prev
	next.IdentityFile = ""
	next.IdentitiesOnly = false
	next.PubkeyAuth = false
	next.PasswordAuth = true

	out, err := UpdateHost(content, "k", prev, next)
	if err != nil {
		t.Fatal(err)
	}
	expected := "Host k\n    HostName k.lan\n    PubkeyAuthentication no\n    PreferredAuthentications password,keyboard-interactive\n"
	if out != expected {
		t.Errorf("unexpected auth switch result:\n%q\nexpected:\n%q", out, expected)
	}

	if _, err := UpdateHost(content, "missing", prev, next); err == nil {
		t.Errorf("expected an error for a missing host")
	}
}

func TestUpdateHostKeepsCRLF(t *testing.T) {
	content := "Host w\r\n    HostName w.lan\r\n"
	prev := parseHosts(content, "config")[0].Entry()
	next := prev
	next.HostName = "w2.lan"
	next.User = "ops"
	out, err := UpdateHost(content, "w", prev, next)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Host w\r\n    HostName w2.lan\r\n    User ops\r\n" {
		t.Errorf("expected CRLF line endings to be kept, got %q", out)
	}
}

func TestCloneHostCopiesUnmanagedDirectives(t *testing.T) {
	prev := parseHosts(sharedConfig, "config")[0].Entry()
	next := prev
	next.Alias = "fortress-2"
	next.HostName = "fortress2.example"

	block := CloneHost(sharedConfig, prev, next)
	expected := "Host fortress-2\n    HostName fortress2.example\n    User admin\n    ForwardAgent yes\n    LocalForward 8080 localhost:80\n"
	if block != expected {
		t.Errorf("unexpected clone:\n%s\nexpected:\n%s", block, expected)
	}
}

func TestInsertHostBeforeWildcardNotes(t *testing.T) {
	content := "Host a\n    HostName a.lan\n    # retired: Port 2222\n# Wildcard defaults\nHost *\n    AddKeysToAgent yes\n"
	out := InsertHost(content, "Host b\n    HostName b.lan\n")
	expected := "Host a\n    HostName a.lan\n    # retired: Port 2222\n\nHost b\n    HostName b.lan\n\n# Wildcard defaults\nHost *\n    AddKeysToAgent yes\n"
	if out != expected {
		t.Errorf("unexpected insert result:\n%q\nexpected:\n%q", out, expected)
	}
}

func TestHostEntryFormatQuotesSpaces(t *testing.T) {
	formatted := HostEntry{Alias: "q", IdentityFile: "~/My Keys/id", PubkeyAuth: true}.Format()
	if !strings.Contains(formatted, `IdentityFile "~/My Keys/id"`) {
		t.Errorf("expected quoted IdentityFile, got:\n%s", formatted)
	}
}

func TestWriteConfigFileBacksUpAndFollowsSymlinks(t *testing.T) {
	dir := t.TempDir()
	config := filepath.Join(dir, "config")
	if err := os.WriteFile(config, []byte("Host old\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := WriteConfigFile(config, []byte("Host new\n")); err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(BackupPath(config)) //nolint:gosec // test file read
	if err != nil || string(backup) != "Host old\n" {
		t.Errorf("expected backup with previous contents, got %q (err %v)", backup, err)
	}

	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks requires extra privileges on Windows")
	}
	target := filepath.Join(dir, "dotfiles-config")
	if err := os.WriteFile(target, []byte("Host linked\n"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked-config")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := WriteConfigFile(link, []byte("Host updated\n")); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("expected %s to remain a symlink", link)
	}
	data, err := os.ReadFile(target) //nolint:gosec // test file read
	if err != nil || string(data) != "Host updated\n" {
		t.Errorf("expected write to go through the symlink, got %q (err %v)", data, err)
	}
}

func TestFindConfigFilesNestedIncludes(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	extraDir := filepath.Join(sshDir, "extra")
	if err := os.MkdirAll(extraDir, 0700); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(sshDir, "config"), "Include=work.conf extra/*.conf\n")
	write(filepath.Join(sshDir, "work.conf"), "Include nested.conf\nHost work\n")
	write(filepath.Join(sshDir, "nested.conf"), "Host nested\n")
	write(filepath.Join(extraDir, "a.conf"), "Host extra-a\n")
	write(filepath.Join(extraDir, ".hidden.conf"), "Host hidden\n")

	hosts, err := LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	var aliases []string
	for _, h := range hosts {
		aliases = append(aliases, h.Alias)
	}
	if strings.Join(aliases, ",") != "extra-a,nested,work" {
		t.Errorf("expected hosts extra-a,nested,work, got %v", aliases)
	}
}

func TestFindAliasOwner(t *testing.T) {
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".ssh", "config.d")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	mainConfig := filepath.Join(tmpDir, ".ssh", "config")
	workConfig := filepath.Join(configDir, "work")
	if err := os.WriteFile(mainConfig, []byte("Host a b\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(workConfig, []byte("Host web\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if file, ok := FindAliasOwner(tmpDir, "web", "", ""); !ok || file != workConfig {
		t.Errorf("expected web in %s, got %q (found %v)", workConfig, file, ok)
	}
	if _, ok := FindAliasOwner(tmpDir, "b", mainConfig, "a"); ok {
		t.Errorf("expected the skipped section not to count as a conflict")
	}
	if _, ok := FindAliasOwner(tmpDir, "missing", "", ""); ok {
		t.Errorf("expected no owner for a missing alias")
	}
}

func TestShortenHome(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "al")
	inside := filepath.Join(home, ".ssh", "id_ed25519")
	sibling := filepath.Join(string(filepath.Separator)+"home", "alice", ".ssh", "id")

	if got := shortenHome(inside, home); got != "~/.ssh/id_ed25519" {
		t.Errorf("shortenHome(%q) = %q", inside, got)
	}
	if got := shortenHome(sibling, home); got != sibling {
		t.Errorf("expected a sibling home directory to be left alone, got %q", got)
	}
}

func TestHostSection(t *testing.T) {
	section := HostSection(sharedConfig, "beta")
	if section != "Host beta\n    HostName beta.lan" {
		t.Errorf("unexpected section: %q", section)
	}
	if !strings.HasPrefix(HostSection(sharedConfig, "10.10.1.218"), "Host fortress 10.10.1.218") {
		t.Errorf("expected lookup by secondary alias to find the fortress section")
	}
}

func FuzzInsertRemoveHost(f *testing.F) {
	f.Add(sharedConfig)
	f.Add("Host a\n    HostName a\n# trailing\n")
	f.Add("Include config.d/*\n\nMatch host x\n    User y\n\nHost *\n    ServerAliveInterval 30\n")
	f.Add("Host a\r\n    User b\r\n")

	const alias = "sshx-fuzz-host"
	f.Fuzz(func(t *testing.T, content string) {
		if strings.Contains(strings.ToLower(content), alias) {
			t.Skip()
		}
		inserted := InsertHost(content, HostEntry{Alias: alias, HostName: "fuzz.lan", PubkeyAuth: true}.Format())
		if !HasHost(inserted, alias) {
			t.Fatalf("inserted host not found in:\n%q", inserted)
		}
		removed := RemoveHost(inserted, alias)
		if HasHost(removed, alias) {
			t.Fatalf("host still present after removal:\n%q", removed)
		}
		if got, want := nonBlankLines(removed), nonBlankLines(content); got != want {
			t.Fatalf("insert+remove changed content\n got: %q\nwant: %q", got, want)
		}
	})
}

func nonBlankLines(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
