package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func main() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error resolving home directory: %v\n", err)
		os.Exit(1)
	}

	for _, a := range os.Args[1:] {
		if a == "-v" || a == "--version" || a == "version" {
			fmt.Printf("sshx %s (commit: %s, built: %s)\n", Version, Commit, BuildDate)
			return
		}
		if a == "-h" || a == "--help" || a == "help" {
			printUsage()
			return
		}
	}

	args := os.Args[1:]
	binName := filepath.Base(os.Args[0])
	if binName == "ssh-add-host" || binName == "fssh-add" {
		args = append([]string{"add"}, args...)
	}

	if len(args) > 0 {
		cmd := strings.ToLower(args[0])

		switch cmd {
		case "add":
			target := ""
			alias := ""
			if len(args) > 1 {
				target = args[1]
			}
			if len(args) > 2 {
				alias = args[2]
			}
			connectTarget, connectNow, err := AddHostWizard(target, alias, homeDir, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
			if connectNow && connectTarget != "" {
				connectSSH(connectTarget)
			}
			return

		case "ls", "list":
			listHosts(homeDir)
			return

		case "rm", "delete", "remove":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: sshx rm <alias>")
				os.Exit(1)
			}
			alias := args[1]
			if err := DeleteHostPrompt(alias, homeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error deleting host: %v\n", err)
				os.Exit(1)
			}
			return

		case "clone", "dup", "duplicate":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: sshx clone <alias>")
				os.Exit(1)
			}
			alias := args[1]
			if _, err := CloneHostWizard(alias, homeDir); err != nil {
				fmt.Fprintf(os.Stderr, "Error cloning host: %v\n", err)
				os.Exit(1)
			}
			return

		case "probe", "ping":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: sshx probe <alias>")
				os.Exit(1)
			}
			alias := args[1]
			probeHostCLI(alias, homeDir)
			return

		case "update", "upgrade":
			force := false
			checkOnly := false
			for _, a := range args[1:] {
				if a == "--force" || a == "-f" {
					force = true
				}
				if a == "--check" || a == "-c" {
					checkOnly = true
				}
			}
			if checkOnly {
				rel, isNewer, err := CheckLatestRelease(Version)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
					os.Exit(1)
				}
				if isNewer {
					fmt.Printf("Update available: %s -> %s (run 'sshx update' to upgrade)\n", Version, rel.TagName)
				} else {
					fmt.Printf("sshx is up to date (%s)\n", Version)
				}
				return
			}
			if _, err := PerformUpdate(Version, os.Stdout, force); err != nil {
				fmt.Fprintf(os.Stderr, "Update error: %v\n", err)
				os.Exit(1)
			}
			return

		case "check-update":
			rel, isNewer, err := CheckLatestRelease(Version)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error checking for updates: %v\n", err)
				os.Exit(1)
			}
			if isNewer {
				fmt.Printf("Update available: %s -> %s (run 'sshx update' to upgrade)\n", Version, rel.TagName)
			} else {
				fmt.Printf("sshx is up to date (%s)\n", Version)
			}
			return

		case "edit":
			if len(args) > 1 {
				alias := args[1]
				if _, err := EditHostWizard(alias, homeDir); err != nil {
					fmt.Fprintf(os.Stderr, "Error editing host: %v\n", err)
					os.Exit(1)
				}
				return
			}
			openEditor(homeDir)
			return

		case "connect":
			if len(args) < 2 {
				fmt.Fprintln(os.Stderr, "Usage: sshx connect <alias>")
				os.Exit(1)
			}
			connectSSH(args[1])
			return

		default:
			// If first argument does not start with "-", check if it's a known host alias!
			if !strings.HasPrefix(cmd, "-") {
				hosts, _ := LoadAllHosts(homeDir)
				for _, h := range hosts {
					for _, a := range h.AllAliases {
						if strings.EqualFold(a, cmd) {
							connectSSH(h.Alias)
							return
						}
					}
				}
			}
		}
	}

	// Default: Launch TUI loop
	lastSelected := ""
	for {
		choice, action, err := RunTUI(homeDir, lastSelected)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}
		if choice != "" {
			lastSelected = choice
		}

		switch action {
		case "connect":
			if choice != "" {
				connectSSH(choice)
			}
			return

		case "add":
			addedAlias, _, _ := AddHostWizard("", "", homeDir, false)
			if addedAlias != "" {
				lastSelected = addedAlias
			}

		case "edit-host":
			if choice != "" {
				editedAlias, err := EditHostWizard(choice, homeDir)
				if err == nil && editedAlias != "" {
					lastSelected = editedAlias
				}
			}

		case "clone-host":
			if choice != "" {
				clonedAlias, err := CloneHostWizard(choice, homeDir)
				if err == nil && clonedAlias != "" {
					lastSelected = clonedAlias
				}
			}

		case "delete":
			if choice != "" {
				_ = DeleteHostPrompt(choice, homeDir)
				pausePrompt()
			}

		case "copy-id":
			if choice != "" {
				runCopyIDForHost(choice, homeDir)
				pausePrompt()
			}

		case "edit-config", "edit":
			openEditor(homeDir)

		case "upgrade":
			updated, err := PerformUpdate(Version, os.Stdout, false)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nUpdate error: %v\n", err)
				pausePrompt()
			} else if updated {
				return
			} else {
				pausePrompt()
			}

		case "quit", "":
			return
		}
	}
}

