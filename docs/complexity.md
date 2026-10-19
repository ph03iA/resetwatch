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

## Whole-repository audit fixes — 2026-10-16

Measured all production functions in touched Go files against `a585649` with gocyclo. Ranked by the greater before/after score; new functions have no invented baseline. The all-functions ceiling, including tests, remains 10.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| main.(*application).statusSnapshot | 9 | 9 |
| store.(*DB).Schedule | 9 | 6 |
| main.(*application).recordClaude | 8 | Removed |
| notify.mailbox | 8 | 8 |
| quota.ValidateSnapshot | 8 | 8 |
| quota.decodeLimit | 8 | 8 |
| store.schedule | New | 8 |
| main.(*application).claudeStatusLine | 7 | 5 |
| main.(*application).configure | 7 | 5 |
| main.(*application).history | 7 | 7 |
| main.(*application).reschedule | 7 | Removed |
| store.(*DB).ConfigureNotifications | New | 7 |
| store.(*DB).NewReadings | 7 | 7 |
| store.(*DB).RecordSnapshot | New | 7 |
| store.mergeClaude | New | 7 |
| watch.(*Engine).cancelReason | 7 | 7 |
| watch.compose | 7 | 7 |
| watch.freshForAlert | 6 | 7 |
| main.(*application).retry | 6 | 6 |
| main.(*application).watch | 6 | 6 |
| quota.decodeSnapshot | 6 | 6 |
| quota.decodeWindow | 6 | 6 |
| quota.validateWindow | 6 | 6 |
| store.(*DB).MoveAlerts | 6 | 4 |
| watch.(*Engine).sendAlert | 4 | 6 |
| watch.windowDuration | 6 | 6 |
| main.(*application).connectFlags | 5 | 5 |
| main.(*application).status | 5 | 5 |
| notify.validateResetMetadata | 5 | 5 |
| notify.windowLabel | 5 | 5 |
| quota.accountIdentity | 5 | 5 |
| store.moveAlert | New | 5 |
| store.prepareDirectory | 5 | 5 |
| store.prepareFile | 5 | 5 |
| store.scheduleSaved | New | 5 |
| watch.(*Engine).Observe | 5 | 3 |
| watch.(*Engine).deliverDue | 5 | 5 |
| watch.(*Engine).loop | 5 | 5 |
| main.(*application).connect | 4 | 4 |
| main.(*application).connectCodex | 4 | 3 |
| main.(*application).render | 4 | 4 |
| main.(*application).renderHistory | 4 | 4 |
| notify.messageContent | 4 | 4 |
| notify.validateLabel | 4 | 4 |
| notify.validateMessage | 4 | 4 |
| quota.decodeLimits | 4 | 4 |
| quota.validPercent | 4 | 4 |
| store.(*DB).Finish | 4 | 4 |
| store.(*DB).History | 4 | 4 |
| store.(*DB).Retry | 4 | 4 |
| store.(*DB).SaveCodexBinary | New | 4 |
| store.Open | 4 | 4 |
| store.alertsToMove | 4 | 4 |
| store.cancelMissing | 4 | 4 |
| store.initialize | 4 | 4 |
| store.moveAlerts | New | 4 |
| store.reconcileNotifications | New | 4 |
| store.recordingSnapshot | New | 4 |
| store.scheduleWindow | 4 | 4 |
| watch.(*Engine).Run | 4 | 4 |
| watch.(*Engine).Step | 4 | 4 |
| watch.(*Engine).alertSnapshot | 4 | 4 |
| watch.confirmedUsageAllowed | New | 4 |
| watch.retryDecision | 4 | 4 |
| notify.ValidateAddresses | 3 | 3 |
| store.(*DB).Config | 3 | 1 |
| store.(*DB).SaveSnapshot | 3 | 1 |
| store.loadConfig | New | 3 |
| store.newerClaudeWindow | New | 3 |
| store.rejectActiveDelivery | New | 3 |
| store.saveSnapshot | New | 3 |
| watch.(*Engine).deliver | 3 | 3 |
| main.(*application).codexProvider | 2 | 2 |
| main.(*application).demo | 2 | 2 |
| main.(*application).refresh | 2 | 2 |
| main.claudeSummary | 2 | 2 |
| main.countdown | 2 | 2 |
| notify.RetryAfter | 2 | 2 |
| notify.Retryable | 2 | 2 |
| notify.limitName | 2 | 2 |
| notify.resetHTML | 2 | 2 |
| quota.validTime | 2 | 2 |
| store.(*DB).Claim | 2 | 2 |
| store.(*DB).SaveConfig | 2 | 1 |
| store.(*DB).Snapshot | 2 | 1 |
| store.loadSnapshot | New | 2 |
| store.releaseEarlyReset | 1 | 2 |
| store.saveConfig | New | 2 |
| store.scanAlert | 2 | 2 |
| watch.(*Engine).clock | 2 | 2 |
| watch.(*Engine).log | 2 | 2 |
| watch.(*Engine).recordFailure | 2 | 2 |
| main.(*application).renderWindow | 1 | 1 |
| main.writeJSON | 1 | 1 |
| notify.(*deliveryError).Error | 1 | 1 |
| notify.emailHTML | 1 | 1 |
| notify.providerName | 1 | 1 |
| notify.testMessageContent | 1 | 1 |
| store.(*DB).Close | 1 | 1 |
| store.alertID | 1 | 1 |
| store.alertKey | 1 | 1 |
| store.cancelReplaced | 1 | 1 |
| store.cancelSuperseded | 1 | 1 |

