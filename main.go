package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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

		case "edit":
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
	for {
		choice, action, err := RunTUI(homeDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
			os.Exit(1)
		}

		switch action {
		case "connect":
			if choice != "" {
				connectSSH(choice)
			}
			return

		case "add":
			_, _, _ = AddHostWizard("", "", homeDir, false)
			// Loop back to update list

		case "delete":
			if choice != "" {
				_ = DeleteHostPrompt(choice, homeDir)
			}
			// Loop back to update list

		case "copy-id":
			if choice != "" {
				runCopyIDForHost(choice, homeDir)
			}

		case "edit":
			openEditor(homeDir)

		case "quit", "":
			return
		}
	}
}

func printUsage() {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205")).Render("sshx — TUI SSH Connection Manager")
	fmt.Printf("%s\n\n", header)
	fmt.Println("Usage:")
	fmt.Println("  sshx                      Launch interactive host browser")
	fmt.Println("  sshx <alias>              Directly connect to host")
	fmt.Println("  sshx add [target] [alias] Interactively add a new SSH host")
	fmt.Println("  sshx ls                   List all configured SSH hosts")
	fmt.Println("  sshx rm <alias>           Remove a host from ~/.ssh/config")
	fmt.Println("  sshx edit                 Open ~/.ssh/config in $EDITOR")
	fmt.Println()
	fmt.Println("TUI Keybindings:")
	fmt.Println("  Enter      Connect to selected host")
	fmt.Println("  /          Filter / search hosts")
	fmt.Println("  a          Add new host (launches wizard)")
	fmt.Println("  d, x       Delete selected host")
	fmt.Println("  c          Copy public key to host (ssh-copy-id)")
	fmt.Println("  e          Edit ~/.ssh/config in $EDITOR")
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

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
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
	var targetHost *HostItem
	for _, h := range hosts {
		if strings.EqualFold(h.Alias, alias) {
			targetHost = &h
			break
		}
	}

	if targetHost == nil {
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

	if key != "" && strings.HasPrefix(key, "~/") {
		key = filepath.Join(homeDir, key[2:])
	}

	pubKey := key + ".pub"
	if _, err := os.Stat(pubKey); err != nil {
		fmt.Printf("Public key %s not found.\n", pubKey)
		return
	}

	user := targetHost.User
	if user == "" {
		user = os.Getenv("USER")
	}
	host := targetHost.HostName
	if host == "" {
		host = targetHost.Alias
	}

	copyKeyCmd(pubKey, targetHost.Port, user, host)
}
