# Security Policy

## Supported Versions

I currently support the latest release with security updates. Users are encouraged to stay up-to-date with the latest version.

| Version | Supported          |
| ------- | ------------------ |
| Latest  | :white_check_mark: |
| Older   | :x:                |

## Reporting a Vulnerability

I take the security of `sshx` seriously. If you discover a security vulnerability within this project, please follow these steps to report it:

1. **Do not open a public issue.** This could expose the vulnerability to malicious actors before a patch is available.
2. Please report the vulnerability using **GitHub's private vulnerability reporting feature**:
   - Navigate to the **Security** tab of the repository.
   - Click on **Advisories** in the sidebar.
   - Click **Report a vulnerability**.
3. Provide a detailed description of the vulnerability, including:
   - A description of the issue.
   - Steps to reproduce it.
   - Potential impact.
   - Suggested mitigations (if any).

I aim to acknowledge your report within 48 hours and provide an estimated timeline for addressing the issue. I will keep you updated on the progress and notify you when the fix is released.

## Security Best Practices

When using `sshx`:

- **Permissions**: `sshx` strictly maintains `0600` permissions on `~/.ssh/config` and uses atomic temporary writes with random suffixes to prevent race conditions and file tampering.
- **Passphrase Protection**: Always protect private keys with strong passphrases and use an SSH agent (or hardware security key) to handle decryption.
- **Agent Hygiene**: Utilize `AddKeysToAgent yes` with appropriate key lifetimes or OS keychain integration to minimize key exposure in memory.
