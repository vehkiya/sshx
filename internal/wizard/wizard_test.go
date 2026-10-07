package wizard

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vehkiya/sshx/internal/sshconfig"
)

func TestBuildEntryUnchangedFormIsNoOp(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "u")
	keyPath := filepath.Join(home, ".ssh", "id_ed25519")

	tests := []HostEntry{
		{Alias: "key", HostName: "k.lan", Port: 22, IdentityFile: "~/.ssh/id_ed25519", PubkeyAuth: true},
		{Alias: "abs", HostName: "a.lan", Port: 2222, IdentityFile: keyPath, IdentitiesOnly: true, PubkeyAuth: true},
		{Alias: "mixed", HostName: "m.lan", Port: 22, PubkeyAuth: true, PasswordAuth: true},
		{Alias: "pw", HostName: "p.lan", User: "root", Port: 22, PasswordAuth: true},
		{Alias: "agent", HostName: "d.lan", Port: 22, PubkeyAuth: true, ProxyJump: "bastion"},
	}
	for _, prev := range tests {
		identity := ""
		if authMethodOf(prev) == authKey {
			// hostWizard.identityFile returns the expanded path of the current key.
			identity = expandHome(prev.IdentityFile, home)
		}
		got := buildEntry(formValuesFromEntry(prev), identity, home, &prev)
		if got != prev {
			t.Errorf("unchanged form for %q produced %+v; expected %+v", prev.Alias, got, prev)
		}
	}
}

func TestBuildEntryAuthChanges(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "u")
	prev := HostEntry{Alias: "h", HostName: "h.lan", Port: 22, PubkeyAuth: true, PasswordAuth: true}

	v := formValuesFromEntry(prev)
	v.auth = authKey
	got := buildEntry(v, filepath.Join(home, ".ssh", "id_new"), home, &prev)
	if got.IdentityFile != "~/.ssh/id_new" || !got.IdentitiesOnly || !got.PubkeyAuth || got.PasswordAuth {
		t.Errorf("unexpected entry after switching to key auth: %+v", got)
	}

	v.auth = authPassword
	v.alias = "  h   h2 "
	got = buildEntry(v, "", home, nil)
	if got.PubkeyAuth || !got.PasswordAuth || got.IdentityFile != "" || got.Alias != "h h2" {
		t.Errorf("unexpected entry for password auth: %+v", got)
	}
}

func TestValidateAlias(t *testing.T) {
	for _, ok := range []string{"prod", "prod prod.lan", "10.0.0.1"} {
		if err := validateAlias(ok); err != nil {
			t.Errorf("validateAlias(%q) = %v; expected nil", ok, err)
		}
	}
	for _, bad := range []string{"", "   ", "*.lan", "web?", "!web", "-oProxyCommand=x", `"quoted"`} {
		if err := validateAlias(bad); err == nil {
			t.Errorf("validateAlias(%q) = nil; expected an error", bad)
		}
	}
	for _, bad := range []string{"two words", "host\x1b]11;rgb:0/0/0\a", "host\n    ProxyCommand evil"} {
		if err := validateHostName(bad); err == nil {
			t.Errorf("validateHostName(%q) = nil; expected an error", bad)
		}
	}
	if err := validateText("root\n    ProxyCommand evil"); err == nil {
		t.Errorf("expected control characters in free-text fields to be rejected")
	}
}

func script(lines ...string) string {
	return strings.Join(lines, "\n") + "\n"
}

func TestAccessibleAddHostWizardPipedStdin(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// Piped answers:
	// HostName: srv1.example.com
	// Alias: srv1
	// User: deploy
	// Port: 2200
	// ProxyJump: (blank)
	// Auth: 2 (Password only)
	// Save: y
	in := script(
		"srv1.example.com",
		"srv1",
		"deploy",
		"2200",
		"",
		"2",
		"y",
	)
	var out bytes.Buffer
	SetIO(strings.NewReader(in), &out)
	defer ResetIO()

	alias, connectNow, err := AddHostWizard("", "", tmpDir, false)
	if err != nil {
		t.Fatalf("AddHostWizard failed: %v", err)
	}
	if alias != "srv1" {
		t.Errorf("alias = %q, want srv1", alias)
	}
	if connectNow {
		t.Errorf("connectNow = true, want false")
	}

	hosts, err := sshconfig.LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := sshconfig.FindHost(hosts, "srv1")
	if !ok {
		t.Fatalf("host srv1 not found in config")
	}
	if h.HostName != "srv1.example.com" || h.User != "deploy" || h.Port != 2200 || !h.PasswordAuth {
		t.Errorf("host = %+v", h)
	}
}

func TestAccessibleAddHostCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// Answers answering "n" to Save:
	in := script(
		"srv2.example.com",
		"srv2",
		"admin",
		"22",
		"",
		"2",
		"n",
	)
	var out bytes.Buffer
	SetIO(strings.NewReader(in), &out)
	defer ResetIO()

	alias, _, err := AddHostWizard("", "", tmpDir, false)
	if err != nil {
		t.Fatalf("expected nil error on cleanly cancelled wizard, got %v", err)
	}
	if alias != "" {
		t.Errorf("expected empty alias on cancel, got %q", alias)
	}

	hosts, _ := sshconfig.LoadAllHosts(tmpDir)
	if _, ok := sshconfig.FindHost(hosts, "srv2"); ok {
		t.Errorf("cancelled host srv2 was written to config")
	}
}

func TestAccessibleEditHostWizardKeepIfEmpty(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(sshDir, "config")
	initialConfig := "Host web\n    HostName web.lan\n    User olduser\n    Port 22\n    PasswordAuthentication yes\n"
	if err := os.WriteFile(configPath, []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// In accessible mode:
	// HostName: empty (keep web.lan)
	// Alias: empty (keep web)
	// User: "newuser" (change)
	// Port: empty (keep 22)
	// ProxyJump: empty (keep empty)
	// Auth: empty (keep Password)
	// Save: y
	in := script(
		"",
		"",
		"newuser",
		"",
		"",
		"",
		"y",
	)
	var out bytes.Buffer
	SetIO(strings.NewReader(in), &out)
	defer ResetIO()

	edited, err := EditHostWizard("web", tmpDir)
	if err != nil {
		t.Fatalf("EditHostWizard failed: %v", err)
	}
	if edited != "web" {
		t.Errorf("edited alias = %q, want web", edited)
	}

	hosts, err := sshconfig.LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := sshconfig.FindHost(hosts, "web")
	if !ok {
		t.Fatalf("host web not found")
	}
	if h.User != "newuser" || h.HostName != "web.lan" || h.Port != 22 {
		t.Errorf("host after edit = %+v", h)
	}
}

func TestAccessibleCloneHostWizard(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(sshDir, "config")
	initialConfig := "Host base\n    HostName base.lan\n    User ubuntu\n    Port 22\n    PasswordAuthentication yes\n"
	if err := os.WriteFile(configPath, []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// Clone prompts:
	// Alias: "cloned"
	// HostName: empty (keep base.lan)
	// User: empty (keep ubuntu)
	// Port: empty (keep 22)
	// ProxyJump: empty
	// Auth: empty (keep password)
	// Save: y
	in := script(
		"cloned",
		"",
		"",
		"",
		"",
		"",
		"y",
	)
	var out bytes.Buffer
	SetIO(strings.NewReader(in), &out)
	defer ResetIO()

	clonedAlias, err := CloneHostWizard("base", tmpDir)
	if err != nil {
		t.Fatalf("CloneHostWizard failed: %v", err)
	}
	if clonedAlias != "cloned" {
		t.Errorf("cloned alias = %q, want cloned", clonedAlias)
	}

	hosts, err := sshconfig.LoadAllHosts(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	h, ok := sshconfig.FindHost(hosts, "cloned")
	if !ok {
		t.Fatalf("host cloned not found in config")
	}
	if h.HostName != "base.lan" || h.User != "ubuntu" || h.Port != 22 {
		t.Errorf("cloned host = %+v", h)
	}
}

func TestAccessibleDeleteHostPrompt(t *testing.T) {
	tmpDir := t.TempDir()
	sshDir := filepath.Join(tmpDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(sshDir, "config")
	initialConfig := "Host todelete\n    HostName d.lan\nHost tokeep\n    HostName k.lan\n"
	if err := os.WriteFile(configPath, []byte(initialConfig), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ACCESSIBLE", "1")

	// 1. Delete confirmed with y
	SetIO(strings.NewReader(script("y")), io.Discard)
	if err := DeleteHostPrompt("todelete", tmpDir); err != nil {
		t.Fatalf("DeleteHostPrompt failed: %v", err)
	}
	hosts, _ := sshconfig.LoadAllHosts(tmpDir)
	if _, ok := sshconfig.FindHost(hosts, "todelete"); ok {
		t.Errorf("host todelete was not deleted")
	}

	// 2. Delete cancelled with n
	SetIO(strings.NewReader(script("n")), io.Discard)
	if err := DeleteHostPrompt("tokeep", tmpDir); err != nil {
		t.Fatalf("DeleteHostPrompt failed: %v", err)
	}
	hosts, _ = sshconfig.LoadAllHosts(tmpDir)
	if _, ok := sshconfig.FindHost(hosts, "tokeep"); !ok {
		t.Errorf("host tokeep should not have been deleted")
	}
	ResetIO()
}
