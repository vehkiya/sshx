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

- **Permissions**: `sshx` strictly maintains `0600` permissions on `~/.ssh/config` and uses atomic temporary writes with random suffixes to prevent race conditions and file tampering. The previous version of every file it modifies is kept as a hidden `.<name>.sshx.bak` backup alongside it.
- **Updates**: `sshx update` refuses to install a binary unless the release's `checksums.txt` carries a valid Ed25519 signature (`checksums.txt.sig`) from a key built into sshx, and the archive's SHA-256 matches that file. Release archives also carry GitHub build provenance attestations; verify a download with `gh attestation verify <archive> --repo vehkiya/sshx`.

## Release Signing

Every release's `checksums.txt` is signed with the sshx release key:

```text
-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA8BjKVqaALl5z4zMcLMFM5Yvm+CZ7qyStyVqZZeefLMQ=
-----END PUBLIC KEY-----
```

To verify a download manually, save the key above as `sshx-release.pub.pem`, then:

```bash
base64 -d checksums.txt.sig > checksums.sig
openssl pkeyutl -verify -pubin -inkey sshx-release.pub.pem -rawin -in checksums.txt -sigfile checksums.sig
sha256sum --check --ignore-missing checksums.txt
```

**Key rotation (maintainers):** installed binaries only trust the keys listed in `TrustedKeys` (`internal/update/update.go`). Add the new public key there and ship a release still signed with the old key; once users have that release, switch the `SSHX_SIGNING_KEY` secret in the `release` environment to the new key and remove the old one from the list. The release workflow refuses to sign with a key that isn't listed.
- **Passphrase Protection**: Always protect private keys with strong passphrases and use an SSH agent (or hardware security key) to handle decryption.
- **Agent Hygiene**: Utilize `AddKeysToAgent yes` with appropriate key lifetimes or OS keychain integration to minimize key exposure in memory.