Extracted: transaction-compatible config/snapshot reads and writes, shared `schedule`, and individual `moveAlert` handling. Added atomic `RecordSnapshot`, `ConfigureNotifications`, and `SaveCodexBinary` operations with small named merge and reconciliation helpers. Removed the CLI `recordClaude` and `reschedule` orchestration. The existing exported `NewReadings` remains compatible but deprecated; ingestion no longer depends on it.

Behavior verified: baseline and combined race suites pass. Regression cases cover failed recording rollback/replay, concurrent partial callbacks, backend allowance denial, expected-only Claude reminders, due address round trips, live/expired claims, immutable payloads, terminal destinations, concurrent connection/settings updates and fixed-UTC request bytes.

## Review follow-ups — 2026-10-16

Local reset time is computed once in `compose` and frozen in `notify.Message.LocalResetAt`; the email falls back to UTC for messages frozen without it. Removed unused session tracking (`NewReadings`, the `readings` table, `session_id` parsing), the standalone `MoveAlerts` wrapper (its test now runs through `ConfigureNotifications`) and `Observe`'s unused settings argument.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| watch.compose | 7 | 7 |
| quota.DecodeClaudeStatusLine | 8 | 7 |
| main.(*application).claudeStatusLine | 5 | 5 |
| notify.messageContent | 4 | 4 |
| store.moveAlerts | 4 | 4 |
| watch.(*Engine).Step | 4 | 3 |
| watch.(*Engine).Observe | 3 | 3 |
| main.(*application).connectCodex | 3 | 2 |
| main.(*application).refresh | 2 | 2 |
| notify.localResetAt | New | 2 |
| store.(*DB).NewReadings | 7 | Removed |
| store.(*DB).MoveAlerts | 4 | Removed |
| quota.validSessionID | 3 | Removed |

Behavior verified: full suite and `go test -race` (64-bit build) pass; `gocyclo -over 10 cmd internal` passes. New tests cover the frozen local time surviving a time-zone change, `compose` using the user's zone, and Claude data without `session_id`.

## Still-exhausted warning — 2026-10-16

`compose` adds the "another limit is still used up" warning from any same-account reading taken after the reset, before the confirmation check, so unconfirmed Codex and Claude emails keep it. Windows whose reset already passed no longer count.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| watch.compose | 7 | 7 |
| watch.freshForAlert | 7 | 3 |
| watch.observedAfterReset | New | 5 |
| watch.windowAvailable | New | 2 |

