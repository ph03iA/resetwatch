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

## Whole-page design pass

- Applied the same warm graphite, copper and self-hosted typography across the header, hero, connected process rail, installation workbook, illustrative email excerpts, notes ledger and footer. The rail becomes vertical on phones, and setup instructions stack above their commands.
- Preserved all existing JavaScript DOM hooks and accessible tab/disclosure contracts. The only JavaScript change replaces the example sender with `onboarding@resend.dev`. Recipient instructions clearly require the operator's Resend account email, with verified-domain requirements for other recipients, checked against Resend's official documentation on 15 October 2026.
- All three platform tabs and both preview phases passed overflow checks at widths 320, 390, 768, 960, 1024, 1280, 1366 and 1440. The collapsed hero still fits a 1366×768 viewport.
- Enter/Space preview toggling, arrow/Home/End OS navigation, exact copied watcher commands on every platform, clipboard-denial fallback, reduced-motion settings and in-page anchor targets passed. Copy controls have a minimum 32px target height. The skip link appears on focus.
- The browser reported no page errors, failed requests or third-party resource loads. Windows, macOS and Linux download links were verified locally. Source packaging and archive checksums were refreshed after the design and documentation changes.
- This pass changes no Go implementation. No real email was sent and no inference request was made. The page remains local and unpublished.

## Reset preview layout fix

- Reproduced desktop hero-copy movement of 87.5–95.6875px when the email preview opened. The growing right column increased the shared grid row height while `align-items: center` recentered the left column.
- Changed the hero grid to `align-items: start`. No JavaScript function, signature or complexity changed; the preview stays in normal document flow.
- Opening and closing the preview now produces 0px document-position movement for the hero copy and headline at widths 320, 390, 768, 960, 1024, 1280, 1366 and 1440. The disclosure still opens, exposes its expanded state and creates no horizontal overflow. Position checks include scroll offsets so browser scrolling is not mistaken for layout movement.

## Immediate Resend delivery test

- Added `test-email --email ADDRESS [--from SENDER]`. It reads `RESEND_API_KEY`, attempts one immediate labeled test send, and uses `ResetWatch <onboarding@resend.dev>` by default. This sender requires the recipient to be the operator's Resend account email. Other recipients need an explicitly supplied sender on a verified domain.
- The command neither connects to Codex nor opens the state database. It changes no watcher configuration and creates no quota alert or history entry. Explicit repeat invocations use new random message keys and send another test; no application-level retry loop runs.
- Reset-email contents remain unchanged. Test contents state that no quota was checked and do not assert a reset or available allowance. Successful output says only that Resend accepted the email; inbox receipt remains a separate check.
- `go test -race ./...`, `go vet ./...` and the project complexity ceiling passed. Initial normal-sandbox fake HTTP tests could not reach their local servers; the same suite passed with local-loopback access. Tests use fake credentials, local HTTP servers or captured senders and never send real email.
- Rebuilt the local Windows executable and all platform archives, refreshed source/checksums, and checked command help plus missing-key rejection without any live Resend request. macOS/Linux binaries remain cross-compiled, unsigned and unexecuted on those platforms.

## Review fixes — 16 October 2026

- Reproduced four defects with isolated regression probes before implementation: stale recipient/sender settings during quota refresh, permanent handling of Resend's temporary concurrent-idempotency conflict, saved relative/PATH executable names, and a ninth claim after an interrupted eighth delivery attempt.
- Added permanent tests for settings changes during quota reads and between deliveries, failure to read settings, both temporary and permanent conflicts, malformed/oversized/unreadable provider bodies, bounded response reads, redaction and unchanged Retry-After handling.
- CLI regressions connect to a fake app-server using relative and PATH-resolved executable names, change the working directory/PATH, then refresh successfully through the saved absolute path. Existing saved relative paths require reconnecting once.
- A gated fake connection reproduced a related stale configuration overwrite: a notification update completed during the connection check was restored to its old value afterward. The connection now reloads settings before saving the executable; the regression preserves the newer recipient and sender. This is not a general atomic merge guarantee for simultaneous configuration writers.
- Store and watcher regressions keep expired eighth attempts at eight, terminalize exhausted pending/retry alerts, allow the next eligible alert through and preserve an active eighth lease.
- Fresh `go test -count=1 -race ./...` passed across all five packages using Go 1.27.2. `go vet ./...` passed, formatting was clean and `gocyclo -over 10 cmd internal` reported no violations.
- CI actions are pinned to commit hashes verified against the official action repositories. Remote CI has not been run for these local changes.
- Validation uses fake credentials, temporary state, local fake app-server processes and captured HTTP transports. No real email, account quota request or inference request was made during these fixes. Platform execution and signing limitations above still apply.
- Rebuilt the local Windows executable and all three platform downloads. All four archive checksums matched; the Windows archive contains the same binary as `bin/resetwatch.exe`. PE x64, ELF x64 and Mach-O ARM64 headers matched their target platforms. All 47 source-archive entries matched current files, including the five new regression files.
- Archive inspection reproduced a packaging defect: Linux/macOS binaries inherited non-executable mode 0666 from Windows. The release filter now writes binary mode 0755 and document mode 0644; both rebuilt tar archives passed those checks.
- Windows Application Control blocked the new packaged executable during the version/help smoke test. No policy or trust settings were changed and no alternative execution location was used to work around it. Source race tests passed normally, but launching this packaged build remains unverified on this Windows host. macOS/Linux builds were not executed.

## Helper extraction — 16 October 2026

