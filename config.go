package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// maxIncludeDepth mirrors OpenSSH's limit on nested Include directives.
const maxIncludeDepth = 16

var blankLineRun = regexp.MustCompile(`\n{3,}`)

// HostItem represents an existing parsed SSH host block.
type HostItem struct {
	Alias          string
	AllAliases     []string
	HostName       string
	User           string
	Port           int
	IdentityFile   string
	IdentitiesOnly bool
	PubkeyAuth     bool
	PasswordAuth   bool
	ProxyJump      string
	ConfigFile     string
	Notes          string
	RawLines       []string
}

// Title returns the primary display title for Bubble Tea list.
func (h HostItem) Title() string {
	return h.Alias
}

// Description returns a concise summary for Bubble Tea list.
func (h HostItem) Description() string {
	var parts []string

	parts = append(parts, h.Target())

	if !h.PubkeyAuth || h.PasswordAuth {
		parts = append(parts, "[Password Auth]")
	} else if h.IdentityFile != "" {
		parts = append(parts, fmt.Sprintf("[Key: %s]", filepath.Base(h.IdentityFile)))
	}

	if h.ProxyJump != "" {
		parts = append(parts, fmt.Sprintf("[Via: %s]", h.ProxyJump))
	}

	return strings.Join(parts, "  •  ")
}

// Target renders the host as user@host[:port], falling back to the alias when
// no HostName is configured (ssh then connects to the alias itself).
func (h HostItem) Target() string {
	target := ""
	if h.User != "" {
		target += h.User + "@"
	}
	if h.HostName != "" {
		target += h.HostName
	} else {
		target += h.Alias
	}
	if h.Port > 0 && h.Port != 22 {
		target += fmt.Sprintf(":%d", h.Port)
	}
	return target
}

// FilterValue implements list.Item for fuzzy filtering.
func (h HostItem) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s %s %s",
		strings.Join(h.AllAliases, " "),
		h.HostName,
		h.User,
		strconv.Itoa(h.Port),
		h.IdentityFile,
		h.Notes,
	)
}

// Entry returns the fields of the host that sshx can edit.
func (h HostItem) Entry() HostEntry {
	return HostEntry{
		Alias:          h.Alias,
		HostName:       h.HostName,
		User:           h.User,
		Port:           h.Port,
		IdentityFile:   h.IdentityFile,
		IdentitiesOnly: h.IdentitiesOnly,
		PubkeyAuth:     h.PubkeyAuth,
		PasswordAuth:   h.PasswordAuth,
		ProxyJump:      h.ProxyJump,
	}
}

// HostEntry represents a newly configured host to be formatted and added.
type HostEntry struct {
	Alias          string
	HostName       string
	User           string
	Port           int
	IdentityFile   string
	IdentitiesOnly bool
	PubkeyAuth     bool
	PasswordAuth   bool
	PreferredAuths string
	ProxyJump      string
}

// preferredAuthentications returns the PreferredAuthentications value to write, or "" for none.
func (h HostEntry) preferredAuthentications() string {
	if h.PreferredAuths != "" {
		return h.PreferredAuths
	}
	if h.PasswordAuth {
		return "password,keyboard-interactive"
	}
	return ""
}

// Format renders the HostEntry into standard OpenSSH syntax.
func (h HostEntry) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Host %s\n", h.Alias)
	if h.HostName != "" {
		fmt.Fprintf(&sb, "    HostName %s\n", quoteValue(h.HostName))
	}
	if h.User != "" {
		fmt.Fprintf(&sb, "    User %s\n", quoteValue(h.User))
	}
	if h.Port > 0 && h.Port != 22 {
		fmt.Fprintf(&sb, "    Port %d\n", h.Port)
	}
	if h.IdentityFile != "" {
		fmt.Fprintf(&sb, "    IdentityFile %s\n", quoteValue(h.IdentityFile))
		if h.IdentitiesOnly {
			sb.WriteString("    IdentitiesOnly yes\n")
		}
	}
	if h.ProxyJump != "" {
		fmt.Fprintf(&sb, "    ProxyJump %s\n", quoteValue(h.ProxyJump))
	}
	if !h.PubkeyAuth {
		sb.WriteString("    PubkeyAuthentication no\n")
	}
	if auths := h.preferredAuthentications(); auths != "" {
		fmt.Fprintf(&sb, "    PreferredAuthentications %s\n", auths)
	}
	return sb.String()
}