Behavior verified: new `TestStillExhaustedWindowWarnsWithoutConfirmation` failed before the change (3 cases) and passes after; full suite and `go test -race` (64-bit build) pass.


## Indian Standard Time display - 2026-10-16

New reset messages reuse the existing frozen `LocalResetAt` field, explicitly formatted in IST (UTC+05:30) with a date. The host's local zone is no longer used for this field. Existing frozen display text and the legacy UTC fallback are unchanged.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| watch.compose | 7 | 7 |

Extracted: none; reused the existing message field, freeze operation and renderer. Only `compose` changed behavior. All production functions in its file were measured; the all-functions complexity ceiling including tests passes.

Behavior verified: the merged-main baseline race suite passed. The new composition regression failed on all three cases before the one-line fix, then passed for Codex, Claude and a year rollover while the host zone was UTC-08:00. Full race tests and vet pass. Tests compare actual Resend request JSON and delivery keys after message serialization and a host-zone change, and verify frozen IST display text survives SQLite reopen without being replaced. Legacy UTC rendering remains covered. The Windows CLI was rebuilt and its version command passed.


## Guided setup implementation

Measured every changed/new production function against `7216008` using gocyclo; Go AST function bodies identify touched functions independently of file moves. Ranked descending by the greater before/after score. New functions have no invented baseline. Platform-specific replacement functions are listed separately. All functions, including tests, remain at complexity 10 or below; current maximum is 9.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| main.(*application).activateSetup | New | 9 |
| main.(*application).collectSetupEmail | New | 9 |
| main.(*application).collectSetupKey | New | 9 |
| main.(*application).confirmSetupDisabling | New | 9 |
| main.(*application).statusSnapshot | 9 | 9 |
| main.(setupPrompter).choose | New | 9 |
| store.schedule | 8 | 9 |
| claudeconfig.writeDocument | New | 8 |
| main.(*application).prepareSetupProvider | New | 8 |
| main.readEnvSnapshot | New | 8 |
| store.(*DB).ValidateDelivery | New | 8 |
| store.moveAlert | 5 | 8 |
| watch.(*Engine).cancelReason | 7 | 8 |
| claudeconfig.(Installer).Restore | New | 7 |
| main.(*application).prepareSetupProviders | New | 7 |
| main.(setupPrompter).confirm | New | 7 |
| main.updateResendEnv | New | 7 |
| main.validateResendEnvKey | New | 7 |
| store.(*DB).ConfigureNotifications | 7 | 1 |
| store.(*DB).RecordSnapshot | 7 | 7 |
| store.(*DB).Schedule | 6 | 7 |
| store.(*DB).configureNotifications | New | 7 |
| store.(Config).Selected | New | 7 |
| store.scheduleSaved | 5 | 7 |
| watch.(*Engine).sendAlert | 6 | 7 |
| claudeconfig.(Installer).readBackup | New | 6 |
| claudeconfig.(Installer).validate | New | 6 |
| claudeconfig.readRegular | New | 6 |
| main.(*application).connect | 4 | 6 |
| main.(*application).connectClaude | New | 6 |
| main.(*application).defaultStatusProvider | New | 6 |
| main.(*application).selectSetupProviders | New | 6 |
| main.(*application).sendSetupTest | New | 6 |
| main.(*application).setup | New | 6 |
| main.(*application).status | 5 | 6 |
| main.(*application).watch | 6 | 6 |
| main.(setupPrompter).password | New | 6 |
| main.(setupPrompter).read | New | 6 |
| main.readSetupLine | New | 6 |
| main.saveResendEnv | New | 6 |
| quota.npmCodexBinary | New | 6 |
| store.configureState | New | 6 |
| claudeconfig.(journal).validEntries | New | 5 |
| claudeconfig.checkDirectory | New | 5 |
| claudeconfig.matchesEntry | New | 5 |
| claudeconfig.readDocument | New | 5 |
| localfile.Write | New | 5 |
| main.(*application).checkSetupProvider | New | 5 |
| main.(*application).claudeInstaller | New | 5 |
| main.(*application).installSetupClaude | New | 5 |
| main.(*application).offerSetupTest | New | 5 |
| main.(*application).promptSetupEmail | New | 5 |
| main.(*application).setupDefaultProvider | New | 5 |
| main.(*application).statusAll | New | 5 |
| main.(*application).statusFlags | New | 5 |
| main.(setupPrompter).ask | New | 5 |
| main.envAssignmentComment | New | 5 |
| main.inspectEnvForWrite | New | 5 |
| main.sameEnvSnapshot | New | 5 |
| store.(*DB).Claim | 2 | 5 |
| store.(*DB).EnableProvider | New | 5 |
| localfile.prepare | New | 4 |
| localfile.regularTarget | New | 4 |
| main.(*application).codexProvider | 2 | 4 |
| main.(*application).collectSetup | New | 4 |
| main.(*application).connectedProviders | New | 4 |
| main.(*application).printSetupSummary | New | 4 |
| main.(*application).setupIsTerminal | New | 4 |
| main.(*application).stateCommand | 4 | 4 |
| main.(*application).statusProviders | New | 4 |
| main.(*application).statusReadings | New | 4 |
| main.(setupPrompter).printChoices | New | 4 |
| main.(statusOptions).combined | New | 4 |
| main.validateSetupChoices | New | 4 |
| quota.codexBinary | 4 | 2 |
| quota.defaultCodexBinary | New | 4 |
| store.(*DB).ConfigureSetup | New | 4 |
| store.(*DB).Retry | 4 | 4 |
| store.(*DB).SaveCodexBinary | 4 | 4 |
| store.alertsToMove | 4 | 4 |
| store.claimSelected | New | 4 |
| store.reconcileNotifications | 4 | 3 |
| store.retrySelected | New | 4 |
| store.scheduleWindow | 4 | 4 |
| watch.(*Engine).Step | 3 | 4 |
| claudeconfig.(Installer).restoreEntry | New | 3 |
| claudeconfig.(journal).owns | New | 3 |
| claudeconfig.validInstalledEntry | New | 3 |
| localfile.replace [replace_other.go] | New | 3 |
| localfile.replace [replace_windows.go] | New | 3 |
| main.(*application).refreshStatusProviders | New | 3 |
| main.(*application).renderStatusReadings | New | 3 |
| main.(*application).reportUnfinishedSetup | New | 3 |
| main.(*application).restoreSetupTerminal | New | 3 |
| main.(*application).setupPassword | New | 3 |
| main.encodeEnvKey | New | 3 |
| main.replaceEnvAssignment | New | 3 |
| main.requireEnvUnchanged | New | 3 |
| main.setupReadError | New | 3 |
| store.(Config).ProvidersList | New | 3 |
| store.rejectActiveDelivery | 3 | 3 |
| watch.(*Engine).Observe | 3 | 1 |
| watch.(*Engine).cycleProvider | New | 3 |
| watch.(*Engine).observe | New | 3 |
| main.(*application).help | 2 | 2 |
| main.(*application).readSetupCodex | New | 2 |
| main.(*application).setupFlags | New | 2 |
| main.boundedEnvContent | New | 2 |
| main.completeSetupLine | New | 2 |
| main.setupCodexBinary | New | 2 |
| quota.DiscoverBinary | New | 2 |
| store.(Config).withProvider | New | 2 |
| claudeconfig.(Installer).backupPath | New | 1 |
| claudeconfig.(Installer).settingsPath | New | 1 |
| store.cancelDisabled | New | 1 |
| store.cancelMovedAlert | New | 1 |
| store.failExhausted | New | 1 |

