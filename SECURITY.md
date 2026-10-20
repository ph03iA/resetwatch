# Security

## Supported scope

This is the initial v0.1.0 release candidate. There are no prebuilt binaries; build from the latest source in the GitHub repository.

## Credentials and local data

- Sign in using Codex itself. ResetWatch does not read Codex authentication files, export account tokens, or make inference requests. The native Codex process owns its authentication and provider connections.
- Gmail is connected through Google's desktop sign-in with PKCE and a random state, asking only for `gmail.send`, `openid` and `email`; ResetWatch cannot read the inbox. The refresh token is stored in the operating system's credential store (Windows Credential Manager, macOS Keychain, Linux Secret Service), never in a file or the database, and there is no plaintext fallback. Access tokens stay in memory. Tokens are never printed. Anyone who can read your credential store as your user can send email as you through this grant; remove it at myaccount.google.com/permissions if a device is lost.
- The SQLite database stores your Gmail address and Google account ID, hashed AI account identity, quota snapshots, alert payloads, and delivery history. It is not encrypted. Protect the state directory, SQLite sidecars, logs, and backups with operating system permissions and disk encryption as appropriate.
- Unix file modes are requested as `0700` for the state directory and `0600` for the database. Windows protection depends on inherited ACLs. Do not point `RESETWATCH_HOME` at a shared or attacker-controlled directory.
- `--codex-bin` is executable configuration. Point it only at a trusted native binary. ResetWatch rejects Windows shell shims and starts the process directly without a shell.

## Network and delivery

Email goes to `https://gmail.googleapis.com/gmail/v1/users/me/messages/send`, from the connected account to itself. Redirects are refused so the access token cannot be forwarded to another endpoint. Requests have bounded timeouts; responses are size limited, and raw response bodies are not printed in errors.

Messages are rendered once and frozen with the reminder, so retries carry identical content and a stable Message-ID. Gmail offers no idempotency, so a retry after a lost answer can deliver twice. Retries stay within eight attempts and 23 hours. An accepted receipt does not prove inbox delivery.

The email (your address, reset time and quota-window labels) is sent to Google. The static website does not access accounts, receive keys, or run analytics.

## Identity limitation

ResetWatch identifies a supported Codex account using a SHA-256 hash of its normalized email. Hashing is pseudonymization, not encryption. Same-email workspace changes cannot be reliably distinguished; the product must not be used as a workspace isolation or authorization control.

## Reporting a vulnerability

No public security inbox or repository has been configured yet. Report issues privately to the person or organization that supplied your copy. Include affected version, reproduction steps, and sanitized impact evidence. Do not include API keys, Codex credentials, your state database, full email addresses, or private account output. Avoid public disclosure of secrets while a fix is being coordinated.
