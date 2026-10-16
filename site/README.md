# ResetWatch website

The install page is a dependency-free static site in `dist/`. Deploy that directory with a static-file host. Download links point to the GitHub release `v0.1.0`. `python tools/release.py` writes the platform archives, source archive and checksums to `work/dist/`; attach those files to that release.

The page works without JavaScript for Windows installation. JavaScript enables accessible OS tabs and command copying. Fonts and their licenses are bundled locally in `dist/fonts/`; no analytics, remote font services, or third-party scripts are loaded.

Installation examples must stay aligned with the CLI commands and archive layouts. `app.js` centralizes platform-specific commands and filenames. Copying uses the browser clipboard API; when unavailable, the command text is selected and the page explains how to copy it manually.

The page pairs the hero copy with the original graphite envelope illustration, followed by a continuous process rail, a three-step installation workbook, an operating-notes ledger and a large wordmark footer. The envelope is a decorative SVG hidden from assistive technology. Desktop layouts change to a vertical process timeline and stacked instructions on smaller screens. The page contains no simulated account status, countdown, email mockups or demo controls. Expected and confirmed reset behavior is explained in the operating notes. The default sender is `onboarding@resend.dev`, which only sends to the operator's Resend account email. Sending to other recipients requires a verified domain.

Preview through a local static HTTP server. Serving over HTTPS enables clipboard permissions in production. This website does not connect to an account, send email, or store API keys.

Save your key once: copy `.env.example` to `.env` in the folder where you will run ResetWatch. Open `.env` in your editor, replace the example value and save. If `.env` already exists, edit that file.

```dotenv
RESEND_API_KEY=your_resend_api_key
```

Run the watcher and delivery test from that same folder so ResetWatch can read `.env`. The website's copied commands configure the recipient and start the watcher; they do not overwrite your key file.

To test actual email delivery from a local Windows source build, run this separately from the project root after saving `.env` there:

```powershell
go build -o .\bin\resetwatch.exe ./cmd/resetwatch
.\bin\resetwatch.exe test-email --email 'YOUR_RESEND_ACCOUNT_EMAIL'
```

With an installed binary on `PATH`, use `resetwatch test-email --email 'YOUR_RESEND_ACCOUNT_EMAIL'`. Replace the example key and recipient with your own values. The command sends one email labeled **TEST**, without Codex, quota checks, or database access. Its default sender, `ResetWatch <onboarding@resend.dev>`, only reaches your own Resend account email; other recipients require `--from` with a sender on your verified domain. An accepted provider receipt does not prove inbox delivery. There are no automatic retries; each rerun sends another test.