func pausePrompt() {
	fmt.Print("\nPress Enter to return to sshx...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
}

func printUsage() {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorCoral).Render("sshx — TUI SSH Connection Manager")
	fmt.Printf("%s\n\n", header)
	fmt.Println("Usage:")
	fmt.Println("  sshx                      Launch interactive host browser")
	fmt.Println("  sshx <alias>              Directly connect to host")
	fmt.Println("  sshx add [target] [alias] Interactively add a new SSH host")
	fmt.Println("  sshx ls                   List all configured SSH hosts")
	fmt.Println("  sshx rm <alias>           Remove a host from ~/.ssh/config")
	fmt.Println("  sshx edit [alias]         Edit host in wizard, or open ~/.ssh/config in $EDITOR")
	fmt.Println("  sshx clone <alias>        Duplicate / clone an existing host")
	fmt.Println("  sshx probe <alias>        Probe TCP reachability / ping host")
	fmt.Println("  sshx update [--check]     Check for and install latest release update")
	fmt.Println()
	fmt.Println("TUI Keybindings:")
	fmt.Println("  Enter      Connect to selected host")
	fmt.Println("  /          Filter / search hosts")
	fmt.Println("  a          Add new host (launches wizard)")
	fmt.Println("  e          Edit selected host (launches wizard)")
	fmt.Println("  D          Duplicate / clone selected host")
	fmt.Println("  d, x       Delete selected host")
	fmt.Println("  c          Copy public key to host (ssh-copy-id)")
	fmt.Println("  y          Yank SSH connect command to clipboard")
	fmt.Println("  p          Probe TCP reachability / ping host")
	fmt.Println("  v          Toggle raw OpenSSH config view")
	fmt.Println("  U          Upgrade sshx to latest release")
	fmt.Println("  E          Open ~/.ssh/config in $EDITOR")
	fmt.Println("  Tab        Toggle details inspector (on compact displays)")
	fmt.Println("  q, Esc     Quit")
}