// parseDirective splits an ssh_config line into its lowercased keyword and its
// value. Both "Key Value" and "Key=Value" forms are accepted, as in ssh_config(5).
// Blank and comment lines return an empty keyword.
func parseDirective(line string) (key, value string) {
	t := strings.TrimSpace(line)
	if t == "" || strings.HasPrefix(t, "#") {
		return "", ""
	}
	i := strings.IndexAny(t, " \t=")
	if i == -1 {
		return strings.ToLower(t), ""
	}
	value = strings.TrimLeft(t[i:], " \t")
	if strings.HasPrefix(value, "=") {
		value = strings.TrimLeft(value[1:], " \t")
	}
	return strings.ToLower(t[:i]), strings.TrimSpace(value)
}

// unquote strips one pair of surrounding quotes from a config value.
func unquote(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		return v[1 : len(v)-1]
	}
	return v
}

// quoteValue double-quotes values containing whitespace so ssh reads them as one argument.
func quoteValue(v string) string {
	if strings.ContainsAny(v, " \t") && unquote(v) == v {
		return `"` + v + `"`
	}
	return v
}

func isUnindentedComment(line string) bool {
	return strings.HasPrefix(line, "#")
}

// isConcreteAlias reports whether a Host pattern names a single host rather than a wildcard or negation.
func isConcreteAlias(p string) bool {
	return !strings.ContainsAny(p, "*?!")
}

func splitPatterns(value string) []string {
	fields := strings.Fields(value)
	for i, f := range fields {
		fields[i] = unquote(f)
	}
	return fields
}

// hostBlock locates a Host section within the lines of a config file.
type hostBlock struct {
	notes    int // first line of the comment group directly above the Host line
	start    int // the Host line
	end      int // one past the last line of the section
	patterns []string
}

func (b hostBlock) has(alias string) bool {
	for _, p := range b.patterns {
		if strings.EqualFold(p, alias) {
			return true
		}
	}
	return false
}

func (b hostBlock) hasAny(aliases []string) bool {
	for _, a := range aliases {
		if b.has(a) {
			return true
		}
	}
	return false
}

// parseHostBlocks finds every Host section in lines. A section runs until the
// next Host or Match keyword, minus any trailing blank lines and unindented
// comments, which introduce the next section. Unindented comments directly above
// a Host line, with no blank line in between, are that host's notes.
func parseHostBlocks(lines []string) []hostBlock {
	var blocks []hostBlock
	open := false
	closeOpen := func(end int) {
		if open {
			b := &blocks[len(blocks)-1]
			for end > b.start+1 && (strings.TrimSpace(lines[end-1]) == "" || isUnindentedComment(lines[end-1])) {
				end--
			}
			b.end = end
			open = false
		}
	}

	for i, line := range lines {
		key, value := parseDirective(line)
		if key != "host" && key != "match" {
			continue
		}
		closeOpen(i)
		if key == "host" {
			notes := i
			for notes > 0 && isUnindentedComment(lines[notes-1]) {
				notes--
			}
			blocks = append(blocks, hostBlock{notes: notes, start: i, patterns: splitPatterns(value)})
			open = true
		}
	}
	closeOpen(len(lines))
	return blocks
}

