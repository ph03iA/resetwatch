# ResetWatch website

The install page is a dependency-free static site in `dist/`. It shows clone-and-build instructions, then `resetwatch setup` and `resetwatch watch`. No prebuilt downloads or hosted watcher are provided.

Without JavaScript, the Windows commands are shown. JavaScript adds the macOS/Linux tab, copying and scroll animations. Local fonts and their licenses are bundled; no analytics, remote fonts or third-party scripts are loaded.

Keep the default HTML instructions and both command sets in `app.js` aligned with the CLI. Windows uses `.\resetwatch.exe`; macOS/Linux uses `./resetwatch`. Command copying uses the clipboard API with a text-selection fallback.

Guided setup chooses Codex, Claude or both, asks for email and sender, reuses an existing Resend key or saves a missing key in the current folder's `.env`, and offers an optional test. Users must run watch from that folder. The default `onboarding@resend.dev` sender only reaches their Resend account email; other recipients require a verified domain. The website itself never collects credentials, connects accounts or sends email.

The page retains its decorative envelope, process rail, three-step installation workbook, operating notes and responsive layout. It contains no simulated account state, countdown, sample email or demo controls. Codex and Claude emails can say "is back" from a fresh reading; Codex also needs backend permission. Reset emails use the computer's time zone with a UTC reference.

Preview locally with `node tools/serve.mjs` from the repository root. Keep the site unpublished unless publishing is explicitly requested.
