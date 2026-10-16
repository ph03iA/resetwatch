# Complexity report

Measured with gocyclo v0.6.0. Ceiling: 10 for every Go function, including tests. The project began empty, so new production functions have no before score.

| Function | Before | After |
| --- | ---: | ---: |
| TestCodexReadHandshakeAndBuckets | 11 | 2 |
| assertSnapshotIdentityAndPrivacy | New | 5 |
| assertCodexQuotaBuckets | New | 6 |
| store.DB.Schedule | New | 9 |
| quota.ValidateSnapshot | New | 8 |
| quota.codexClient.response | New | 8 |
| quota.decodeLimit | New | 8 |
| notify.mailbox | New | 8 |
| CLI connect/status | New | 8 |

Extracted: assertSnapshotIdentityAndPrivacy and assertCodexQuotaBuckets. These separate identity/privacy assertions from quota-window assertions; no production behavior changed in the refactor.

Behavior verified: provider tests passed before and after extraction. The complete suite subsequently passed with the race detector using Go 1.27.2. `gocyclo -over 10 cmd internal` reports no violations. Website JavaScript named functions were counted manually: maximum CC 5 after the envelope preview update; nested callbacks maximum CC 2. Python release functions have maximum CC 6 by manual decision counting, including conditional expressions. Developer preview server handler CC 5 (guard, conditional expression, catch, nullish fallback).

Run the same Go check with:

```sh
go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 10 cmd internal
```

## Installation page refresh

Manual decision count for the removed visual-demo functions:

| Function | Before | After |
| --- | ---: | --- |
| startDemoCountdown | 2 | Removed |
| renderCountdown | 1 | Removed |
| formatCountdown | 1 | Removed |
| serve (preview handler) | 5 | 5 |
| source_files (archive assets) | 4 | 4 |

Extracted: none. Removed the simulated ticking display and its timer; at that pass, platform selection, keyboard navigation, copying and email-preview functions retained their existing signatures and CC of 1–2. Font files and license notices are included in the source archive.

Behavior verified: platform selection, actual copied command text, denied-clipboard fallback, arrow-key tab navigation and email preview passed browser checks before/after the presentation changes. No Go functions were modified.

## Envelope preview redesign

Manual decision count, ranked by CC. The only changed JavaScript function is `toggleEmailPreview`; its four conditional expressions select the button label, illustration phase, example minutes and saved-reminder label. The existing signature is preserved.

| Function | Before | After |
| --- | ---: | ---: |
| toggleEmailPreview | 2 | 5 |
| handleTabKey | 2 | 2 |
| selectCommandText | 2 | 2 |
| copyCommand | 2 | 2 |
| updateInstallCommands | 1 | 1 |
| updateDownload | 1 | 1 |
| selectPlatform | 1 | 1 |
| showCopySuccess | 1 | 1 |

Extracted: none. CSS handles the two explanatory captions and reduced-motion behavior. No polling, countdown timer, frontend dependency or backend code was added.

Behavior verified: existing preview open/close and Linux installation selection passed before editing. After editing, all three platform tabs and both illustration phases passed overflow checks at 320, 390, 768, 1280, 1366 and 1440 pixels. Enter/Space toggling, arrow/Home/End navigation, exact copied text, denied-clipboard selection fallback and download responses passed. Reduced motion disables both the letter transition and email reveal animation. JavaScript syntax validation passed.

## Whole-page redesign

The only production function touched in this pass changes its example sender constant. Manual count: no branching; CC remains 1. All other JavaScript functions retain their previous scores, with maximum CC 5.

| Function | Before | After |
| --- | ---: | ---: |
| updateInstallCommands | 1 | 1 |

Extracted: none. The process diagram, workbook, email examples, notes ledger and footer use semantic HTML and responsive CSS. No additional JavaScript, framework or backend logic was introduced.

Behavior verified: preview open/close and Linux archive selection passed before editing. The redesigned page passed all-platform, both-preview-phase overflow checks at 320, 390, 768, 960, 1024, 1280, 1366 and 1440 pixels. Keyboard interaction, exact copying with the no-domain sender on each OS, denied-clipboard fallback, reduced motion, anchor targets and download responses were checked. JavaScript syntax validation passed.