// expandIncludePattern resolves an Include argument the way ssh does for user
// configs: "~/" is the home directory and relative paths are under ~/.ssh.
// Like glob(3), wildcards do not match hidden files unless the pattern does.
func expandIncludePattern(pattern, homeDir, sshDir string) []string {
	if strings.HasPrefix(pattern, "~/") {
		pattern = filepath.Join(homeDir, pattern[2:])
	} else if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(sshDir, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	showHidden := strings.HasPrefix(filepath.Base(pattern), ".")
	var out []string
	for _, m := range matches {
		if showHidden || !strings.HasPrefix(filepath.Base(m), ".") {
			out = append(out, m)
		}
	}
	return out
}

// FindConfigFiles gathers ~/.ssh/config, any files it includes (recursively) via Include directives, and ~/.ssh/config.d/
func FindConfigFiles(homeDir string) []string {
	var files []string
	seenFiles := make(map[string]bool)
	sshDir := filepath.Join(homeDir, ".ssh")

	var visit func(p string, depth int)
	visit = func(p string, depth int) {
		clean := filepath.Clean(p)
		if seenFiles[clean] {
			return
		}
		fi, err := os.Stat(clean)
		if err != nil || fi.IsDir() {
			return
		}
		seenFiles[clean] = true
		files = append(files, clean)

		if depth >= maxIncludeDepth {
			return
		}
		data, err := os.ReadFile(clean) //nolint:gosec // intentional user SSH config parsing
		if err != nil {
			return
		}
		for _, line := range strings.Split(string(data), "\n") {
			if key, value := parseDirective(line); key == "include" {
				for _, pattern := range splitPatterns(value) {
					for _, m := range expandIncludePattern(pattern, homeDir, sshDir) {
						visit(m, depth+1)
					}
				}
			}
		}
	}

	visit(filepath.Join(sshDir, "config"), 0)

	for _, sub := range []string{"config.d", "conf.d"} {
		dir := filepath.Join(sshDir, sub)
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					visit(filepath.Join(dir, e.Name()), 1)
				}
			}
		}
	}
	return files
}

// LoadAllHosts parses all SSH config files and returns a list of configured hosts.
func LoadAllHosts(homeDir string) ([]HostItem, error) {
	var hosts []HostItem
	seenAliases := make(map[string]bool)

	for _, file := range FindConfigFiles(homeDir) {
		data, err := os.ReadFile(filepath.Clean(file)) //nolint:gosec // user SSH config file paths
		if err != nil {
			continue
		}
		for _, h := range parseHosts(string(data), file) {
			key := strings.ToLower(h.Alias)
			if !seenAliases[key] {
				seenAliases[key] = true
				hosts = append(hosts, h)
			}
		}
	}

	sort.Slice(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Alias) < strings.ToLower(hosts[j].Alias)
	})

	return hosts, nil
}

// parseHosts extracts the hosts defined in one config file. As in ssh, the first
// value given for a directive within a section wins.
func parseHosts(content, file string) []HostItem {
	lines := strings.Split(content, "\n")
	var hosts []HostItem

	for _, b := range parseHostBlocks(lines) {
		var aliases []string
		for _, p := range b.patterns {
			if isConcreteAlias(p) {
				aliases = append(aliases, p)
			}
		}
		if len(aliases) == 0 {
			continue
		}

		var notes []string
		for _, l := range lines[b.notes:b.start] {
			if c := strings.TrimSpace(strings.TrimPrefix(l, "#")); c != "" {
				notes = append(notes, c)
			}
		}

		h := HostItem{
			Alias:      aliases[0],
			AllAliases: aliases,
			Port:       22,
			PubkeyAuth: true,
			ConfigFile: file,
			Notes:      strings.Join(notes, " "),
			RawLines:   append([]string(nil), lines[b.notes:b.end]...),
		}

		seen := make(map[string]bool)
		for _, line := range lines[b.start+1 : b.end] {
			key, val := parseDirective(line)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			val = unquote(val)

			switch key {
			case "hostname":
				h.HostName = val
			case "user":
				h.User = val
			case "port":
				if p, err := strconv.Atoi(val); err == nil && p > 0 {
					h.Port = p
				}
			case "identityfile":
				h.IdentityFile = val
			case "identitiesonly":
				h.IdentitiesOnly = strings.EqualFold(val, "yes")
			case "pubkeyauthentication":
				h.PubkeyAuth = !strings.EqualFold(val, "no")
			case "preferredauthentications":
				h.PasswordAuth = strings.Contains(strings.ToLower(val), "password")
			case "proxyjump":
				h.ProxyJump = val
			}
		}
		hosts = append(hosts, h)
	}
	return hosts
}

