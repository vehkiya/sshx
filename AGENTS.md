# Agent & Contributor Guidelines (`AGENTS.md`)

This document outlines the core architecture principles, development workflows, quality gates, and standards for developing and maintaining `sshx`.

All automated agents and human contributors must adhere to the rules in this document.

---

## 1. Non-Negotiable Pre-Commit Quality Gates

Before committing or submitting changes, all of the following steps **MUST** pass cleanly:

1. **Code Formatting (`gofmt`):**
   * Code must be formatted using the official Go formatter with simplification enabled:
     ```bash
     gofmt -s -w .
     ```
   * Ensure `git diff` shows zero formatting inconsistencies.

2. **Test Suite Must Pass:**
   * All package tests must pass with zero failures:
     ```bash
     go test -v -race ./...
     ```
   * The `-race` flag is mandatory to catch concurrency issues early.

3. **Static Analysis & Linting:**
   * Run `golangci-lint` to satisfy repository rules (including `errcheck`, `govet`, `staticcheck`, `gosec`, `unused`):
     ```bash
     golangci-lint run
     ```
   * Do not ignore lint warnings using `//nolint` unless accompanied by a justifiable reason in an inline comment.

4. **Dependency Hygiene:**
   * Ensure modules and sums are clean and tidy:
     ```bash
     go mod tidy
     git diff --exit-code go.mod go.sum
     ```

---

## 2. Architecture & Design Principles

### 2.0 Package Layout & Dependency Direction
* Code lives in packages under `internal/`; `main.go` holds only argument parsing and dispatch.
* Dependencies point one way, from `main` down to the leaves, and `TestDependenciesPointOneWay` (`architecture_test.go`) checks them in CI. When you add a package or an import across packages, update its `layers` map in the same change:
  * `.` (`main`) → `probe`, `sshconfig`, `tui`, `ui`, `update`, `wizard`.
  * `tui` → `probe`, `sshconfig`, `ui`, `update`.
  * `wizard` → `sshconfig`, `ui`.
  * `probe` → `sshconfig`.
  * `update` → `sshconfig`.
  * `sshconfig` and `ui` are leaves with zero internal dependencies.
* **Interactive Peer Isolation:** `tui` and `wizard` **never** import each other. `tui` presents the host browser and returns an action; `main` dispatches the requested action.
* **No package imports `main`.**

### 2.1 Zero External Runtime Dependencies
* `sshx` compiles down to a single static binary.
* Rely exclusively on the standard library and the official Charm libraries (`bubbletea`, `bubbles`, `huh`, `lipgloss`, all v2 from `charm.land`).
* Do not introduce heavy ORMs, dynamic runtime dependencies, or CGO.

### 2.2 Security & SSH Config Integrity
* **Strict Permissions**: Any write to `~/.ssh/config` must enforce `0600` permissions.
* **Atomic Writes**: Writes must use temporary files with randomized suffixes in the same directory, followed by an atomic rename (`os.Rename`), preventing configuration corruption during power outages or system interruptions.
* **Non-Destructive Parsing**: Modifying or removing host blocks must preserve comments, wildcards (`Host *`), and directives across other host definitions.
* **In-Place Edits**: Editing a host must only rewrite the directives whose values changed (`UpdateHost`); other aliases, patterns, unmanaged directives, and the section's position stay as they were.
* **Single Write Path**: Write SSH config files through `WriteConfigFile`, which keeps a hidden backup, writes atomically with `0600`, and preserves symlinks.

### 2.3 Unified Charm Design System
* Keep UI styling consistent with the `sshx` palette:
  * **Brand / Accent**: Charm Purple (`#7D56F4`)
  * **Headers / Selections**: Coral Pink (`#FF5F87`)
  * **Prompts / Cursors / Keys**: Vibrant Cyan (`#00D7D7`)
  * **Success / Badges**: Spring Green (`#5FD787`)
  * **Warnings / Password**: Amber (`#FFAF00`)
* Both the split-pane host browser TUI and the interactive Huh forms must adhere to this cohesive visual aesthetic.
* Print styled text with `lipgloss.Printf`, `lipgloss.Println` or `lipgloss.Fprint*`, not `fmt`. Lip Gloss v2 always styles for a full-color terminal; its print functions drop what the output can't show, so pipes and files get plain text.

### 2.4 Dual Invocation Modes
* `sshx` provides both an interactive terminal UI (`sshx`) and direct non-interactive CLI subcommands (`sshx <alias>`, `sshx add`, `sshx rm`, `sshx ls`, `sshx edit`).
* Maintain binary name awareness (`ssh-add-host` / `fssh-add`) for backwards-compatible symlinks.
