package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
)

// localTimeout bounds the ssh and ssh-keygen calls that only read local
// files, so a hung tool can't freeze sshx.
const localTimeout = 15 * time.Second

// noSuchHost stands for a host no config entry names.
const noSuchHost = "sshx-no-such-host.invalid"

// keychainSupported reports whether passphrases can be kept in the macOS
// Keychain: this is a Mac, and the ssh on PATH is Apple's. Other builds,
// such as Homebrew's, refuse the UseKeychain option. Nothing connects.
var keychainSupported = func() bool {
	if runtime.GOOS != "darwin" {
		return false
	}
	return runLocal("ssh", "-G", "-F", os.DevNull, "-o", "UseKeychain=yes", noSuchHost) == nil
}

// addToKeychain loads key into the agent with Apple's ssh-add and keeps its
// passphrase in the Keychain. ssh-add asks for the passphrase on the
// terminal, so it has no time limit.
var addToKeychain = func(key string) error {
	cmd := exec.Command("ssh-add", "--apple-use-keychain", key) //nolint:gosec // fixed binary; the key sshx just generated
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ssh-add: %w", err)
	}
	return nil
}

// runLocal runs a tool that only reads local files, with a time limit.
func runLocal(name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run() //nolint:gosec // fixed binaries; arguments built by sshx
}

// hasPassphrase reports whether a private key is protected by a passphrase:
// loading it with an empty one only works when it has none.
func hasPassphrase(key string) bool {
	if _, err := os.Stat(key); err != nil {
		return false
	}
	return runLocal("ssh-keygen", "-y", "-P", "", "-f", key) != nil
}

// rememberPassphrase helps ssh use a key sshx just generated without asking
// for its passphrase on every connection. On a Mac with Apple's ssh it
// offers (ask) to keep the passphrase in the Keychain, then says what
// ~/.ssh/config still needs for ssh to read it from there for alias after
// logging in again. Elsewhere it says how to load the key into the agent.
func rememberPassphrase(w io.Writer, key, alias, homeDir string, ask func(title, description string) bool) {
	if key == "" || !hasPassphrase(key) {
		return
	}
	short := shortenHome(key, homeDir)
	dim := lipgloss.NewStyle().Foreground(colorDim)
	if !keychainSupported() {
		_, _ = lipgloss.Fprintf(w, "\n%s\n", dim.Render(fmt.Sprintf(
			"Load %s into your agent so ssh doesn't ask for its passphrase on every connection: ssh-add %s", short, short)))
		return
	}
	if !ask(fmt.Sprintf("Keep the passphrase of %s in your macOS Keychain?", short),
		"Then ssh doesn't ask for it. ssh-add asks for it once more to save it") {
		_, _ = lipgloss.Fprintf(w, "\n%s\n", dim.Render("To keep it later: ssh-add --apple-use-keychain "+short))
		return
	}
	if err := addToKeychain(key); err != nil {
		_, _ = lipgloss.Fprintf(w, "\n%s Couldn't keep the passphrase in the Keychain (%v). Try again with: ssh-add --apple-use-keychain %s\n",
			badge(" KEYCHAIN ", colorBlack, colorAmber), err, short)
		return
	}
	_, _ = lipgloss.Fprintf(w, "\n%s The passphrase of %s is in your Keychain, and the key is in your agent\n",
		badge(" KEYCHAIN ", colorBlack, colorGreen), short)

	// The agent forgets the key when you log out. ssh then reads the
	// passphrase from the Keychain only where UseKeychain applies, and
	// AddKeysToAgent puts the key back in the agent.
	cfg := filepath.Join(homeDir, ".ssh", "config")
	var missing []string
	if !usesKeychain(cfg, alias) {
		missing = append(missing, "UseKeychain yes")
	}
	if !addsKeysToAgent(cfg, alias) {
		missing = append(missing, "AddKeysToAgent yes")
	}
	if len(missing) > 0 {
		_, _ = lipgloss.Fprintf(w, "%s\n", dim.Render(fmt.Sprintf(
			"So ssh still finds it after you log in again, add %s under `Host *` in ~/.ssh/config.", strings.Join(missing, " and "))))
	}
}