// FindHost returns the host that lists alias among its aliases (case-insensitive).
func FindHost(hosts []HostItem, alias string) (HostItem, bool) {
	for _, h := range hosts {
		for _, a := range h.AllAliases {
			if strings.EqualFold(a, alias) {
				return h, true
			}
		}
	}
	return HostItem{}, false
}

// ParseTarget parses strings such as "user@host:port", "[2001:db8::1]:port", or "host:port".
func ParseTarget(input string) (user, host string, port int) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "ssh://")

	if idx := strings.Index(input, "@"); idx != -1 {
		user = input[:idx]
		input = input[idx+1:]
	}

	if strings.HasPrefix(input, "[") {
		if end := strings.Index(input, "]"); end != -1 {
			host = input[1:end]
			remainder := input[end+1:]
			if strings.HasPrefix(remainder, ":") {
				if p, err := strconv.Atoi(remainder[1:]); err == nil && p > 0 && p <= 65535 {
					port = p
				}
			}
			return user, host, port
		}
	}

	// If there are multiple colons and no brackets, it's an IPv6 address without port
	if strings.Count(input, ":") > 1 {
		return user, input, port
	}

	if idx := strings.LastIndex(input, ":"); idx != -1 {
		pStr := input[idx+1:]
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 && p <= 65535 {
			port = p
			host = input[:idx]
		} else {
			host = input
		}
	} else {
		host = input
	}

	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return user, host, port
}

// HasHost checks whether any of the given aliases (space-separated) appears on a Host line (case-insensitive).
func HasHost(content, alias string) bool {
	targets := strings.Fields(alias)
	if len(targets) == 0 {
		return false
	}
	for _, b := range parseHostBlocks(strings.Split(content, "\n")) {
		if b.hasAny(targets) {
			return true
		}
	}
	return false
}

// lineSuffix returns the carriage return of a CRLF line so rewritten lines keep the file's line endings.
func lineSuffix(line string) string {
	if strings.HasSuffix(line, "\r") {
		return "\r"
	}
	return ""
}

func leadingSpace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

// rewriteHostLine replaces the patterns on a Host line, keeping its indentation and keyword spelling.
func rewriteHostLine(line string, patterns []string) string {
	t := strings.TrimLeft(line, " \t")
	kw := strings.TrimRight(t, "\r")
	if i := strings.IndexAny(t, " \t="); i != -1 {
		kw = t[:i]
	}
	return leadingSpace(line) + kw + " " + strings.Join(patterns, " ") + lineSuffix(line)
}

// RemoveHost removes the given aliases (space-separated, case-insensitive) from
// every Host line that lists them. A section left with no positive pattern is
// deleted together with its notes; one still shared with other names or
// wildcards keeps its directives. Comments belonging to other sections are kept.
func RemoveHost(content, alias string) string {
	targets := make(map[string]bool)
	for _, t := range strings.Fields(alias) {
		targets[strings.ToLower(t)] = true
	}
	if len(targets) == 0 {
		return content
	}

	lines := strings.Split(content, "\n")
	blocks := parseHostBlocks(lines)
	changed := false

	for i := len(blocks) - 1; i >= 0; i-- {
		b := blocks[i]
		var kept []string
		positive := false
		for _, p := range b.patterns {
			if !targets[strings.ToLower(p)] {
				kept = append(kept, p)
				positive = positive || !strings.HasPrefix(p, "!")
			}
		}
		if len(kept) == len(b.patterns) {
			continue
		}
		changed = true

		if positive {
			lines[b.start] = rewriteHostLine(lines[b.start], kept)
			continue
		}
		end := b.end
		for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
			end++
		}
		lines = append(lines[:b.notes], lines[end:]...)
	}

	if !changed {
		return content
	}
	return cleanConsecutiveBlankLines(strings.Join(lines, "\n"))
}