## Immediate test email

Measured with gocyclo before and after. The existing email validator (CC 6) was the touched hotspot. The first version of the new CLI test measured 10; its field and key assertions were extracted before delivery.

| Function | Before | After |
| --- | ---: | ---: |
| validateMessage | 6 | 4 |
| messageContent | 3 | 4 |
| application.run | 3 | 4 |
| application.help | 2 | 2 |
| TestEmailSendsExplicitTestWithoutState | 10 | 5 |
| application.testEmail | New | 5 |
| validateResetMetadata | New | 5 |
| application.testEmailFlags | New | 4 |
| assertTestEmailFields | New | 4 |
| assertDistinctTestEmailKeys | New | 3 |
| emailHTML | New | 1 |
| testMessageContent | New | 1 |

Extracted: validateResetMetadata, emailHTML, assertTestEmailFields and assertDistinctTestEmailKeys. Existing reset email text and HTML output remain unchanged. Test emails have their own explicit label and never assert a reset or confirmed quota.

Behavior verified: CLI and notify baseline tests passed before changes. After changes, `go test -race ./...`, `go vet ./...` and `gocyclo -over 10 cmd internal` passed. New tests cover a single immediate send, distinct per-invocation keys, sender defaults/overrides, invalid addresses, missing API keys, no state database access and no automatic retry. Fake HTTP tests verify the labeled test email and preserve reset-email checks. No real emails were sent.

## Review fixes — 2026-10-16

Measured with gocyclo before and after, ranked by final CC. Existing nontrivial regression probes failed before implementation; the permanent regression suite now passes. Exported signatures remain unchanged.

| Function | Before | After |
| --- | ---: | ---: |
| application.connect | 8 | 10 |
| Engine.deliver | 5 | 6 |
| Engine.Observe | 4 | 5 |
| Engine.deliverDue | 5 | 5 |
| quota.ResolveBinary | New | 5 |
| notify.statusError | 3 | 4 |
| notify.concurrentIdempotentRequest | New | 4 |
| Engine.Step | 3 | 3 |
| DB.Claim | 1 | 2 |

Extracted: none. New helpers are `ResolveBinary` for resolving an explicitly configured native executable and `concurrentIdempotentRequest` for bounded parsing of the temporary Resend conflict. New regression tests and their helpers have CC 9 or lower. Every Go function, including existing tests, remains at CC 10 or lower.

Behavior verified: fresh `go test -count=1 -race ./...`, `go vet ./...`, formatting and `gocyclo -over 10 cmd internal` checks passed. Regressions cover settings changes during quota reads and between deliveries, unreadable settings, bounded conflict parsing/redaction, relative/PATH executable persistence, preservation of notification settings changed during a connection check, exhausted lease recovery and preservation of a live eighth lease. Tests use temporary databases, fake executables and captured email transports; no real account access or email delivery occurs.

Release packaging was also checked manually. `build` has five decisions (one conditional expression, three loops and one if), so CC is 6. The archive-permissions helper adds one conditional expression, so CC is 2.

| Function | Before | After |
| --- | ---: | ---: |
| release.build | 6 | 6 |
| release.archive_permissions | New | 2 |

Extracted: none. The new archive filter sets Unix binary mode to 0755 and document mode to 0644 instead of inheriting Windows file modes. Behavior verified by rebuilding and inspecting both tar archives; their contents, platform binary headers, source archive and checksums are checked independently.

## Helper extraction — 2026-10-16

The scoped hotspots before this refactor were `application.connect` (CC 10), `Engine.deliver` (CC 6) and `release.build` (CC 6). Go scores use gocyclo. Python scores count decisions manually: `build` retains its binary-name conditional expression and document-copy loop; `archive_target` owns one format condition and two loops.

| Function | Before | After |
| --- | ---: | ---: |
| application.connectFlags | New | 5 |
| application.connect | 10 | 4 |
| application.connectCodex | New | 4 |
| Engine.sendAlert | New | 4 |
| release.archive_target | New | 4 |
| Engine.deliver | 6 | 3 |
| release.build | 6 | 3 |

