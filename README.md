# sshx 🚀

[![Release](https://github.com/vehkiya/sshx/actions/workflows/cd.yml/badge.svg)](https://github.com/vehkiya/sshx/actions/workflows/cd.yml)
[![CodeQL Analysis](https://github.com/vehkiya/sshx/actions/workflows/codeql.yml/badge.svg)](https://github.com/vehkiya/sshx/actions/workflows/codeql.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/vehkiya/sshx)](https://goreportcard.com/report/github.com/vehkiya/sshx)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**sshx** is a modern, responsive TUI SSH connection manager and OpenSSH configuration wizard built in Go with the [Charm](https://charm.sh) stack ([`bubbletea`](https://github.com/charmbracelet/bubbletea), [`huh`](https://github.com/charmbracelet/huh), and [`lipgloss`](https://github.com/charmbracelet/lipgloss)).

It provides an intuitive dual-pane terminal interface to search, inspect, connect, configure, and manage remote servers without manually editing `~/.ssh/config`.

---

## ✨ Features

* **Dual-Pane TUI Inspector**:
  * Real-time fuzzy filtering across all configured host aliases and hostnames.
  * Live detail pane featuring connection targets, port status, authentication badges, proxy jumps, and exact CLI command previews.
  * Responsive layout that adapts smoothly between wide terminal splits and compact mobile/single-column views.
* **Interactive Configuration Wizard (`sshx add`)**:
  * Step-by-step guided host setup powered by `huh` and styled with custom Lip Gloss borders and badges.
  * Automated private key discovery from `~/.ssh`.
  * On-the-fly Ed25519 key generation (`ssh-keygen`) with automated comment tagging.
  * One-click public key deployment to remote hosts via `ssh-copy-id`.
  * Real-time syntax-colored preview card of the generated SSH configuration block.
* **Strict OpenSSH File Integrity**:
  * Non-destructive parsing that preserves inline comments, directives, and wildcard blocks (`Host *`).
  * Atomic writes via randomized temporary files to prevent configuration corruption.
  * Automatic `0600` permission enforcement on `~/.ssh/config`.
  * Support for included configuration fragments (`~/.ssh/config.d/*`).
* **Instant CLI Shortcuts**:
  * Connect directly by alias: `sshx <alias>`.
  * Non-interactive target parsing: `sshx add user@192.168.1.100:2222 prod-server`.
  * Zero external runtime dependencies — single static binary.

---

## 📦 Installation

### Via `go install` (Recommended)

```bash
go install github.com/vehkiya/sshx@latest
```

Ensure `$(go env GOPATH)/bin` (typically `~/go/bin`) is in your `$PATH`.

### From Precompiled Release Binaries

Download prebuilt binaries for Linux, macOS (Apple Silicon / Intel), or Windows from the [Releases page](https://github.com/vehkiya/sshx/releases).

```bash
# Example for Linux AMD64:
curl -sSL https://github.com/vehkiya/sshx/releases/latest/download/sshx_linux_amd64.tar.gz | tar -xz
sudo mv sshx /usr/local/bin/
```

### Build from Source

```bash
git clone https://github.com/vehkiya/sshx.git
cd sshx
go build -trimpath -ldflags="-s -w" -o sshx .
mv sshx ~/.local/bin/
```

---

## ⌨️ TUI Keyboard Shortcuts

Launch the interactive host manager simply by typing `sshx`:

| Key | Action |
| :--- | :--- |
| `↑` / `k` | Move cursor up |
| `↓` / `j` | Move cursor down |
| `/` | Filter / fuzzy search hosts and notes |
| `Enter` | Connect to selected host immediately |
| `a` | Add a new host (launches interactive wizard) |
| `e` | Edit selected host (launches interactive wizard) |
| `D` | Duplicate / clone selected host |
| `c` | Copy public key to remote host (`ssh-copy-id`) |
| `y` | Yank SSH connect command to clipboard (OSC 52) |
| `p` | Probe TCP reachability / ping host |
| `v` | Toggle raw OpenSSH config view |
| `d`, `x` | Delete selected host (with in-TUI confirmation) |
| `E` | Open `~/.ssh/config` directly in `$EDITOR` |
| `Tab` | Toggle host details inspector (on compact displays) |
| `q` / `Esc` | Exit |

---

## 🚀 CLI Commands

```bash
# Launch interactive TUI host manager
sshx

# Connect directly to a host alias
sshx prod-server

# Interactively add a new SSH host
sshx add

# Pre-populate connection target and alias
sshx add admin@10.0.0.5:2202 backup-node

# Interactively edit an existing SSH host
sshx edit prod-server

# List all configured SSH hosts in terminal
sshx ls

# Remove a host from ~/.ssh/config
sshx rm prod-server

# Open ~/.ssh/config in your configured $EDITOR (vim/nvim)
sshx edit

# View version info
sshx --version
```

---

## 🛠️ Development & Quality Gates

This repository adheres to strict pre-commit quality standards outlined in [`AGENTS.md`](AGENTS.md):

```bash
# Run unit tests with race detector
go test -v -race ./...

# Run static analysis and linting
golangci-lint run

# Format code
gofmt -s -w .
```

---

## 📄 License

Distributed under the [MIT License](LICENSE).