// setDirective sets key to value within a Host section (whose first line is the
// Host line), replacing the first occurrence in place. An empty value removes
// every occurrence. A key not yet present is added after the last directive,
// matching the section's indentation.
func setDirective(section []string, key, value string) []string {
	lk := strings.ToLower(key)
	out := make([]string, 0, len(section)+1)
	out = append(out, section[0])
	replaced := false
	last := 0
	indent := ""
	indentFound := false

	for _, line := range section[1:] {
		k, _ := parseDirective(line)
		if k == lk {
			if value == "" {
				continue
			}
			if !replaced {
				line = leadingSpace(line) + key + " " + quoteValue(value) + lineSuffix(line)
				replaced = true
			}
		}
		out = append(out, line)
		if k != "" {
			last = len(out) - 1
			if !indentFound {
				indent = leadingSpace(line)
				indentFound = true
			}
		}
	}

	if value != "" && !replaced {
		if !indentFound {
			indent = "    "
		}
		added := indent + key + " " + quoteValue(value) + lineSuffix(section[0])
		out = append(out[:last+1], append([]string{added}, out[last+1:]...)...)
	}
	return out
}

// applyEntryChanges rewrites a Host section (Host line first) so it reflects
// next, touching only the directives whose values differ from prev. alias is
// the name on the Host line that next.Alias replaces.
func applyEntryChanges(section []string, alias string, prev, next HostEntry) []string {
	s := append([]string(nil), section...)

	if next.Alias != prev.Alias {
		_, value := parseDirective(s[0])
		var patterns []string
		seen := make(map[string]bool)
		add := func(p string) {
			if !seen[strings.ToLower(p)] {
				seen[strings.ToLower(p)] = true
				patterns = append(patterns, p)
			}
		}
		for _, p := range splitPatterns(value) {
			if strings.EqualFold(p, alias) {
				for _, a := range strings.Fields(next.Alias) {
					add(a)
				}
			} else {
				add(p)
			}
		}
		s[0] = rewriteHostLine(s[0], patterns)
	}

	if next.HostName != prev.HostName {
		s = setDirective(s, "HostName", next.HostName)
	}
	if next.User != prev.User {
		s = setDirective(s, "User", next.User)
	}
	if next.Port > 0 && next.Port != prev.Port {
		s = setDirective(s, "Port", strconv.Itoa(next.Port))
	}
	if next.IdentityFile != prev.IdentityFile {
		s = setDirective(s, "IdentityFile", next.IdentityFile)
	}
	if next.IdentitiesOnly != prev.IdentitiesOnly {
		value := ""
		if next.IdentitiesOnly {
			value = "yes"
		}
		s = setDirective(s, "IdentitiesOnly", value)
	}
	if next.ProxyJump != prev.ProxyJump {
		s = setDirective(s, "ProxyJump", next.ProxyJump)
	}
	if next.PubkeyAuth != prev.PubkeyAuth {
		value := ""
		if !next.PubkeyAuth {
			value = "no"
		}
		s = setDirective(s, "PubkeyAuthentication", value)
	}
	if next.preferredAuthentications() != prev.preferredAuthentications() {
		s = setDirective(s, "PreferredAuthentications", next.preferredAuthentications())
	}
	return s
}