Extracted: `connectFlags`, `connectCodex`, `sendAlert` and `archive_target`. The connection retains validation/error order, native executable resolution and the post-observation settings reload. Delivery retains its settings/cancellation gate, immutable message, retry and lease fences, persistence-before-log order and existing method signatures. Packaging retains filenames, compression, sorted members and Unix permissions.

Behavior verified: fresh `go test -count=1 -race ./...` passed before and after extraction. New integration tests also passed before extraction and still pass afterward: stale claims never send, and lease loss during either sender success or failure preserves the newer claim and returns the persistence error without logging an outcome. `go vet ./...`, formatting and the all-functions complexity ceiling passed. A temporary-fixture archive self-check covered Windows, Linux and macOS member names/order, content, ZIP compression and Unix modes. No live account or email calls were made.

## Local key file — 2026-10-16

Measured with gocyclo before and after. The existing touched hotspot was `application.watch` (CC 6); `configure` changes only its setup message. New parser responsibilities are separate from command orchestration.

| Function | Before | After |
| --- | ---: | ---: |
| application.configure | 7 | 7 |
| application.watch | 6 | 6 |
| parseResendEnv | New | 6 |
| application.testEmail | 5 | 5 |
| openDotEnv | New | 5 |
| dotenvValue | New | 5 |
| application.resendAPIKey | New | 4 |
| quotedDotenvValue | New | 4 |
| loadResendEnv | New | 3 |
| readResendEnv | New | 3 |
| application.help | 2 | 2 |

Extracted: shared key lookup into `resendAPIKey`. Added named helpers for opening, bounded reading and parsing the key file. Existing test fixtures now use isolated working directories when checking absent keys, so they cannot read a developer's real `.env`. All new Go test functions have CC 6 or lower; all Go functions still satisfy the ceiling of 10.

Python/JavaScript decision counts are manual, including conditional expressions and loops:

| Function | Before | After |
| --- | ---: | ---: |
| release.archive_target | 4 | 4 |
| release.source_files | 4 | 4 |
| release.include_source | New | 4 |
| release.build | 3 | 3 |
| release.package_source | 3 | 3 |
| release.platform_files | New | 3 |
| updateInstallCommands | 1 | 1 |

`platform_files` gives binary archives an explicit file allowlist. `include_source` rejects real environment files case-insensitively before source packaging. The largest new release-test function has CC 7. No dependency was added.

Behavior verified: baseline and final `go test -count=1 -race ./...` passed, as did vet, formatting and the all-functions complexity ceiling. The fake email regression failed before the new loader and passes afterward. Tests cover process precedence, supported file syntax, redaction, size/read failures, ignored variables, fake watcher use and no test-email state database. Two Python release tests cover all three archive targets and nested secret-file exclusions. JavaScript syntax and isolated all-platform copy/no-JavaScript parity checks passed; no visual redesign was made.

## Remove page demonstrations — 2026-10-16

Manual JavaScript decision counts before and after. The preview function was the only removed function; it had four conditional expressions. The surviving installation function changes only by deleting its demo-command update.

| Function | Before | After |
| --- | ---: | --- |
| toggleEmailPreview | 5 | Removed |
| updateInstallCommands | 1 | 1 |

Extracted: none. Deleted the preview listener and unused countdown/envelope/email/demo CSS, including breakpoint rules and keyframes. The remaining hero is a single text-led block. No Go function, exported API or release function changed.

Behavior verified: the baseline page loaded without overflow and its installation commands were recorded before removal. Afterward, all three OS tabs passed at eight viewport widths (320–1440px), without page overflow or remaining demonstration elements. Actual browser checks verified all command-copy controls, clipboard-denial fallback, Arrow/Home/End tab navigation, anchor targets, reduced motion and the JavaScript-disabled Windows instructions. Every platform download returned HTTP 200. JavaScript syntax passed and the browser reported no console errors. Desktop/mobile screenshots were inspected. Only local files and an isolated clipboard stub were used; no account or email calls occurred.

The decorative envelope was subsequently restored from the original SVG geometry, without its sample email text or interactive demonstration. Only HTML/CSS and documentation changed; no function's complexity changed. Eight viewport checks and desktop/mobile screenshot inspection verified the restored layout.
