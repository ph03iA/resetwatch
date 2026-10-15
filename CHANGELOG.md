# Changelog

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
- Added initial Windows x64, Linux x64, and macOS Apple silicon archive packaging, checksums, tests, and CI checks.

Local validation does not establish live email or inbox delivery. Binaries are unsigned. Only Codex is supported; the watcher requires an awake, online host.