Extracted: status argument parsing, provider resolution, snapshot collection and rendering; shared setup configuration transaction; delivery claiming and final lease validation; per-cycle provider construction; bounded prompts and hidden input; atomic local-file replacement; Claude journal validation, ownership and persistence. No existing exported signature was removed or changed.

During implementation, `status` measured 12 and `statusAll` 13 before extracting their separate jobs; their final scores are listed above. `setupPrompter.choose` measured 11 before extracting choice validation and finishes at 9. The interrupted-launcher recovery fix splits journal validation and ownership without hiding decisions.

Behavior verified: baseline and combined race suites, independent subsystem tests, real Windows CLI and hidden-input/Ctrl-C smoke checks, synthetic installer recovery including locked Windows replacement, provider cancellation/claim races, immutable retry payloads, legacy JSON compatibility and actual browser checks. See validation.md for limits and delivery state.


## Sender-format correction - 2026-10-19

| Function | Before | After |
|----------|--------|-------|
| watch.compose | 7 | 7 |
| notify.(Resend).request | 4 | 6 |
| main.(*application).testEmail | 5 | 5 |
| notify.statusError | 4 | 5 |

New tests and reminders record the corrected sender-format choice in the frozen message. An absent flag preserves old retry bodies and delivery keys; named senders keep their existing wire format. HTTP 422 includes static guidance without exposing a provider response. No helpers were extracted or public signatures changed.

