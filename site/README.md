# ResetWatch website

A dependency-free static page in `dist/`: `index.html`, `styles.css`, `app.js`, `example-email.html`, the favicon and self-hosted fonts with their licenses. No analytics, cookies, remote fonts or third-party scripts.

The page is set like a printed manual: paper and ink, one copper accent, hairline rules, Instrument Serif headings and Manrope text. It follows the visitor's light or dark setting. Keep it plain: no gradients, shadows, rounded corners, glass effects, icons, emoji, animations or card grids.

It shows real product output instead of mock-ups:

- `example-email.html` is the reset email exactly as ResetWatch's own email code renders it, with example values. Regenerate it from `messageContent` in `internal/notify` if the email changes.
- The status block is the output of the real `status` renderer, with example values.

Sections: how it works, install (Windows and macOS/Linux), the six commands, limits, privacy and terms (the MIT License). Keep the commands in `index.html` and `app.js` matching the CLI. Windows uses `.\resetwatch.exe`, macOS/Linux `./resetwatch`. Without JavaScript the Windows commands are shown; JavaScript adds the OS switch, copy buttons and fits the email frame to its content.

Preview with `node tools/serve.mjs` from the repository root. Keep the site unpublished unless publishing is explicitly requested.
