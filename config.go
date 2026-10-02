package main

import (
	"bufio"
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
	RawLines       []string
}

// Title returns the primary display title for Bubble Tea list.
func (h HostItem) Title() string {
	return h.Alias
}

// Description returns a concise summary for Bubble Tea list.
func (h HostItem) Description() string {
	var parts []string

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
	parts = append(parts, target)

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

// FilterValue implements list.Item for fuzzy filtering.
func (h HostItem) FilterValue() string {
	return fmt.Sprintf("%s %s %s %s %s",
		strings.Join(h.AllAliases, " "),
		h.HostName,
		h.User,
		strconv.Itoa(h.Port),
		h.IdentityFile,
	)
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

// Format renders the HostEntry into standard OpenSSH syntax.
func (h HostEntry) Format() string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Host %s\n", h.Alias)
	if h.HostName != "" {
		fmt.Fprintf(&sb, "    HostName %s\n", h.HostName)
	}
	if h.User != "" {
		fmt.Fprintf(&sb, "    User %s\n", h.User)
	}
	if h.Port > 0 && h.Port != 22 {
		fmt.Fprintf(&sb, "    Port %d\n", h.Port)
	}
	if h.IdentityFile != "" {
		fmt.Fprintf(&sb, "    IdentityFile %s\n", h.IdentityFile)
		if h.IdentitiesOnly {
			sb.WriteString("    IdentitiesOnly yes\n")
		}
	}
	if h.ProxyJump != "" {
		fmt.Fprintf(&sb, "    ProxyJump %s\n", h.ProxyJump)
	}
	if !h.PubkeyAuth {
		sb.WriteString("    PubkeyAuthentication no\n")
	}
	if h.PasswordAuth || h.PreferredAuths != "" {
		auths := h.PreferredAuths
		if auths == "" {
			auths = "password,keyboard-interactive"
		}
		fmt.Fprintf(&sb, "    PreferredAuthentications %s\n", auths)
	}
	return sb.String()
}

// FindConfigFiles gathers ~/.ssh/config and any included fragments in ~/.ssh/config.d/
func FindConfigFiles(homeDir string) []string {
	var files []string
	mainConfig := filepath.Join(homeDir, ".ssh", "config")
	if fi, err := os.Stat(mainConfig); err == nil && !fi.IsDir() {
		files = append(files, mainConfig)
	}

	for _, sub := range []string{"config.d", "conf.d"} {
		dir := filepath.Join(homeDir, ".ssh", sub)
		entries, err := os.ReadDir(dir)
		if err == nil {
			for _, e := range entries {
				if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
					files = append(files, filepath.Join(dir, e.Name()))
				}
			}
		}
	}
	return files
}

// LoadAllHosts parses all SSH config files and returns a list of configured hosts.
func LoadAllHosts(homeDir string) ([]HostItem, error) {
	files := FindConfigFiles(homeDir)
	if len(files) == 0 {
		return nil, nil
	}

	var hosts []HostItem
	seenAliases := make(map[string]bool)

	for _, file := range files {
		data, err := os.ReadFile(filepath.Clean(file)) //nolint:gosec // user SSH config file paths
		if err != nil {
			continue
		}

		lines := strings.Split(string(data), "\n")
		var currentHost *HostItem

		finishCurrent := func() {
			if currentHost != nil && len(currentHost.AllAliases) > 0 {
				if !seenAliases[currentHost.Alias] {
					seenAliases[currentHost.Alias] = true
					if currentHost.Port == 0 {
						currentHost.Port = 22
					}
					hosts = append(hosts, *currentHost)
				}
			}
			currentHost = nil
		}

		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") || trimmed == "" {
				if currentHost != nil {
					currentHost.RawLines = append(currentHost.RawLines, line)
				}
				continue
			}

			fields := strings.Fields(trimmed)
			if len(fields) == 0 {
				continue
			}

			key := strings.ToLower(fields[0])

			if key == "host" || key == "match" {
				finishCurrent()

				if key == "host" && len(fields) > 1 {
					var validAliases []string
					for _, alias := range fields[1:] {
						if !strings.ContainsAny(alias, "*?!") {
							validAliases = append(validAliases, alias)
						}
					}

					if len(validAliases) > 0 {
						currentHost = &HostItem{
							Alias:      validAliases[0],
							AllAliases: validAliases,
							Port:       22,
							PubkeyAuth: true,
							ConfigFile: file,
							RawLines:   []string{line},
						}
					}
				}
				continue
			}

			if currentHost != nil {
				currentHost.RawLines = append(currentHost.RawLines, line)
				val := ""
				if len(fields) > 1 {
					val = strings.Join(fields[1:], " ")
					// Remove surrounding quotes if present
					val = strings.Trim(val, `"'`)
				}

				switch key {
				case "hostname":
					currentHost.HostName = val
				case "user":
					currentHost.User = val
				case "port":
					if p, err := strconv.Atoi(val); err == nil && p > 0 {
						currentHost.Port = p
					}
				case "identityfile":
					currentHost.IdentityFile = val
				case "identitiesonly":
					currentHost.IdentitiesOnly = strings.EqualFold(val, "yes")
				case "pubkeyauthentication":
					if strings.EqualFold(val, "no") {
						currentHost.PubkeyAuth = false
					}
				case "preferredauthentications":
					if strings.Contains(strings.ToLower(val), "password") {
						currentHost.PasswordAuth = true
					}
				case "proxyjump":
					currentHost.ProxyJump = val
				}
			}
		}
		finishCurrent()
	}

	sort.Slice(hosts, func(i, j int) bool {
		return strings.ToLower(hosts[i].Alias) < strings.ToLower(hosts[j].Alias)
	})

	return hosts, nil
}

