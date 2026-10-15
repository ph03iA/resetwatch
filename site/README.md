# ResetWatch website

The install page is a dependency-free static site in `dist/`. Deploy that directory with a static-file host. The release packaging step supplies `dist/downloads/` with platform archives, source, and checksums.

The page works without JavaScript for Windows installation. JavaScript enables accessible OS tabs, command copying, and the clearly labeled reset preview. The illustrative readout changes from 42 to 00 minutes only when the preview button is pressed; it is not a live countdown. A custom SVG envelope lifts its letter, and a sample email explains that availability is unconfirmed. The letter motion and email reveal respect reduced-motion preferences. Fonts and their licenses are bundled locally in `dist/fonts/`; no analytics, remote font services, or third-party scripts are loaded.

Installation examples must stay aligned with the CLI commands and archive layouts. `app.js` centralizes platform-specific commands and filenames. Copying uses the browser clipboard API; when unavailable, the command text is selected and the page explains how to copy it manually.

Preview through a local static HTTP server. Serving over HTTPS enables clipboard permissions in production. This website does not connect to an account, send email, or store API keys.