// addsKeysToAgent reports whether an ssh config file sets AddKeysToAgent for
// host, so ssh loads a key into the agent once its passphrase is given. ssh
// -G reads the config without connecting, so Include, Match and wildcards
// count as ssh counts them.
func addsKeysToAgent(sshConfig, host string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), localTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ssh", "-G", "-F", sshConfig, host).Output() //nolint:gosec // fixed binary; the user's own config and alias
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "addkeystoagent "); ok {
			v = strings.TrimSpace(v)
			return v != "false" && v != "no"
		}
	}
	return false
}

// usesKeychain reports whether an ssh config file sets UseKeychain for
// host, so Apple's ssh reads key passphrases from the Keychain. ssh -G
// doesn't print UseKeychain, so sshx reads the file itself, as ssh does: the
// first value that applies wins, and Host blocks and Include count. A Match
// block is taken to apply, so an unusual setup isn't reported as missing it.
func usesKeychain(sshConfig, host string) bool {
	v, _ := scanUseKeychain(sshConfig, strings.ToLower(host), filepath.Dir(sshConfig), true, 0)
	return v
}

// scanUseKeychain reads one config file. active says whether the lines
// before the first Host or Match apply; an Include in a block that doesn't
// apply never applies either, as in ssh.
func scanUseKeychain(file, host, sshDir string, active bool, depth int) (value, found bool) {
	if depth > 16 { // ssh's own limit, which also stops Include loops
		return false, false
	}
	f, err := os.Open(filepath.Clean(file)) //nolint:gosec // the user's ssh config, or a file it includes
	if err != nil {
		return false, false
	}
	defer func() { _ = f.Close() }()
	never := !active
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, args := configLine(sc.Text())
		switch key {
		case "host":
			active = !never && hostMatches(host, args)
		case "match":
			active = !never
		case "include":
			if !active {
				continue
			}
			for _, pattern := range args {
				if strings.HasPrefix(pattern, "~/") {
					pattern = filepath.Join(filepath.Dir(sshDir), pattern[2:])
				} else if !filepath.IsAbs(pattern) {
					pattern = filepath.Join(sshDir, pattern)
				}
				matches, _ := filepath.Glob(pattern)
				for _, m := range matches {
					if v, ok := scanUseKeychain(m, host, sshDir, true, depth+1); ok {
						return v, true
					}
				}
			}
		case "usekeychain":
			if active && len(args) > 0 {
				v := strings.ToLower(args[0])
				return v == "yes" || v == "true", true
			}
		}
	}
	return false, false
}

// configLine splits an ssh config line into its lowercased keyword and its
// arguments: "Key value", "Key=value" and quoted arguments.
func configLine(line string) (string, []string) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", nil
	}
	end := strings.IndexAny(line, " \t=")
	if end < 0 {
		return strings.ToLower(line), nil
	}
	key := strings.ToLower(line[:end])
	rest := strings.TrimLeft(line[end:], " \t")
	rest = strings.TrimLeft(strings.TrimPrefix(rest, "="), " \t")
	var args []string
	for rest != "" {
		var arg string
		if rest[0] == '"' {
			arg, rest, _ = strings.Cut(rest[1:], `"`)
		} else {
			i := strings.IndexAny(rest, " \t")
			if i < 0 {
				i = len(rest)
			}
			arg, rest = rest[:i], rest[i:]
		}
		if strings.HasPrefix(arg, "#") {
			break
		}
		args = append(args, arg)
		rest = strings.TrimLeft(rest, " \t")
	}
	return key, args
}

// hostMatches matches host against a Host line's patterns as ssh does: one
// pattern must match, and none of the negated ones (!pattern) may.
func hostMatches(host string, patterns []string) bool {
	matched := false
	for _, p := range patterns {
		for _, alt := range strings.Split(strings.ToLower(p), ",") {
			negated := strings.HasPrefix(alt, "!")
			ok, _ := path.Match(strings.TrimPrefix(alt, "!"), host)
			if ok && negated {
				return false
			}
			matched = matched || ok
		}
	}
	return matched
}