// UpdateHost edits the first Host section listing alias in place, changing only
// the directives whose values differ between prev and next. Other names and
// patterns on the Host line, directives sshx does not manage, comments and the
// section's position in the file are all preserved.
func UpdateHost(content, alias string, prev, next HostEntry) (string, error) {
	lines := strings.Split(content, "\n")
	for _, b := range parseHostBlocks(lines) {
		if !b.has(alias) {
			continue
		}
		section := applyEntryChanges(lines[b.start:b.end], alias, prev, next)
		out := make([]string, 0, len(lines)+len(section))
		out = append(out, lines[:b.start]...)
		out = append(out, section...)
		out = append(out, lines[b.end:]...)
		return strings.Join(out, "\n"), nil
	}
	return content, fmt.Errorf("host '%s' not found in configuration", alias)
}

// CloneHost builds a new Host section for next by copying the section that
// lists prev.Alias, so directives sshx does not manage carry over. The copy's
// Host line lists only next.Alias. Without a source section it formats next.
func CloneHost(content string, prev, next HostEntry) string {
	lines := strings.Split(content, "\n")
	for _, b := range parseHostBlocks(lines) {
		if !b.has(prev.Alias) {
			continue
		}
		section := append([]string(nil), lines[b.start:b.end]...)
		section[0] = rewriteHostLine(section[0], []string{prev.Alias})
		section = applyEntryChanges(section, prev.Alias, prev, next)
		return strings.Join(section, "\n") + "\n"
	}
	return next.Format()
}

// ResolveHostConfigFile finds the configuration file containing the specified host alias.
func ResolveHostConfigFile(alias, homeDir string, targetConfigFile ...string) (string, error) {
	if len(targetConfigFile) > 0 && targetConfigFile[0] != "" {
		return targetConfigFile[0], nil
	}

	if hosts, err := LoadAllHosts(homeDir); err == nil {
		if h, ok := FindHost(hosts, alias); ok {
			if h.ConfigFile != "" {
				return h.ConfigFile, nil
			}
			return filepath.Join(homeDir, ".ssh", "config"), nil
		}
	}

	if file, ok := FindAliasOwner(homeDir, alias, "", ""); ok {
		return file, nil
	}

	return "", fmt.Errorf("host '%s' not found in SSH configuration", alias)
}

// FindAliasOwner returns the first config file with a Host section listing any
// of the given aliases (space-separated). The section in skipFile that lists
// skipAlias is ignored, so a host being renamed does not conflict with itself.
func FindAliasOwner(homeDir, alias, skipFile, skipAlias string) (string, bool) {
	targets := strings.Fields(alias)
	for _, file := range FindConfigFiles(homeDir) {
		data, err := os.ReadFile(filepath.Clean(file)) //nolint:gosec // user SSH config file paths
		if err != nil {
			continue
		}
		for _, b := range parseHostBlocks(strings.Split(string(data), "\n")) {
			if !b.hasAny(targets) {
				continue
			}
			if skipAlias != "" && file == skipFile && b.has(skipAlias) {
				continue
			}
			return file, true
		}
	}
	return "", false
}

// DeleteHostFromConfigFile removes the given aliases (space-separated) from the specified config file.
func DeleteHostFromConfigFile(alias, configPath string) error {
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // user SSH config file path
	if err != nil {
		return err
	}

	if !HasHost(string(data), alias) {
		return fmt.Errorf("host '%s' not found in %s", alias, configPath)
	}
	return WriteConfigFile(configPath, []byte(RemoveHost(string(data), alias)))
}

// InsertHost inserts a formatted host block before the wildcard 'Host *' section (and its notes).
func InsertHost(content, block string) string {
	block = strings.TrimRight(block, "\n")
	if strings.TrimSpace(content) == "" {
		return block + "\n"
	}

	lines := strings.Split(content, "\n")
	insertIdx := -1
	for _, b := range parseHostBlocks(lines) {
		if len(b.patterns) == 1 && b.patterns[0] == "*" {
			insertIdx = b.notes
			break
		}
	}

	if insertIdx == -1 {
		return strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
	}

	var sb strings.Builder
	if before := strings.TrimRight(strings.Join(lines[:insertIdx], "\n"), "\n"); strings.TrimSpace(before) != "" {
		sb.WriteString(before)
		sb.WriteString("\n\n")
	}
	sb.WriteString(block)
	sb.WriteString("\n\n")
	sb.WriteString(strings.Join(lines[insertIdx:], "\n"))

	return cleanConsecutiveBlankLines(sb.String())
}

