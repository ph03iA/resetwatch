# ResetWatch

A local CLI that emails you when an exhausted Codex usage window reaches its reset time. Written in Go, with durable SQLite alerts and delivery through your Resend account.

**Status:** v0.1.0 is a locally tested release candidate. The email integration is tested with simulated HTTP responses; live email delivery and inbox receipt have not been verified. There are no prebuilt binaries; build from source. This project does not operate a hosted watcher.

## Install

The installation page is local in `site/dist/`. From the project root, run `node tools/serve.mjs` and open [the local page](http://127.0.0.1:4173). This project has not been published.

There are no prebuilt downloads. Install Git and Go 1.27.2 or newer, then clone and build:

```sh
git clone https://github.com/ph03iA/resetwatch.git
cd resetwatch
go build -o resetwatch ./cmd/resetwatch
./resetwatch version
```

On Windows PowerShell, build `resetwatch.exe` instead and run it as `.\resetwatch.exe`:

```powershell
go build -o resetwatch.exe ./cmd/resetwatch
.\resetwatch.exe version
```

To use `resetwatch` from any directory, run `go install ./cmd/resetwatch` instead. Go installs to `GOBIN`, or to `GOPATH/bin` when `GOBIN` is unset; add that directory to your `PATH` if needed. The module path is local (`resetwatch`), so `go install github.com/...@latest` does not work.

To test email delivery, run this from the cloned folder:

```powershell
.\resetwatch.exe test-email --email 'YOUR_RESEND_ACCOUNT_EMAIL'
```

Before testing, create `.env` from `.env.example` in the project folder if it does not already exist, and paste your Resend key after `RESEND_API_KEY=`. Keep the command's working directory in that folder. Replace the recipient with your own value. This sends one email labeled **TEST**, without connecting to Codex or opening the state database. The default sender is `ResetWatch <onboarding@resend.dev>`, which can only send to your own Resend account email unless you use a verified sender domain. Provider acceptance does not guarantee inbox receipt. There are no automatic retries; rerunning the command sends another test.

## Connect and watch

Requirements: a native Codex executable, a ChatGPT subscription sign-in exposing quota data, and a Resend API key. You can test without a domain by sending from `onboarding@resend.dev` to the email address associated with your Resend account. Sending to other recipients requires a domain verified in Resend. API-key-only Codex accounts do not provide the supported account quota interface.

First, sign in through Codex itself:

```sh
codex login
resetwatch connect codex
resetwatch notify --email you@example.com --from 'ResetWatch <onboarding@resend.dev>'
```

Replace `you@example.com` with your Resend account email for this no-domain test. For other recipients, replace the sender with an address on your verified domain. See [Resend's test-sender restrictions](https://resend.com/docs/knowledge-base/403-error-resend-dev-domain). If Codex is not on `PATH`, or your installation exposes a shell shim, pass the actual native executable:

```sh
resetwatch connect codex --codex-bin /absolute/path/to/codex
```

On Windows, use the native `codex.exe` path. If you installed Codex with npm, only a `codex.cmd` shim is on `PATH`; the native executable is at `%APPDATA%\npm\node_modules\@openai\codex\node_modules\@openai\codex-win32-x64\vendor\x86_64-pc-windows-msvc\bin\codex.exe`. `.cmd`, `.bat`, and `.ps1` shims are intentionally unsupported. ResetWatch never runs a shell to interpret a configured executable path.

An explicit `--codex-bin` value is resolved and saved as an absolute path during connection, including relative paths and names found on `PATH`. Later directory or PATH changes do not change that saved executable. Without this flag, ResetWatch continues to look up Codex on `PATH` at runtime. If you previously saved a relative path, reconnect once with `--codex-bin` to normalize it.

Save your Resend key once in a file named `.env` in the folder where you run ResetWatch. If no `.env` exists yet, copy the supplied `.env.example` and edit the copy:

```dotenv
RESEND_API_KEY=re_your_actual_resend_key
```

Then run the watcher from that folder, including in a new terminal session:

```sh
resetwatch watch
```

Both `watch` and `test-email` load this key automatically. A nonempty `RESEND_API_KEY` already set in the process environment takes priority, and the file is not read in that case. Only `.env` in the current working directory is searched; the binary's folder, parent directories and state directory are not searched. Only `RESEND_API_KEY` is read. Plain values and matching single/double quotes are supported, along with blank lines, UTF-8 BOM, CRLF and comments. An unquoted `#` starts a comment; a quoted `#` remains literal. This is a key file rather than a shell script: use `RESEND_API_KEY=...` without `export`. Escape sequences, variable expansion and multiline values are not supported, and no shell commands run. Duplicate key assignments, malformed values, non-regular files and files over 64 KiB are rejected without exposing their contents.

The `.env` file contains a plaintext secret. Keep it private, restrict it to your user (`chmod 600 .env` on Unix), and do not commit or share it. Git and release packaging exclude real environment files; `.env.example` contains no key. File-loaded values are not exported to the process environment or the Codex subprocess. The key is not stored in SQLite. Process environment variables remain supported for services and secret managers. Keep the watcher running on an awake, online computer. `Ctrl+C` stops it safely; saved alerts resume on the next run.

The file is loaded when a command starts. Restart a running watcher after changing its key.

To check email delivery immediately with a binary on your `PATH`, run `resetwatch test-email --email 'YOUR_RESEND_ACCOUNT_EMAIL'` from the folder containing `.env`. No quota exhaustion is needed. To use your own verified domain, add `--from 'ResetWatch <alerts@yourdomain.com>'`.

## Commands

| Command | Behavior |
| --- | --- |
| `resetwatch connect codex [--codex-bin PATH]` | Read quota through the local Codex app-server and save the connection. |
| `resetwatch notify --email ADDRESS --from SENDER` | Save one recipient and sender; send no email. |
| `resetwatch test-email --email ADDRESS [--from SENDER]` | Send one TEST email immediately; no Codex access, saved state, or automatic retries. |
| `resetwatch watch [--interval 1m] [--once]` | Refresh quotas and deliver due alerts. Interval must be 15 seconds to 1 hour. |
| `resetwatch status [--refresh] [--json]` | Show the saved observation; `--refresh` requests current data first. |
| `resetwatch history [--limit 20] [--json]` | Show alert states and delivery attempts. Limit: 1–1000. |
| `resetwatch retry --id FULL_ALERT_ID` | Requeue an eligible failed alert, keeping the same delivery key and frozen message. |
| `resetwatch demo` | Display labeled sample data without opening accounts or sending email. |
| `resetwatch version` | Print the version. |

Use `resetwatch history --json` to obtain the full alert ID for a retry. A manual retry requires a failed alert, fewer than eight attempts, and a time within both the first-attempt retry window and the reset-age cutoff. It queues delivery; the watcher performs the send and cancels the alert if settings or the detected account have changed.

## How alerts work

- An alert is armed when a reported window is exhausted (`used_percent >= 100`) and notification settings exist. The tool does not email for every routine quota check.
- If Codex restores an exhausted window before its recorded reset time, the watcher emails at once instead of waiting. Windows that were never exhausted never send email.
- At the recorded reset time, the watcher checks fresh quota data. A **confirmed** email requires fresh data showing the relevant window and every other reported window below exhaustion. A remaining exhausted window prevents that claim.
- If fresh quota data is unavailable, the alert says **expected reset**. A reset timestamp alone does not prove access is available.
- Pending alerts and delivery leases survive process restarts. A changed account, recipient, sender, or future reset cancels obsolete pending alerts.
- Network failures, HTTP 408/429, server errors, and Resend's temporary `409 concurrent_idempotent_requests` response are retried with exponential backoff and bounded `Retry-After` handling. Other conflicts and delivery failures stop automatic retries.
- Delivery uses one stable idempotency key and an immutable message payload per alert. Resend's deduplication window is 24 hours; ResetWatch stops retrying before 23 hours from the first attempt, with at most eight attempts. Manual retries keep those limits.
- Recovering an expired delivery lease also respects the eight-attempt limit. An interrupted final attempt becomes failed when its lease expires; it is not sent a ninth time.
- Alerts older than 24 hours past their reset are cancelled. A computer waking after a week does not send a week of stale reminders.
- `sent` means Resend returned an accepted receipt. It does not guarantee inbox delivery or that a provider will accept your next AI request.

## Local data and privacy

Default state location:

| OS | Database |
| --- | --- |
| Windows | `%AppData%\resetwatch\state.db` |
| macOS | `~/Library/Application Support/resetwatch/state.db` |
| Linux | `$XDG_CONFIG_HOME/resetwatch/state.db`, or `~/.config/resetwatch/state.db` |

Set `RESETWATCH_HOME` to choose a different state directory. Keep it private and local. SQLite may also create `state.db-wal` and `state.db-shm` files while running.

The database contains recipient/sender addresses, quota snapshots, a hashed account identity, pending alert payloads, and history. It is not encrypted. On Unix, the application requests directory mode `0700` and database mode `0600`; Windows access control depends on the directory's ACLs. Protect backups too.

Codex credentials remain with Codex. ResetWatch calls `initialize`, `account/read` with `refreshToken: false`, and `account/rateLimits/read` over the local app-server protocol; it makes no inference requests and does not read Codex authentication files. Codex itself may contact its provider while servicing these calls. Email recipient, sender, and alert content go to Resend over HTTPS. The website loads no analytics or third-party scripts.

## Keep it running

A foreground terminal is sufficient for desktop use. Closing it usually stops the watcher. Run on an always-on host if you need notifications while your laptop is off; that host needs its own configured Codex sign-in and email environment.

For Linux, this optional **example** uses a user systemd service. Substitute your real absolute binary paths and set the native Codex path using `connect` before enabling it. Create `~/.config/resetwatch/resend.env` privately, containing `RESEND_API_KEY=your_real_key`, then set its permissions to `0600`. Do not commit the file.

```ini
# ~/.config/systemd/user/resetwatch.service
[Unit]
Description=ResetWatch quota reset notifications

[Service]
Type=simple
ExecStart=/absolute/path/to/resetwatch watch
EnvironmentFile=%h/.config/resetwatch/resend.env
Restart=on-failure
RestartSec=15

[Install]
WantedBy=default.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now resetwatch
journalctl --user -u resetwatch
```

This does not configure systemd automatically. A user service normally requires an active user session; consult your host's service policy if it must run after logout. Supply `RESETWATCH_HOME` in the service if you used a custom state directory.

## Boundaries

Only Codex is supported in this release. One connected account and one email configuration are stored per state directory. The account key hashes the normalized email address returned by Codex, so same-email workspace/account-context switches cannot be reliably distinguished. Use separate state directories and matching Codex contexts when isolation is needed; verify quota after switching. Do not assume cross-workspace identity isolation.

The Codex protocol and account eligibility can change. Missing or malformed quota data is an error, never zero usage. This tool cannot bypass limits, recover an expired sign-in, wake your laptop, or promise mail delivery. For production rollout, validate your actual Codex installation, sender domain, and inbox before relying on alerts.

## Develop and verify

```sh
go test ./...
go vet ./...
go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 10 -ignore '_test.go' cmd internal
node --check site/dist/app.js
```

CI also checks formatting and runs `go test -race ./...` on Linux. The SQLite driver is pure Go, so regular builds do not require a C compiler. Race instrumentation requires a supported Go platform and C toolchain.

Serve `site/dist/` with a static server to preview the website. The page provides platform-specific build commands, product behavior and operating notes. The website does not connect to accounts or collect API keys. Manrope and Instrument Serif are self-hosted with their SIL Open Font License notices in `site/dist/fonts/`. See [AGENTS.md](AGENTS.md) for the complexity and implementation rules, [SECURITY.md](SECURITY.md) for handling sensitive data, and [CHANGELOG.md](CHANGELOG.md) for release scope.
