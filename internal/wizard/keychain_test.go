package wizard

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// writeHome writes files (relative to a new home folder) and returns it.
func writeHome(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for name, content := range files {
		path := filepath.Join(home, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// needTool skips a test when a tool it runs for real isn't installed,
// or when a tool is unsupported/prone to console hangs in headless CI.
func needTool(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "windows" && name == "ssh-keygen" {
		t.Skip("ssh-keygen interactive prompts hang in headless Windows environments")
	}
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s isn't installed", name)
	}
}

// newKey generates an Ed25519 key in home's .ssh with ssh-keygen, protected
// by passphrase unless it's empty, and returns its private path.
func newKey(t *testing.T, home, name, passphrase string) string {
	t.Helper()
	needTool(t, "ssh-keygen")
	key := filepath.Join(home, ".ssh", name)
	if err := os.MkdirAll(filepath.Dir(key), 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ssh-keygen", "-q", "-t", "ed25519", "-N", passphrase, "-C", "test", "-f", key).CombinedOutput() //nolint:gosec // test key in a temp folder
	if err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}
	return key
}

func TestUsesKeychain(t *testing.T) {
	cases := []struct {
		name, config string
		files        map[string]string // other files under ~/.ssh
		host         string
		want         bool
	}{
		{"every host", "Host *\n  UseKeychain yes\n", nil, "web", true},
		{"no setting", "Host *\n  AddKeysToAgent yes\n", nil, "web", false},
		{"before any Host", "UseKeychain=yes\nHost x\n", nil, "web", true},
		{"another host only", "Host db\n  UseKeychain yes\n", nil, "web", false},
		{"wildcard", "Host w*\n\tusekeychain true\n", nil, "web", true},
		{"ignores case", "HOST Web\n  UseKeychain yes\n", nil, "web", true},
		{"several patterns", "Host db web\n  UseKeychain yes\n", nil, "web", true},
		{"negated", "Host * !web\n  UseKeychain yes\n", nil, "web", false},
		{"first value wins", "Host web\n  UseKeychain no\nHost *\n  UseKeychain yes\n", nil, "web", false},
		{"comment", "Host *\n  # UseKeychain yes\n", nil, "web", false},
		{"quoted", "Host \"web\"\n  UseKeychain \"yes\"\n", nil, "web", true},
		{"Match is taken to apply", "Match exec \"true\"\n  UseKeychain yes\n", nil, "web", true},
		{"include", "Include config.d/*\n", map[string]string{"config.d/mac": "Host *\n  UseKeychain yes\n"}, "web", true},
		{"include from home", "Include ~/.ssh/mac.conf\n", map[string]string{"mac.conf": "UseKeychain yes\n"}, "web", true},
		{"include in another host's block", "Host db\n  Include mac.conf\n", map[string]string{"mac.conf": "UseKeychain yes\n"}, "web", false},
		{"include in this host's block", "Host web\n  Include mac.conf\n", map[string]string{"mac.conf": "UseKeychain yes\n"}, "web", true},
		{"include loop", "Include config\n", nil, "web", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := map[string]string{".ssh/config": c.config}
			for name, content := range c.files {
				files[".ssh/"+name] = content
			}
			home := writeHome(t, files)
			if got := usesKeychain(filepath.Join(home, ".ssh", "config"), c.host); got != c.want {
				t.Errorf("usesKeychain = %v, want %v", got, c.want)
			}
		})
	}
	if usesKeychain(filepath.Join(t.TempDir(), ".ssh", "config"), "web") {
		t.Error("usesKeychain without a config = true")
	}
}

// Real ssh reads AddKeysToAgent from the config, as -G prints it.
func TestAddsKeysToAgent(t *testing.T) {
	needTool(t, "ssh")
	home := writeHome(t, map[string]string{
		".ssh/config": "Host web\n  AddKeysToAgent yes\nHost db\n  AddKeysToAgent confirm 5m\nHost lab\n  AddKeysToAgent no\n",
	})
	cfg := filepath.Join(home, ".ssh", "config")
	for host, want := range map[string]bool{"web": true, "db": true, "lab": false, "other": false} {
		if got := addsKeysToAgent(cfg, host); got != want {
			t.Errorf("addsKeysToAgent(%s) = %v, want %v", host, got, want)
		}
	}
	if addsKeysToAgent(filepath.Join(home, ".ssh", "missing"), "web") {
		t.Error("addsKeysToAgent without a config = true")
	}
}

func TestKeychainSupported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	// fakeSSH puts an ssh first on PATH that accepts UseKeychain or not.
	fakeSSH := func(accepts bool) {
		bin := t.TempDir()
		script := "#!/bin/sh\nexit 255\n"
		if accepts {
			script = "#!/bin/sh\ncase \"$*\" in *UseKeychain=yes*) exit 0 ;; esac\nexit 255\n"
		}
		if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o700); err != nil { //nolint:gosec // the fake must be executable
			t.Fatal(err)
		}
		t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	fakeSSH(true)
	if got, want := keychainSupported(), runtime.GOOS == "darwin"; got != want {
		t.Errorf("keychainSupported on %s with an ssh that accepts UseKeychain = %v, want %v", runtime.GOOS, got, want)
	}
	fakeSSH(false)
	if keychainSupported() {
		t.Error("keychainSupported = true with an ssh that refuses UseKeychain (Homebrew's)")
	}
}

