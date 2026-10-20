# ResetWatch

A small command-line tool that emails you when a used-up Codex or Claude usage limit comes back.

**Status:** v0.1.0, a locally tested release candidate. There are no prebuilt downloads and no hosted service: you build it from source and it runs on your own computer.

**Gmail sign-in needs the project owner's Google client.** Emails go through your own Gmail account, and signing in to Google needs a Desktop OAuth client registered by the project owner. This source tree ships without one (`internal/gmailauth/google-client.json` is empty), so until the owner adds it, `setup` and `gmail connect` stop with a message saying so. See [docs/details.md](docs/details.md#for-the-project-owner-the-google-client).

## How it works

1. The watcher checks your Codex and Claude usage once a minute. It sends no prompts, so checking uses none of your limits.
2. When a limit reaches **100%**, it notes when that limit resets.
3. At the reset time, it checks again and sends **one email**:
   - **"is back"**: a fresh reading confirms you can use it again.
   - **"should be back"**: it could not confirm (offline, signed out, or another limit is still at 100%).

Limits that never reach 100% never send email. Each limit counts on its own: Codex 5-hour and weekly, Claude 5-hour and weekly.

## Install

You need Git and Go 1.27.2 or newer.

Windows (PowerShell):

```bat
git clone https://github.com/ph03iA/resetwatch.git
cd resetwatch
go build ./cmd/resetwatch
.\resetwatch.exe version
```

macOS and Linux:

```sh
git clone https://github.com/ph03iA/resetwatch.git
cd resetwatch
go build ./cmd/resetwatch
./resetwatch version
```

The examples below use `./resetwatch`; on Windows write `.\resetwatch.exe`. To run it from any folder, use `go install ./cmd/resetwatch` instead and add Go's `bin` folder to your `PATH`.

Windows Smart App Control may block a locally built, unsigned `resetwatch.exe` ("An Application Control policy has blocked this file"). ResetWatch cannot work around this.

## Set up

You need:

- **Codex** signed in with a ChatGPT plan (`codex login`), and/or **Claude Code** signed in with a Pro or Max plan (`claude`).
- A **Google account with Gmail**. Reminders are sent from it to itself.
- A desktop with a working system credential store: Windows Credential Manager, macOS Keychain, or Linux Secret Service (for example GNOME Keyring). Headless servers are not supported in this version.

```sh
./resetwatch setup
```

Setup asks which tools to watch and checks each one, then opens your browser to sign in to Google. Google asks to let ResetWatch **send email on your behalf**; it never gets permission to read your inbox. The sign-in is kept in your system credential store, never in a file. Running setup again lets you keep the connected account, connect a different one, or cancel.

Setup can send one test email at the end. A test that arrives proves delivery works.

`resetwatch gmail connect --no-browser` prints the sign-in link instead of opening a browser; open it on the same computer.

## Run the watcher

```sh
./resetwatch watch
```

Keep this window open and the computer awake and online. `Ctrl+C` stops it safely; saved reminders continue next time. It does not start by itself after a restart.

Check what it sees at any time:

```sh
./resetwatch status            # last saved readings
./resetwatch status --refresh  # read fresh usage first
./resetwatch history           # emails sent or waiting
```

## Commands

| Command | What it does |
| --- | --- |
| `setup` | Guided setup. Flags: `--providers codex\|claude\|both`, `--codex-bin PATH`, `--non-interactive` (needs Gmail already connected), `--test-email`. |
| `watch [--interval 1m] [--once]` | Check usage and send due emails. Interval: 15s to 1h. |
| `status [--provider codex\|claude] [--all] [--refresh] [--json]` | Show usage readings. |
| `history [--limit 20] [--json]` | Show reminder emails and their state. |
| `gmail connect [--no-browser]` | Connect, reconnect or change the Gmail account. |
| `gmail status [--json]` | Show the connected account and whether delivery is paused. Does not contact Google. |
| `gmail disconnect [--yes]` | Disconnect Gmail on this computer and cancel unsent reminders. |
| `test-email` | Send one email marked TEST to your Gmail account now. Checks no usage. |
| `version` | Print the version. |

To change which tools are watched, run `setup` again. `--email` and `--from` were removed: reminders always go from your Gmail account to itself.

## Limits

- **Claude's usage check is experimental.** ResetWatch starts Claude Code with no prompt and asks for the numbers `/usage` shows (the Claude Agent SDK's `get_usage` request). A Claude Code update may change or remove it. If it breaks, Claude emails fall back to "should be back"; they do not stop.
- **Offline at reset time:** the email says "should be back" and is sent once you are online again, within 23 hours.
- **"Sent"** means Gmail accepted the email. It does not guarantee it reaches your inbox.
- **Rare duplicates.** If Gmail's answer is lost, ResetWatch retries, and Gmail may deliver the same reminder twice. Gmail offers no way to prevent this.
- **Sign-ins can stop working.** If you remove ResetWatch's access in your Google account, or Google ends the sign-in, delivery pauses (usage is still read) until you run `resetwatch gmail connect`. Reminders keep their attempts while paused.
- One account per tool and one email setup per state folder. Codex accounts are told apart by email, so two workspaces on the same email look the same.
- API-key Codex accounts and API-key, Bedrock or Vertex Claude sessions have no plan limits to watch.
- It cannot bypass limits, renew an expired sign-in, or wake a sleeping computer.

## Privacy and local data

- **Codex:** ResetWatch talks to your local Codex app-server (`account/read`, `account/rateLimits/read`). It never reads Codex sign-in files.
- **Claude:** ResetWatch runs your local Claude Code with your settings, hooks, plugins and MCP servers turned off. It never reads Claude credentials or changes Claude settings, except to restore a status line an older ResetWatch version replaced.
- **Email:** ResetWatch sends through Google's Gmail API over HTTPS with send-only permission, from your address to your address. The long-lived sign-in (refresh token) is kept in your system credential store; short-lived access tokens stay in memory. Neither is written to the database or printed. `gmail disconnect` deletes the local sign-in; to withdraw access at Google too, remove ResetWatch at [myaccount.google.com/permissions](https://myaccount.google.com/permissions).
- **State:** saved in an unencrypted SQLite database: `%AppData%\resetwatch\state.db` on Windows, `~/Library/Application Support/resetwatch/` on macOS, `~/.config/resetwatch/` on Linux. Set `RESETWATCH_HOME` to use another folder. It holds your Gmail address and Google account ID, usage readings, a hashed account ID per tool and email history.

## Upgrading from a Resend version

Earlier versions sent email through Resend. To upgrade: stop every running watcher, build this version, and run `resetwatch setup`. Connecting Gmail upgrades your saved state in one step and carries unsent reminders over to Gmail; sent and failed history is kept. Until then, `status` and `history` still work, but `watch` asks you to upgrade. Your old `.env` file is left alone and is no longer read; delete it when you like.

## More

- [docs/details.md](docs/details.md): exact alert and retry rules, Gmail sign-in and credentials, Codex location, running as a Linux service, upgrading, and the owner's Google client.
- [SECURITY.md](SECURITY.md), [CHANGELOG.md](CHANGELOG.md), [AGENTS.md](AGENTS.md) (coding rules).

## Develop

```sh
go test ./...
go vet ./...
go run github.com/fzipp/gocyclo/cmd/gocyclo@v0.6.0 -over 10 cmd internal
```

CI also checks formatting, runs `go test -race ./...`, `govulncheck` and `node --check site/dist/app.js`. The install website is in `site/dist/`; preview it with `node tools/serve.mjs`.
