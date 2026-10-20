# ResetWatch details

The [README](../README.md) covers install, setup and everyday use. This page has the exact rules.

## Alerts

- An alert is armed when a reported window reaches 100% and Gmail is connected. Routine checks never send email.
- If a provider shows a new window (a later reset time) before the recorded reset, the email is sent at once instead of waiting.
- At the reset time the watcher reads usage again. An **is back** email needs a reading taken after the reset and at most two minutes old, with the alert's window below 100% and no other window still at 100%.
  - **Codex** must also report `ordinaryUsageAllowed: true`. Older Codex versions without that field get **should be back**.
  - **Claude** has no such flag; fresh windows below 100% are the evidence. After a reset Claude leaves the window out until you use Claude again, so a fresh reading without it counts as unused.
- If another window is still at 100%, the email adds a warning and says **should be back**.
- A Claude reading may omit a window; that means unknown, so the last known value and its reminder are kept until it resets. Claude reset times are rounded to the minute because Claude recomputes them with sub-second drift.
- The email shows the reset time in the time zone of the computer running the watcher, plus the exact UTC time. Setup prints that zone.

## Delivery and retries

- Each reminder is rendered once into a MIME message with a stable Message-ID and frozen with the reminder, so every retry sends identical content. An email first tried while offline keeps its **should be back** wording.
- Gmail documents no idempotency for `users.messages.send`. When an answer is lost or unreadable, ResetWatch retries and Gmail may deliver the reminder twice; this is an accepted limitation.
- Network failures, HTTP 408 and 429, Gmail rate-limit errors, server errors and unreadable receipts are retried with exponential backoff and bounded `Retry-After`. Other failures stop automatic retries.
- At most eight attempts, all within 23 hours of the first. An interrupted final attempt becomes failed when its lease expires.
- Alerts more than 24 hours past their reset are cancelled, so a computer waking after a week does not send old reminders.
- Pending alerts and delivery leases survive restarts. A changed AI account cancels unsent alerts for the old one.
- Deselecting a provider in setup cancels its unsent reminders. Re-enabling it does not replay old ones; a new reading can arm a future reset.

## Gmail sign-in and credentials

- Sign-in uses Google's desktop flow: your system browser, authorization code with PKCE (S256), a random `state` and a temporary callback on `127.0.0.1` that gives up after five minutes. ResetWatch asks only for `gmail.send`, `openid` and `email`, and reads the verified address and stable account ID from Google's userinfo endpoint.
- The refresh token goes to the system credential store under a new name for every successful sign-in, namespaced by state folder. If the store is unavailable, connecting fails; there is no plaintext fallback. Access tokens are renewed automatically and kept in memory only.
- A new sign-in is saved before it is activated. If anything fails before activation, the new credential is deleted and the existing connection keeps working.
- Reminders are bound to the Google account ID. Reconnecting the same account keeps them. Connecting a different account moves unsent reminders to it, and switching back resumes them. If a delivery is in progress, the change waits until it finishes or its two-minute lease expires.
- Before claiming reminders, the watcher renews the access token. If Google refuses the sign-in (removed access, or a sign-in Google ended), delivery pauses for that credential only, the reminder in progress gets its attempt back, and usage is still read. A failure from an old credential never pauses a newer connection. `gmail connect` clears the pause.
- `gmail disconnect` cancels unsent reminders and turns delivery off first, then deletes the local credential. It does not withdraw access at Google; do that at myaccount.google.com/permissions.
- `gmail status` reports what this computer knows. It does not contact Google, so it cannot tell whether the sign-in still works.

## Codex location

- Without `--codex-bin`, ResetWatch finds the native Codex on `PATH`. On Windows it also finds npm installs under `%APPDATA%\npm\node_modules\@openai\codex` (x64 and ARM64).
- `.cmd`, `.bat` and `.ps1` shims are not supported, and ResetWatch never runs a shell. Pass the native executable with `--codex-bin` if needed.
- An explicit `--codex-bin` is saved as an absolute path. Setup saves the path it found.
- Provider and executable changes apply on the running watcher's next cycle.

## Running as a Linux service (example)

A user systemd service keeps the watcher running in your desktop session. Connect Gmail first with `resetwatch setup`; the service reuses that sign-in from Secret Service, which needs your session's keyring to be unlocked. Use your real binary path:

```ini
# ~/.config/systemd/user/resetwatch.service
[Unit]
Description=ResetWatch quota reset notifications

[Service]
Type=simple
ExecStart=/absolute/path/to/resetwatch watch
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

A user service normally needs an active session. Add `RESETWATCH_HOME` if you use a custom state folder. The computer needs its own Codex or Claude Code sign-in. Headless servers without a credential store are not supported in this version.

## Upgrading from older versions

- **From Resend (state version 1):** stop every running watcher, then run `resetwatch setup` (or `resetwatch gmail connect`). Connecting Gmail upgrades the state in one transaction: the reminder table is renamed, pending, retrying and interrupted reminders are re-created for your Gmail account with their reset times, attempts and retry deadlines, and the originals are cancelled as "moved to Gmail during upgrade". Sent, failed and exhausted reminders stay as history, and nothing is rebuilt from saved readings. The upgrade is refused while an older watcher holds a delivery lease, and the renamed table stops an older watcher that is still running from claiming anything afterwards. Until you connect Gmail, `status` and `history` work and `watch` asks you to upgrade. Old `.env` files are left untouched and no longer read.
- Older versions replaced Claude Code's status line. Setup restores your original entry and leave other settings alone; if you edited it since, it is left as is with a warning. `resetwatch claude-statusline` stays as a silent no-op so an old status line shows nothing instead of an error.
- Older frozen retries keep their original text: local time if saved, otherwise UTC.
- A confirmed Claude retry frozen by an older build is cancelled rather than rewritten under its old key.

## For the project owner: the Google client

Ordinary users never create Google Cloud projects. The owner registers one Desktop OAuth client and ships it with ResetWatch:

1. In Google Cloud, enable the Gmail API, configure the OAuth consent screen with the scopes `gmail.send`, `openid` and `email`, and create an OAuth client of type **Desktop app**.
2. Download its JSON (Google's "installed" format) and replace `internal/gmailauth/google-client.json` with it. Google treats desktop client secrets as public; PKCE protects the sign-in.
3. Build, run `resetwatch setup`, sign in, send the test email, then restart the watcher and check that it reuses the saved sign-in.

Before a public launch: `gmail.send` is a sensitive scope, so the app needs Google's verification, a public homepage, a privacy policy and a verified domain. An external app left in **Testing** issues refresh tokens that expire after seven days, which would pause delivery every week. See Google's [verification requirements](https://developers.google.com/identity/protocols/oauth2/production-readiness/sensitive-scope-verification) and [token expiration](https://developers.google.com/identity/protocols/oauth2#expiration).

## Development notes

- SQLite runs through a pure-Go driver, so normal builds need no C compiler. `go test -race` needs a C toolchain.
- The install website in `site/dist/` loads no analytics or third-party scripts. Its fonts and licenses are in `site/dist/fonts/`.
