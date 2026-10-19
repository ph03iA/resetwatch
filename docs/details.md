# ResetWatch details

The [README](../README.md) covers install, setup and everyday use. This page has the exact rules.

## Alerts

- An alert is armed when a reported window reaches 100% and email settings exist. Routine checks never send email.
- If a provider shows a new window (a later reset time) before the recorded reset, the email is sent at once instead of waiting.
- At the reset time the watcher reads usage again. An **is back** email needs a reading taken after the reset and at most two minutes old, with the alert's window below 100% and no other window still at 100%.
  - **Codex** must also report `ordinaryUsageAllowed: true`. Older Codex versions without that field get **should be back**.
  - **Claude** has no such flag; fresh windows below 100% are the evidence. After a reset Claude leaves the window out until you use Claude again, so a fresh reading without it counts as unused.
- If another window is still at 100%, the email adds a warning and says **should be back**.
- A Claude reading may omit a window; that means unknown, so the last known value and its reminder are kept until it resets. Claude reset times are rounded to the minute because Claude recomputes them with sub-second drift.
- The email shows the reset time in the time zone of the computer running the watcher, plus the exact UTC time. Setup prints that zone.

## Delivery and retries

- Each alert has one stable idempotency key and a message frozen on its first send attempt, so retries are identical and Resend never sends it twice. This also means an email first tried while offline keeps its **should be back** wording.
- Network failures, HTTP 408/429, server errors and Resend's temporary `409 concurrent_idempotent_requests` are retried with exponential backoff and bounded `Retry-After`. Other failures stop automatic retries.
- At most eight attempts, all within 23 hours of the first (Resend deduplicates for 24 hours). An interrupted final attempt becomes failed when its lease expires.
- Alerts more than 24 hours past their reset are cancelled, so a computer waking after a week does not send old reminders.
- Pending alerts and delivery leases survive restarts. A changed account, recipient or sender cancels unsent alerts for the old one.
- Changing email settings moves pending reminders to the new address. If a delivery is in progress, setup asks you to rerun it after the delivery finishes or its two-minute lease expires.
- Deselecting a provider in setup cancels its unsent reminders. Re-enabling it does not replay old ones; a new reading can arm a future reset.

## The `.env` key file

- Only `RESEND_API_KEY` is read, and only from `.env` in the current folder. A nonempty `RESEND_API_KEY` in the environment wins and the file is not read.
- Plain values and matching single or double quotes work, as do blank lines, comments, a UTF-8 BOM and CRLF. An unquoted `#` starts a comment.
- No `export`, escapes, variable expansion or multiline values. Duplicate keys, malformed values, non-regular files and files over 64 KiB are rejected without showing their contents.
- File values are not exported to the environment or to Codex. Restart a running watcher after changing the key.
- The file holds a plaintext secret. Restrict it to your user (`chmod 600 .env` on Unix) and never commit it.

## Codex location

- Without `--codex-bin`, ResetWatch finds the native Codex on `PATH`. On Windows it also finds npm installs under `%APPDATA%\npm\node_modules\@openai\codex` (x64 and ARM64).
- `.cmd`, `.bat` and `.ps1` shims are not supported, and ResetWatch never runs a shell. Pass the native executable with `--codex-bin` if needed.
- An explicit `--codex-bin` is saved as an absolute path. Setup saves the path it found.
- Provider and executable changes apply on the running watcher's next cycle.

## Running as a Linux service (example)

A user systemd service keeps the watcher running. Put your key in `~/.config/resetwatch/resend.env` as `RESEND_API_KEY=...` with mode `0600`, and use your real binary path:

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

A user service normally needs an active session. Add `RESETWATCH_HOME` if you use a custom state folder. The host needs its own Codex or Claude Code sign-in.

## Upgrading from older versions

- Older versions replaced Claude Code's status line. Setup restores your original entry and leave other settings alone; if you edited it since, it is left as is with a warning. `resetwatch claude-statusline` stays as a silent no-op so an old status line shows nothing instead of an error.
- Older frozen retries keep their original text: local time if saved, otherwise UTC.
- A confirmed Claude retry frozen by an older build is cancelled rather than rewritten under its old key.
- If an older frozen retry with an unnamed sender fails with HTTP 422, rerun `setup --from 'ResetWatch <onboarding@resend.dev>'` instead of retrying it.

## Development notes

- SQLite runs through a pure-Go driver, so normal builds need no C compiler. `go test -race` needs a C toolchain.
- The install website in `site/dist/` loads no analytics or third-party scripts. Its fonts and licenses are in `site/dist/fonts/`.