func TestHasPassphrase(t *testing.T) {
	home := t.TempDir()
	if !hasPassphrase(newKey(t, home, "id_locked", "secret")) {
		t.Error("hasPassphrase(protected key) = false")
	}
	if hasPassphrase(newKey(t, home, "id_open", "")) {
		t.Error("hasPassphrase(key without a passphrase) = true")
	}
	if hasPassphrase(filepath.Join(home, ".ssh", "id_missing")) {
		t.Error("hasPassphrase(missing key) = true")
	}
}

// keychainStub stands in for the Mac: supported says whether the Keychain
// can be used, answer is the reply to the offer, and the returned slices
// collect the questions asked and the keys sent to the Keychain.
type keychainStub struct {
	asked, added []string
	answer       bool
}

func stubKeychain(t *testing.T, supported, answer bool) *keychainStub {
	t.Helper()
	s := &keychainStub{answer: answer}
	oldSupported, oldAdd := keychainSupported, addToKeychain
	t.Cleanup(func() { keychainSupported, addToKeychain = oldSupported, oldAdd })
	keychainSupported = func() bool { return supported }
	addToKeychain = func(key string) error {
		s.added = append(s.added, key)
		return nil
	}
	return s
}

func (s *keychainStub) ask(title, _ string) bool {
	s.asked = append(s.asked, title)
	return s.answer
}

func TestRememberPassphraseOffersTheKeychain(t *testing.T) {
	needTool(t, "ssh")
	cases := []struct {
		name, config string
		wantHint     string // the ~/.ssh/config advice, or "" for none
	}{
		{"config sets neither", "Host web\n  HostName web.lan\n", "add UseKeychain yes and AddKeysToAgent yes under `Host *`"},
		{"config lacks AddKeysToAgent", "IgnoreUnknown UseKeychain\nHost *\n  UseKeychain yes\n", "add AddKeysToAgent yes under `Host *`"},
		// IgnoreUnknown lets ssh builds without UseKeychain read the file,
		// as many dotfile setups shared with Linux do.
		{"config sets both", "IgnoreUnknown UseKeychain\nHost *\n  UseKeychain yes\n  AddKeysToAgent yes\n", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			home := writeHome(t, map[string]string{".ssh/config": c.config})
			key := newKey(t, home, "id_ed25519_web", "secret")
			short := shortenHome(key, home)
			s := stubKeychain(t, true, true)

			var out bytes.Buffer
			rememberPassphrase(&out, key, "web", home, s.ask)
			if want := []string{"Keep the passphrase of " + short + " in your macOS Keychain?"}; !slices.Equal(s.asked, want) {
				t.Errorf("asked %q, want %q", s.asked, want)
			}
			if !slices.Equal(s.added, []string{key}) {
				t.Errorf("sent %q to the Keychain, want the new key", s.added)
			}
			if !strings.Contains(out.String(), "The passphrase of "+short+" is in your Keychain, and the key is in your agent") {
				t.Errorf("doesn't confirm the Keychain:\n%s", out.String())
			}
			if c.wantHint == "" {
				if strings.Contains(out.String(), "~/.ssh/config") {
					t.Errorf("gives config advice the config doesn't need:\n%s", out.String())
				}
			} else if !strings.Contains(out.String(), c.wantHint) {
				t.Errorf("config advice missing %q:\n%s", c.wantHint, out.String())
			}
		})
	}
}

