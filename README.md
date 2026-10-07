# sshx 🚀

[![Release](https://github.com/vehkiya/sshx/actions/workflows/cd.yml/badge.svg)](https://github.com/vehkiya/sshx/actions/workflows/cd.yml)
[![CodeQL Analysis](https://github.com/vehkiya/sshx/actions/workflows/codeql.yml/badge.svg)](https://github.com/vehkiya/sshx/actions/workflows/codeql.yml)
[![Linted with golangci-lint](https://img.shields.io/badge/linted%20with-golangci--lint-00ADD8?logo=go&logoColor=white)](https://golangci-lint.run)
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
  * <kbd>Shift</kbd>+<kbd>Tab</kbd> goes back to any earlier step, keeping your answers; <kbd>Esc</kbd> cancels from any step. Nothing is written, and no key is generated, until you save on the last step.
  * Automated private key discovery from `~/.ssh`.
  * Path completion in the custom key and new key inputs: <kbd>Tab</kbd> accepts the suggestion shown (keys and folders, starting with `~/.ssh/`).
  * On-the-fly Ed25519 key generation (`ssh-keygen`) with automated comment tagging.
  * Remembering a new key's passphrase: on macOS with Apple's `ssh`, sshx offers to run `ssh-add --apple-use-keychain <key>`, which asks for the passphrase once more, keeps it in the login Keychain and loads the key into the agent. If `~/.ssh/config` doesn't set `UseKeychain yes` and `AddKeysToAgent yes` for the host, sshx says to add them under `Host *`, so ssh still finds the passphrase after you log in again. Elsewhere (or with another `ssh`, such as Homebrew's, which has no Keychain support) it suggests `ssh-add <key>`.
  * One-click public key deployment to remote hosts via `ssh-copy-id`.
  * Real-time syntax-colored preview card of the generated SSH configuration block.
* **Strict OpenSSH File Integrity**:
  * Edits happen in place: only the directives you change are rewritten. Other aliases on the `Host` line, wildcard patterns, comments, and directives sshx doesn't manage (`ForwardAgent`, `LocalForward`, ...) are left untouched.
  * Deleting a host removes its aliases and notes without disturbing neighbouring hosts' comments; wildcard patterns that shared the `Host` line are kept.
  * Atomic writes via randomized temporary files to prevent configuration corruption.
  * The previous version of each file is kept as a hidden backup alongside it (e.g. `~/.ssh/.config.sshx.bak`).
  * Symlinked configs (e.g. managed by a dotfiles repo) are written through, not replaced.
  * Automatic `0600` permission enforcement on `~/.ssh/config`.
  * Understands both `Key Value` and `Key=Value` syntax, quoted values, and nested `Include` directives, plus fragments in `~/.ssh/config.d/*`.
* **Instant CLI Shortcuts**:
  * Connect directly by alias: `sshx <alias>`, or run a remote command: `sshx <alias> uptime`. ssh's exit code is passed through.
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

Download prebuilt binaries for macOS, Linux, or Windows from the [Releases page](https://github.com/vehkiya/sshx/releases).

#### macOS (Apple Silicon / M-series - `arm64`)

```bash
curl -sSL https://github.com/vehkiya/sshx/releases/latest/download/sshx_darwin_arm64.tar.gz | tar -xz --strip-components=1
sudo mv sshx /usr/local/bin/
```

#### macOS (Intel - `amd64`)

```bash
curl -sSL https://github.com/vehkiya/sshx/releases/latest/download/sshx_darwin_amd64.tar.gz | tar -xz --strip-components=1
sudo mv sshx /usr/local/bin/
```

#### Linux (x86_64 / `amd64`)

```bash
curl -sSL https://github.com/vehkiya/sshx/releases/latest/download/sshx_linux_amd64.tar.gz | tar -xz --strip-components=1
sudo mv sshx /usr/local/bin/
```

#### Linux (ARM64 / `arm64`)

```bash
curl -sSL https://github.com/vehkiya/sshx/releases/latest/download/sshx_linux_arm64.tar.gz | tar -xz --strip-components=1
sudo mv sshx /usr/local/bin/
```

#### Windows

Download `sshx_windows_amd64.zip` from the [Releases page](https://github.com/vehkiya/sshx/releases/latest), extract `sshx.exe`, and place it in your `%PATH%`.

#### Verifying a Download

Release archives carry GitHub build provenance attestations. With the [GitHub CLI](https://cli.github.com):

```bash
gh attestation verify sshx_linux_amd64.tar.gz --repo vehkiya/sshx
```

### Build from Source

```bash
git clone https://github.com/vehkiya/sshx.git
cd sshx
go build -trimpath -ldflags="-s -w" -o sshx .
sudo mv sshx /usr/local/bin/
```

### 🔄 Auto-Update & Upgrades

Keep `sshx` up to date with the built-in self-updater:

```bash
# Check and upgrade to the latest GitHub release
sshx update

# Check if a new version is available without upgrading
sshx update --check
```

In the interactive TUI, `sshx` checks for new releases in the background (at most every 6 hours, cached in your user cache directory). If an update is available, an **`UPDATE`** badge appears in the top-right inspector and pressing **`U`** upgrades the binary directly in-place. Set `SSHX_NO_UPDATE_CHECK=1` to disable the background check.

The updater only installs a binary from a release whose `checksums.txt` is signed with the sshx release key and whose archive matches that checksum; anything unsigned, signed by another key, or tampered with is refused. See [SECURITY.md](SECURITY.md#release-signing) to verify downloads manually.

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
| `U` | Upgrade sshx to latest release |
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

# Run a remote command (exit code is passed through)
sshx prod-server uptime

# Pass any target straight to ssh
sshx connect admin@10.0.0.5

# Interactively add a new SSH host
sshx add

# Pre-populate connection target and alias
sshx add admin@10.0.0.5:2202 backup-node

# Interactively edit an existing SSH host
sshx edit prod-server

# Duplicate / clone an existing SSH host
sshx clone prod-server

# Probe TCP reachability / ping host
sshx probe prod-server

# Check for updates and automatically upgrade
sshx update

# Check if a new version is available without upgrading
sshx update --check

# List all configured SSH hosts in terminal
sshx ls

# Output hosts as structured JSON for scripting
sshx ls --json

# Remove a host (and all of its aliases) from the config file that defines it
sshx rm prod-server

# Open ~/.ssh/config in $VISUAL / $EDITOR (falls back to nvim, vim, vi, nano)
sshx edit

# View version info
sshx --version
```

### Accessibility (`ACCESSIBLE=1`)

Set `ACCESSIBLE=1` for plain, non-interactive line-by-line prompts designed for screen readers and scripted execution:
- `ACCESSIBLE=1 sshx` outputs the host table (`sshx ls`) instead of launching the full-screen interactive browser.
- Wizards (`add`, `edit`, `clone`, `rm`) run one prompt per line, making them operable with screen readers or by piping scripted inputs to `stdin`.

### Machine-Readable Output (`sshx ls --json`)

`sshx ls --json` prints a plain JSON array of host objects without terminal styling or ANSI escape sequences:
- `alias`: Primary host alias
- `hostName`: Configured HostName / IP target
- `user`: Remote username
- `port`: Connection port
- `identityFile`: Path to private key file
- `auth`: Authentication method (`"key"`, `"password"`, or `"default"`)
- `configFile`: Path to the config file where the host block is defined (including nested `Include` files)

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

# Check module hygiene
go mod tidy -diff
```

---

## 📄 License

Distributed under the [MIT License](LICENSE).