// ParseTarget parses strings such as "user@host:port" or "host:port".
func ParseTarget(input string) (user, host string, port int) {
	input = strings.TrimSpace(input)
	input = strings.TrimPrefix(input, "ssh://")

	if idx := strings.Index(input, "@"); idx != -1 {
		user = input[:idx]
		input = input[idx+1:]
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

// HasHost checks whether the given alias exists in the configuration content.
func HasHost(content, alias string) bool {
	targets := strings.Fields(alias)
	if len(targets) == 0 {
		return false
	}
	targetMap := make(map[string]bool)
	for _, t := range targets {
		targetMap[t] = true
	}

	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[0], "Host") {
			for _, pat := range fields[1:] {
				if targetMap[pat] {
					return true
				}
			}
		}
	}
	return false
}

// RemoveHost removes any Host block matching the given alias.
func RemoveHost(content, alias string) string {
	targets := strings.Fields(alias)
	if len(targets) == 0 {
		return content
	}
	targetMap := make(map[string]bool)
	for _, t := range targets {
		targetMap[t] = true
	}

	lines := strings.Split(content, "\n")
	var result []string
	inSkipBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		fields := strings.Fields(trimmed)

		if len(fields) >= 1 && (strings.EqualFold(fields[0], "Host") || strings.EqualFold(fields[0], "Match")) {
			inSkipBlock = false
			if strings.EqualFold(fields[0], "Host") {
				for _, pat := range fields[1:] {
					if targetMap[pat] {
						inSkipBlock = true
						break
					}
				}
			}
		}

		if !inSkipBlock {
			result = append(result, line)
		}
	}

	return cleanConsecutiveBlankLines(strings.Join(result, "\n"))
}

// ResolveHostConfigFile finds the configuration file containing the specified host alias.
func ResolveHostConfigFile(alias, homeDir string, targetConfigFile ...string) (string, error) {
	if len(targetConfigFile) > 0 && targetConfigFile[0] != "" {
		return targetConfigFile[0], nil
	}

	if hosts, err := LoadAllHosts(homeDir); err == nil {
		for _, h := range hosts {
			for _, a := range h.AllAliases {
				if strings.EqualFold(a, alias) {
					if h.ConfigFile != "" {
						return h.ConfigFile, nil
					}
					return filepath.Join(homeDir, ".ssh", "config"), nil
				}
			}
		}
	}

	for _, file := range FindConfigFiles(homeDir) {
		if data, err := os.ReadFile(filepath.Clean(file)); err == nil {
			if HasHost(string(data), alias) {
				return file, nil
			}
		}
	}

	return "", fmt.Errorf("host '%s' not found in SSH configuration", alias)
}

// DeleteHostFromConfigFile removes a host block from the specified config file.
func DeleteHostFromConfigFile(alias, configPath string) error {
	data, err := os.ReadFile(filepath.Clean(configPath)) //nolint:gosec // user SSH config file path
	if err != nil {
		return err
	}

	cleaned := RemoveHost(string(data), alias)
	return AtomicWrite(configPath, []byte(cleaned), 0600)
}

// InsertHost inserts a formatted host block before any wildcard 'Host *' block (and its comments).
func InsertHost(content, block string) string {
	block = strings.TrimRight(block, "\n")
	if strings.TrimSpace(content) == "" {
		return block + "\n"
	}

	lines := strings.Split(content, "\n")
	wildcardIdx := -1

	for i, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 2 && strings.EqualFold(fields[0], "Host") && fields[1] == "*" {
			wildcardIdx = i
			break
		}
	}

	if wildcardIdx == -1 {
		trimmed := strings.TrimRight(content, "\n")
		return trimmed + "\n\n" + block + "\n"
	}

	insertIdx := wildcardIdx
	for i := wildcardIdx - 1; i >= 0; i-- {
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "#") {
			insertIdx = i
		} else {
			break
		}
	}

	var sb strings.Builder
	for i := 0; i < insertIdx; i++ {
		sb.WriteString(lines[i])
		sb.WriteString("\n")
	}

	before := strings.TrimRight(sb.String(), "\n")
	if len(before) > 0 {
		sb.Reset()
		sb.WriteString(before)
		sb.WriteString("\n\n")
	} else {
		sb.Reset()
	}

	sb.WriteString(block)
	sb.WriteString("\n\n")

	for i := insertIdx; i < len(lines); i++ {
		sb.WriteString(lines[i])
		if i < len(lines)-1 {
			sb.WriteString("\n")
		}
	}

	return cleanConsecutiveBlankLines(sb.String())
}

func cleanConsecutiveBlankLines(s string) string {
	re := regexp.MustCompile(`\n{3,}`)
	cleaned := re.ReplaceAllString(s, "\n\n")
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

// AtomicWrite writes data atomically to targetPath with given permissions.
func AtomicWrite(targetPath string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	randBytes := make([]byte, 4)
	if _, err := rand.Read(randBytes); err != nil {
		return err
	}
	tmpName := filepath.Join(dir, fmt.Sprintf(".tmp.%s.%s", filepath.Base(targetPath), hex.EncodeToString(randBytes)))

	tmpFile, err := os.OpenFile(filepath.Clean(tmpName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) //nolint:gosec // atomic temp file write
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