func TestRememberPassphraseDeclined(t *testing.T) {
	home := t.TempDir()
	key := newKey(t, home, "id_ed25519_web", "secret")
	s := stubKeychain(t, true, false)

	var out bytes.Buffer
	rememberPassphrase(&out, key, "web", home, s.ask)
	if len(s.asked) != 1 || len(s.added) != 0 {
		t.Errorf("asked %q, sent %q to the Keychain; want one question and nothing sent", s.asked, s.added)
	}
	if want := "To keep it later: ssh-add --apple-use-keychain " + shortenHome(key, home); !strings.Contains(out.String(), want) {
		t.Errorf("output missing %q:\n%s", want, out.String())
	}
}

// Without Apple's ssh there's no Keychain to offer, so sshx says how to load
// the key into the agent instead.
func TestRememberPassphraseWithoutTheKeychain(t *testing.T) {
	home := t.TempDir()
	key := newKey(t, home, "id_ed25519_web", "secret")
	s := stubKeychain(t, false, true)

	var out bytes.Buffer
	rememberPassphrase(&out, key, "web", home, s.ask)
	if len(s.asked) != 0 || len(s.added) != 0 {
		t.Errorf("asked %q, sent %q to the Keychain; want neither", s.asked, s.added)
	}
	short := shortenHome(key, home)
	if want := "Load " + short + " into your agent so ssh doesn't ask for its passphrase on every connection: ssh-add " + short; !strings.Contains(out.String(), want) {
		t.Errorf("output missing %q:\n%s", want, out.String())
	}
	if strings.Contains(out.String(), "Keychain") {
		t.Errorf("mentions the Keychain without one:\n%s", out.String())
	}
}

// A key without a passphrase, or no new key at all, needs no help.
func TestRememberPassphraseSaysNothingWhenNotNeeded(t *testing.T) {
	home := t.TempDir()
	for name, key := range map[string]string{
		"no new key":       "",
		"no passphrase":    newKey(t, home, "id_ed25519_open", ""),
		"key wasn't saved": filepath.Join(home, ".ssh", "id_missing"),
	} {
		t.Run(name, func(t *testing.T) {
			s := stubKeychain(t, true, true)
			var out bytes.Buffer
			rememberPassphrase(&out, key, "web", home, s.ask)
			if out.Len() != 0 || len(s.asked) != 0 || len(s.added) != 0 {
				t.Errorf("asked %q, sent %q, printed:\n%s", s.asked, s.added, out.String())
			}
		})
	}
}

// Only a key the wizard generates gets the offer: an existing key's
// passphrase is the user's business, and they may have stored it already.
func TestGeneratedKeyIsOnlyTheNewKey(t *testing.T) {
	home := keyHome(t)
	existing := filepath.Join(home, ".ssh", "id_ed25519_lab")
	cases := []struct {
		name      string
		auth      string
		keyChoice string
		newKey    string
		want      string
	}{
		{"generated, default path", authKey, keyGenerate, "", filepath.Join(home, ".ssh", "id_ed25519_web")},
		{"generated, chosen path", authKey, keyGenerate, "~/.ssh/id_new", filepath.Join(home, ".ssh", "id_new")},
		{"existing key", authKey, existing, "", ""},
		{"custom key", authKey, keyCustom, "", ""},
		{"password", authPassword, keyGenerate, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newHostWizard(wizardAdd, hostFormValues{hostName: "web.lan", alias: "web", user: "me", port: "22", auth: c.auth}, home, "")
			w.keyChoice, w.newKey = c.keyChoice, c.newKey
			if got := w.generatedKey(); got != c.want {
				t.Errorf("generatedKey = %q, want %q", got, c.want)
			}
		})
	}
}
