# Security

## Supported scope

This is the initial v0.1.0 release candidate. Use the latest available source or packaged build. Platform archives are unsigned; compare their SHA-256 values against checksums obtained through a trusted channel. Checksums alone do not authenticate a compromised release channel.

## Credentials and local data

- Sign in using Codex itself. ResetWatch does not read Codex authentication files, export account tokens, or make inference requests. The native Codex process owns its authentication and provider connections.
- Supply `RESEND_API_KEY` through the process environment or a private `.env` file in the current working directory. A nonempty process value wins without reading the file. Only the email key is loaded from the file, and it is not exported to Codex or saved in SQLite. Environment files contain plaintext secrets: restrict access to your user (`chmod 600 .env` on Unix; private ACLs on Windows), protect backups, and never commit them. Real environment files are excluded from Git and release archives; the included `.env.example` is blank. Environment variables remain accessible to sufficiently privileged local processes. Use a restricted sending key where your email provider supports it.
- The SQLite database stores email addresses, hashed account identity, quota snapshots, alert payloads, and delivery history. It is not encrypted. Protect the state directory, SQLite sidecars, logs, and backups with operating system permissions and disk encryption as appropriate.
- Unix file modes are requested as `0700` for the state directory and `0600` for the database. Windows protection depends on inherited ACLs. Do not point `RESETWATCH_HOME` at a shared or attacker-controlled directory.
- `--codex-bin` is executable configuration. Point it only at a trusted native binary. ResetWatch rejects Windows shell shims and starts the process directly without a shell.

## Network and delivery

Production email requests use `https://api.resend.com/emails`. Redirects are refused so the API key cannot be forwarded to another endpoint. Requests have bounded timeouts; responses are size limited, and raw response bodies are not printed in errors.

Alert payloads are frozen before delivery and reused with a stable idempotency key. Automatic and manual retries stay within the local attempt and age limits, keeping the retry window shorter than Resend's 24-hour idempotency period. This reduces duplicate sends but is not an unlimited exactly-once delivery guarantee. An accepted receipt does not prove inbox delivery.

The recipient, sender, reset timestamp, and quota-window labels are sent to Resend. The static website does not access accounts, receive keys, or run analytics.

## Identity limitation

ResetWatch identifies a supported Codex account using a SHA-256 hash of its normalized email. Hashing is pseudonymization, not encryption. Same-email workspace changes cannot be reliably distinguished; the product must not be used as a workspace isolation or authorization control.

## Reporting a vulnerability

No public security inbox or repository has been configured yet. Report issues privately to the person or organization that supplied your copy. Include affected version, reproduction steps, and sanitized impact evidence. Do not include API keys, Codex credentials, your state database, full email addresses, or private account output. Avoid public disclosure of secrets while a fix is being coordinated.
