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

Behavior verified: provider tests passed before and after extraction. The complete suite subsequently passed with the race detector using Go 1.27.2. `gocyclo -over 10 cmd internal` reports no violations. Website JavaScript named functions were counted manually: maximum CC 5 after the envelope preview update; nested callbacks maximum CC 2. Python release functions have maximum CC 5 by manual decision counting. Developer preview server handler CC 5 (guard, conditional expression, catch, nullish fallback).

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