Behavior verified: the sender regression failed for bare and angle-only addresses against isolated pre-fix commit `0f847ba`, then passed in the full race suite with the fix. Tests compare actual request JSON, serialized retry bodies and keys, and legacy/new format preservation after SQLite reopen. Vet and the repository complexity ceiling passed. Both Windows executable copies were rebuilt and their version commands ran. No live email was sent.

## Remove inline prompt suggestions - 2026-10-19

| Function | Before | After |
|----------|--------|-------|
| main.(setupPrompter).ask | 5 | 4 |

Removed only the bracketed default display from the shared prompt writer. Email, sender, numeric choices and confirmations retain their input handling, including blank-input defaults and cancellation. No helpers were extracted. CLI race tests and vet passed; the prompt regression checks that the saved email, `[3]` and `[n]` are absent from the output.

## Claude usage through Claude Code — 2026-10-19

Claude readings now come from Claude Code's experimental `get_usage` control request (no prompt sent) instead of status-line callbacks. New: `quota.Claude` (`claude.go`, `claude_rpc.go`), multi-provider polling in the watcher, `keepOmittedWindows` for partial readings, and status-line restoration in setup and `connect claude`. Removed: status-line decoding, the Claude merge, the install path in setup, and the guard that retired confirmed Claude payloads.

| Function | Before CC | After CC |
| --- | ---: | ---: |
| quota.decodeClaudeUsage | New | 9 |
| store.keepOmittedWindows | New | 8 |
| store.(*DB).RecordSnapshot | 7 | 8 |
| quota.(*claudeSession).request | New | 7 |
| main.(*application).statusSnapshot | 9 | 7 |
| main.(*application).activateSetup | 9 | 7 |
| quota.claudeWindowAt | 5 | 6 |
| quota.(Claude).Read | New | 6 |
| main.(*application).prepareSetupProviders | 7 | 6 |
| quota.startClaude | New | 5 |
| watch.(*Engine).sendAlert | 7 | 5 |
| watch.(*Engine).Step | 4 | 5 |
| main.(*application).connectClaude | 6 | 4 |
| watch.(*Engine).cycleProviders | New (replaces cycleProvider, 3) | 4 |
| main.(*application).claudeStatusLine | 5 | 1 |
| store.mergeClaude | 7 | Removed |
| quota.DecodeClaudeStatusLine | 7 | Removed |
| main.(*application).installSetupClaude | 5 | Removed |

Behavior verified: full suite and `go test -race` (64-bit build) pass; `gocyclo -over 10 cmd internal` passes. A built binary ran `connect claude` and `status --provider claude --refresh` against a real Claude Code 2.1.301 Pro sign-in (no prompt, throwaway state); tests use a fake Claude Code process and never reach a real account.
