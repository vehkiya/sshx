package wizard

import (
	"path/filepath"
	"testing"
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
