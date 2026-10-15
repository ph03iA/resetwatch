# Release validation

Validated on Windows x64, 15 October 2026. Release version: 0.1.0.

- Go 1.27.2 portable compiler, downloaded from go.dev and SHA-256 verified against the official release listing. The user's system Go installation was not changed.
- Pinned modernc.org/sqlite v1.60.1. `go mod verify` passed.
- `go test -race -cover ./...` passed across CLI, email, quota protocol, SQLite persistence, and scheduler packages.
- Statement coverage: CLI 59.5%, email 96.7%, quota 89.5%, store 77.8%, scheduler 74.5%.
- `go vet ./...` passed.
- `govulncheck` v1.1.4 reported “No vulnerabilities found.”
- The packaged Windows binary successfully connected to the installed Codex app-server and read actual account quota data using an isolated state directory. No inference requests or emails were made.
- All Go functions, including tests, are at complexity 10 or below. See complexity.md.
- Website browser checks passed at widths 320, 390, 768, 1280 and 1440. No page overflow across installation tabs. Copy success and fallback were exercised with an isolated clipboard stub; no operating-system clipboard data was read. Keyboard tabs, email preview, reduced motion, and zero third-party resource requests were verified.

Tests use temporary state directories, simulated providers, fake email credentials and local/in-memory HTTP transports. No real email was sent. Live sender verification and inbox delivery require the operator's Resend account. macOS/Linux binaries are cross-compiled; execution on those operating systems and binary signing/notarization are not validated here. CI configuration is supplied but has not run on a remote repository.

## Local website redesign

- Replaced the terminal window and overlapping email card with an open reset readout and a custom SVG graphite envelope. Self-hosted Manrope and Instrument Serif retain their bundled font licenses.
- The preview is explicitly illustrative: the button changes 42 minutes to 00, lifts the letter and shows an expected-reset sample email. It does not run a countdown or assert fresh confirmed allowance. Reset text is available to assistive technology, and the button exposes its expanded state.
- Verified every OS tab and both preview phases at widths 320, 390, 768, 1280, 1366 and 1440 without horizontal page overflow. The complete collapsed hero fits a 1366×768 laptop viewport.
- Exercised copy success with exact command text, denied-clipboard fallback, arrow/Home/End tab navigation and Enter/Space preview open/close. Only an isolated clipboard stub was used.
- Reduced-motion emulation reported a 0-second letter transition and no email reveal animation. JavaScript syntax validation passed.
- All three platform-download requests returned HTTP 200. The browser reported no page errors, failed requests or external resource requests. Font files loaded from the local preview.
- The existing muted-text palette is retained; its earlier six-style contrast spot check measured 4.70–7.12 against the actual backgrounds.
- The application backend was unchanged. The website remains local and unpublished.