- Split the existing connection function into argument parsing and quota/configuration persistence helpers. Split the delivery function at the existing cancellation/send boundary. Moved archive format selection/writing out of the compile-and-copy function. Commands, error messages, action order, exported signatures, retry payloads and archive formats remain unchanged.
- Fresh `go test -count=1 -race ./...` passed across all five packages before and after extraction. Existing relative/PATH persistence and concurrent-notification regressions remain green.
- Added two delivery integration tests and ran them successfully before production changes: an expired claim fails before sending, and lease loss during either an accepted or rejected send preserves the newer claim and returns the completion error before logging an outcome. Both remain green after extraction.
- `go vet ./...`, formatting and `gocyclo -over 10 cmd internal` passed. Touched production functions now have CC 5 or below; see complexity.md for the complete before/after table.
- Inspected existing archive metadata before extraction. The extracted writer passed a small-fixture check on all three target formats: identical member ordering, names and content, retained ZIP compression, binary mode 0755 and document mode 0644 in tar archives.
- Checks use temporary state and fake providers/transports only. No real email, account request or inference request was made. All changes remain local.
- Rebuilt all platform downloads and the source archive, refreshed checksums and updated `bin/resetwatch.exe`. The current Windows build successfully ran `version` and `help` from that normal path; the earlier Application Control block did not recur, and no policy or trust settings were changed.
- Independently verified all four archive checksums, source entries against current files, Windows archive/local executable equality, target binary headers and Unix archive permissions. macOS/Linux binaries remain cross-compiled and unexecuted on those platforms.

## Local key file — 16 October 2026

- Both `watch` and `test-email` now use a shared key loader. A nonempty process key wins without opening the file; otherwise only `.env` in the current working directory is read. No parent, executable or state directory search is performed.
- File loading accepts a single `RESEND_API_KEY` assignment, whitespace, matching quotes, comments, UTF-8 BOM and CRLF. Reads are bounded to 64 KiB plus one overflow-detection byte. Duplicate/malformed target values, read failures and non-regular files produce redacted errors. No variable is exported, interpolated or evaluated.
- A fake CLI send failed before implementation because only the environment was supported. Baseline and final fresh `go test -count=1 -race ./...` pass across all five packages. New tests cover key precedence, syntax, missing/invalid/bounded input, redaction, unchanged process variables, fake Codex watcher use and test-email without a state database. Existing missing-key tests now run in temporary directories so no real `.env` is accessed.
- `go vet ./...`, formatting and `gocyclo -over 10 cmd internal` pass. No new dependency was added.
- Real environment files are ignored by Git, including mixed-case names. Binary archives use an explicit five-file allowlist and source packaging excludes actual environment files; both include the blank `.env.example`. Two stdlib Python regression tests pass across Windows/Linux/macOS fixtures and root/nested source files, covering contents, compression and Unix permissions. CI includes those tests; remote CI has not run here.
- The installation page and documentation now explain one-time `.env` setup. JavaScript syntax and isolated DOM/clipboard checks passed for all operating systems, with exact no-JavaScript Windows command parity. A browser visual pass was not performed for these copy changes.
- The existing project `.env` was preserved without reading its contents. All validation used synthetic credentials and temporary fixtures. No real email, account quota or inference request was made. Changes remain local.
- Rebuilt all three platform downloads, refreshed source/checksums and updated the local Windows executable and blank template. Normal-path Windows `version` and `help` passed without reading the key file. Final archive checks confirm target headers, template/docs bytes, local/archived Windows binary equality, source byte equality, all four checksums and absence of real environment files. macOS/Linux binaries remain cross-compiled and unexecuted on those platforms.

## Remove page demonstrations — 16 October 2026

- Removed the simulated reset readout, envelope/letter artwork, preview control and sample email, the separate email-excerpt section and installation demo block. Deleted their JavaScript bindings and unused styles. Kept installation inputs/template filenames because operators need to replace those values during setup.
- Reflowed the hero into a text-led layout, aligned the release information after removing the demo column and renumbered the notes section. Expected versus confirmed reset behavior remains factual operating-note text rather than a mock email.
- Real browser checks passed all three OS tabs at widths 320, 390, 768, 960, 1024, 1280, 1366 and 1440. No horizontal page overflow or remaining demonstration elements were observed. Visible sample/illustration/preview labels are absent.
- Verified exact copy contents for all three command blocks on every OS using an isolated clipboard stub, clipboard-denial selection fallback, Arrow/Home/End keyboard navigation, valid anchor targets and a zero-duration reduced-motion transition. JavaScript-disabled Windows instructions matched the active Windows tab and had no overflow at 390px.
- Windows/macOS/Linux download responses, source archive and checksums returned HTTP 200. JavaScript syntax passed; the browser reported no console errors. Inspected desktop and mobile screenshots.
- No Go code changed, so the source race suite was not repeated for this page-only deletion. No real clipboard contents, keys or account data were read, and no email/inference request was made. The website remains local and unpublished.
- Refreshed packaged documentation, the source archive and all four checksums. All 52 source entries matched current files and contained no real environment files. The Windows, Linux and macOS binary hashes stayed unchanged; their archive documentation matches the current README and templates.

## Restore decorative envelope — 16 October 2026

- Recovered the original envelope SVG geometry from the repository and restored it beside the hero copy. The letter retains the brand mark; sample email text, countdown, statuses, email excerpts and preview/demo controls stay removed. The SVG is decorative and hidden from assistive technology.
- Restored a balanced two-column desktop hero and a stacked mobile layout. Actual browser checks passed at widths 320, 390, 768, 960, 1024, 1280, 1366 and 1440: the artwork stayed inside the viewport, the heading did not overlap it, and no horizontal page overflow occurred. All checks found zero removed demo controls and only the brand text in the SVG.
- Inspected desktop and mobile screenshots; the browser reported no console errors. No JavaScript, Go or release function changed, so their complexity and behavior are unchanged. Refreshed the local source archive and checksums; no publishing or provider calls occurred.