func cleanConsecutiveBlankLines(s string) string {
	cleaned := blankLineRun.ReplaceAllString(s, "\n\n")
	if !strings.HasSuffix(cleaned, "\n") {
		cleaned += "\n"
	}
	return cleaned
}

// DiscoverKeys inspects ~/.ssh for private keys.
func DiscoverKeys(sshDir string) ([]string, error) {
	entries, err := os.ReadDir(sshDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var keys []string
	ignoredNames := map[string]bool{
		"config":          true,
		"authorized_keys": true,
		"known_hosts":     true,
		"known_hosts.old": true,
		"allowed_signers": true,
		"environment":     true,
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".pub") {
			continue
		}
		if ignoredNames[name] || strings.HasPrefix(name, "known_hosts") {
			continue
		}

		fullPath := filepath.Join(sshDir, name)
		fi, err := entry.Info()
		if err != nil {
			continue
		}
		if fi.Mode().IsRegular() {
			keys = append(keys, fullPath)
		}
	}

	sort.Strings(keys)
	return keys, nil
}

// expandHome turns a leading "~" into homeDir.
func expandHome(path, homeDir string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homeDir, path[2:])
	}
	if path == "~" {
		return homeDir
	}
	return path
}

// shortenHome rewrites a path inside homeDir to the "~/" form used in SSH configs.
func shortenHome(path, homeDir string) string {
	if homeDir == "" {
		return path
	}
	if path == homeDir {
		return "~"
	}
	prefix := strings.TrimSuffix(homeDir, string(filepath.Separator)) + string(filepath.Separator)
	if strings.HasPrefix(path, prefix) {
		return "~/" + filepath.ToSlash(path[len(prefix):])
	}
	return path
}

// BackupPath is where WriteConfigFile keeps the previous version of a config
// file: a hidden file alongside it, which neither ssh's Include globs nor sshx read.
func BackupPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "."+filepath.Base(configPath)+".sshx.bak")
}

// WriteConfigFile atomically replaces an SSH config file with 0600 permissions,
// first saving its previous contents to BackupPath.
func WriteConfigFile(configPath string, data []byte) error {
	if old, err := os.ReadFile(filepath.Clean(configPath)); err == nil && !bytes.Equal(old, data) { //nolint:gosec // user SSH config file path
		if err := AtomicWrite(BackupPath(configPath), old, 0600); err != nil {
			return fmt.Errorf("failed to back up %s: %w", configPath, err)
		}
	}
	return AtomicWrite(configPath, data, 0600)
}

// AtomicWrite writes data atomically to targetPath with given permissions.
// When targetPath is a symlink, the file it points to is replaced and the link is kept.
func AtomicWrite(targetPath string, data []byte, perm os.FileMode) error {
	if resolved, err := filepath.EvalSymlinks(targetPath); err == nil {
		targetPath = resolved
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return err
	}
	tmpName := filepath.Join(dir, fmt.Sprintf(".tmp.%s.%s", filepath.Base(targetPath), hex.EncodeToString(randBytes)))

	tmpFile, err := os.OpenFile(filepath.Clean(tmpName), os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm) //nolint:gosec // atomic temp file write
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}

	success := false
	defer func() {
		if !success {
			_ = tmpFile.Close()
			_ = os.Remove(tmpName)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("failed to write to temp file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("failed to set permissions on temp file: %w", err)
	}
	if err := os.Rename(tmpName, targetPath); err != nil {
		return fmt.Errorf("failed to rename temp file to %s: %w", targetPath, err)
	}

	success = true
	return nil
}
