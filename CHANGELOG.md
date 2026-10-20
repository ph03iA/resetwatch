# Changelog

## Unreleased — 2026-10-16

- Redesigned the website as a plain printed manual that shows real output from the email and status code, in light and dark, and brought it up to date: Codex and Claude, "is back" versus "should be back", the six commands, the sender rule and honest limits.
- Removed the `connect`, `notify`, `retry` and `demo` commands; `setup` does their job. The commands are now `setup`, `watch`, `status`, `history`, `test-email` and `version`. The README is shorter, with the detailed rules in `docs/details.md`.
- Claude reset emails now say **is back** when a fresh reading taken after the reset leaves out that window: Claude omits a window until it is used again. The email also names the 5-hour or weekly limit in that case.
- Claude usage now comes from Claude Code itself: the watcher starts Claude Code with no prompt and sends the experimental `get_usage` request behind `/usage` (the Claude Agent SDK approach), every cycle like Codex. Claude emails can say **is back** from a fresh reading, early resets are detected, and the account is identified. The status-line method is retired: setup and `connect claude` restore any status line an older version replaced, `--replace-statusline` is removed, and `claude-statusline` is a silent no-op for old installs. AGENTS.md records this owner-approved exception to documented-only interfaces.
- Find npm-installed Codex on Windows from 32-bit ResetWatch builds too, and keep Claude's `settings.json` indented when setup installs or restores the status line.

- Add guided `resetwatch setup`: choose Codex, Claude or both; configure email and a hidden local key; safely install and restore Claude status lines; optionally send a test. Provider selection applies live, disables unsent reminders and preserves delivery safety. Add combined status output and Windows npm executable discovery.

- New reset emails show the date and time in the time zone of the computer running the watcher, alongside exact UTC. Existing frozen retries keep their original display text.

- Reset emails again warn when another limit is still used up, even when the reset cannot be confirmed (Codex permission false or unknown, or Claude last-known readings).

- Whole-repository audit fixes: preserve Codex backend usage permission; keep Claude resets expected-only; atomically save readings, notification settings and reminders; recover expired claims and cancelled destinations without reviving completed events; freeze the displayed reset time once per message so retries stay identical; remove unused session tracking and the standalone reminder-move entry point; reload connections on the next watcher cycle; correct Claude support copy and keep the keyboard skip link above the header.

- Claude support: `resetwatch claude-statusline` reads Claude Code's documented status line rate limits (5-hour and weekly) and schedules reset reminders. The watcher skips Codex polling until Codex is connected and uses saved readings for Claude alerts.
- Claude callbacks save readings and reminders together, preserve independently omitted windows and never claim confirmed allowance. `notify` schedules saved observations on first setup and moves outstanding reminders when addresses change; `status` shows Claude and accepts `--provider`.
- Friendlier reset email: subjects like "Your Codex 5-hour limit is back. Get back to work", the window named by length (5-hour, weekly), IST reset date and time with exact UTC kept, and a styled HTML card. Unconfirmed resets still say "should be back".
- Name the npm `codex.exe` location in the Windows not-found error and README.
- Set the version once: release builds stamp `main.version`, which is also reported to Codex.
- Drop prebuilt downloads: the site and README now show clone-and-build with Go, archives are no longer committed, and the release packaging scripts and their CI step are removed.
- Email immediately when Codex resets an exhausted window early, instead of silently cancelling the alert. Windows that never reach 100% still send nothing.
- Remove countdown/email previews, mock emails and demo controls from the installation page, retaining the original decorative envelope, real setup instructions and reset-evidence notes.
- Load the Resend key once from a local `.env` for both watcher and test emails, with process-environment precedence, bounded parsing and redacted errors. Include a blank `.env.example` and exclude real environment files from Git and release archives.
- Extract connection parsing/persistence, alert sending and archive writing into small named helpers without changing behavior; add delivery lease/error-ordering regressions.
- Reload notification settings after quota reads and before each delivery, so recovered alerts with a former recipient or sender are cancelled.
- Keep notification settings changed during a connection check when saving the resolved Codex executable.
- Retry Resend's temporary `409 concurrent_idempotent_requests` response while keeping payload conflicts and malformed responses permanent.
- Resolve an explicitly configured Codex executable to an absolute native path when connecting, preserving it across directory and PATH changes.
- Enforce the eight-attempt limit when recovering expired delivery leases and let other eligible alerts proceed.
- Set executable permissions in Linux/macOS archives built on Windows.
- Pin CI checkout and Go setup actions to verified commit hashes.
- Add permanent regressions for all four fixes and refresh the local executable, platform archives, source archive and checksums.

## 0.1.0 — 2026-10-15 — release candidate

- Added a Go CLI for reading Codex usage windows through the local app-server.
- Added saved status, optional fresh/JSON output, and a safe sample-data demo.
- Added SQLite quota snapshots, pending alerts, delivery leases, and history.
- Added Resend email delivery with stable keys, frozen retry payloads, exponential backoff, bounded retries, and manual retry of eligible failed alerts.
- Distinguished expected reset reminders from fresh confirmation that all reported windows have allowance.
- Cancelled obsolete alerts when settings or detected account identity change, and discarded reset alerts more than 24 hours old.
- Added a responsive dark installation website with platform-specific commands, working copy controls, and labeled simulated previews.
- Refined the installation page with self-hosted Manrope and Instrument Serif, readable setup instructions, static product examples, and fewer decorative sections.
- Replaced the terminal and stacked email card with an open reset readout, a custom graphite envelope illustration, and an accessible expected-reset preview with reduced-motion support.
- Redesigned the full page with a connected process diagram, installation workbook, expected/confirmed email excerpts, operating-notes ledger, and large wordmark footer. Installation now starts with Resend's no-domain test sender for the operator's own account email.
- Fixed hero text moving when the reset email preview expands by aligning both hero columns at the top.
- Added `test-email --email ADDRESS [--from SENDER]` for one immediate, clearly labeled Resend delivery test, without Codex access or local alert-state changes.
- Added initial Windows x64, Linux x64, and macOS Apple silicon archive packaging, checksums, tests, and CI checks.

Local validation does not establish live email or inbox delivery. Binaries are unsigned. Only Codex is supported; the watcher requires an awake, online host.