func listHosts(homeDir string) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading hosts: %v\n", err)
		os.Exit(1)
	}

	if len(hosts) == 0 {
		fmt.Println("No configured SSH hosts found in ~/.ssh/config.")
		return
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(colorPurple)
	fmt.Printf("%-20s %-30s %-16s %s\n",
		headerStyle.Render("ALIAS"),
		headerStyle.Render("TARGET"),
		headerStyle.Render("PORT"),
		headerStyle.Render("AUTH"),
	)
	fmt.Println(strings.Repeat("─", 80))

	for _, h := range hosts {
		target := h.HostName
		if h.User != "" {
			target = h.User + "@" + target
		}
		auth := "Default"
		if !h.PubkeyAuth || h.PasswordAuth {
			auth = "Password"
		} else if h.IdentityFile != "" {
			auth = filepath.Base(h.IdentityFile)
		}

		fmt.Printf("%-20s %-30s %-16d %s\n", h.Alias, target, h.Port, auth)
	}
}

func connectSSH(alias string) {
	fmt.Printf("Connecting to %s...\n\n", alias)
	cmd := exec.Command("ssh", alias) //nolint:gosec // intentional user SSH connection
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func openEditor(homeDir string) {
	configPath := filepath.Join(homeDir, ".ssh", "config")
	editor := os.Getenv("EDITOR")
	if editor == "" {
		if _, err := exec.LookPath("nvim"); err == nil {
			editor = "nvim"
		} else {
			editor = "vim"
		}
	}

	cmd := exec.Command(editor, configPath) //nolint:gosec // intentional editor launch
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Run()
}

func runCopyIDForHost(alias, homeDir string) {
	hosts, _ := LoadAllHosts(homeDir)
	targetHost, ok := FindHost(hosts, alias)
	if !ok {
		fmt.Printf("Host '%s' not found.\n", alias)
		return
	}

	key := targetHost.IdentityFile
	if key == "" {
		keys, _ := DiscoverKeys(filepath.Join(homeDir, ".ssh"))
		if len(keys) > 0 {
			key = keys[0]
		}
	}
	if key == "" {
		fmt.Println("No SSH key found to copy; add one with 'sshx edit' or ssh-keygen.")
		return
	}

	pubKey := expandHome(key, homeDir) + ".pub"
	if _, err := os.Stat(pubKey); err != nil {
		fmt.Printf("Public key %s not found.\n", pubKey)
		return
	}

	if err := copyPublicKey(pubKey, targetHost.Alias); err != nil {
		fmt.Fprintf(os.Stderr, "Copying public key failed: %v\n", err)
	}
}

func probeHostCLI(alias, homeDir string) {
	hosts, err := LoadAllHosts(homeDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading hosts: %v\n", err)
		os.Exit(1)
	}

	var targetHost *HostItem
	for _, h := range hosts {
		if strings.EqualFold(h.Alias, alias) {
			targetHost = &h
			break
		}
		for _, a := range h.AllAliases {
			if strings.EqualFold(a, alias) {
				targetHost = &h
				break
			}
		}
		if targetHost != nil {
			break
		}
	}

	if targetHost == nil {
		fmt.Fprintf(os.Stderr, "Host '%s' not found in SSH configuration.\n", alias)
		os.Exit(1)
	}

	host := targetHost.HostName
	if host == "" {
		host = targetHost.Alias
	}
	host = strings.Trim(host, "[]")
	port := targetHost.Port
	if port <= 0 {
		port = 22
	}

	target := net.JoinHostPort(host, strconv.Itoa(port))
	fmt.Printf("Probing TCP reachability for %s (%s)...\n", targetHost.Alias, target)

	start := time.Now()
	conn, err := net.DialTimeout("tcp", target, 2*time.Second)
	if err != nil {
		failBadge := badge(" UNREACHABLE ", colorWhite, colorRed)
		fmt.Printf("\n%s Failed to connect to %s: %v\n", failBadge, target, err)
		os.Exit(1)
	}
	_ = conn.Close()
	latency := time.Since(start).Milliseconds()
	okBadge := badge(" REACHABLE ", colorBlack, colorGreen)
	fmt.Printf("\n%s Connected to %s in %dms\n", okBadge, target, latency)
}
