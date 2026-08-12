# Flow

A focus timer that you cannot turn off, and a site blocker that takes fifteen minutes to switch
off, on Windows.

The value of this app is that it does not negotiate with you. A session, once committed, runs to
zero — there is no stop button, and closing the window does nothing because the window is not what
enforces anything. Everything else here is packaging around that one property.

There is always a way out: the escape hatch cannot be disabled by any setting, and an administrator
can uninstall the whole thing in under two minutes, mid-session included. Both are deliberate. See
[Getting out](#getting-out).

---

## Install

### 1. Get the installer

**There is no published download yet.** Nothing has been released on
[the releases page](https://github.com/Lushenwar/Flow/releases), so for now you build it yourself.

You need [Go](https://go.dev/dl/), [Node](https://nodejs.org/), the Wails CLI, and NSIS:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@latest
winget install NSIS.NSIS
```

Then, from the repository root:

```powershell
.\build-release.ps1 -Arch amd64     # use arm64 on an ARM machine
```

That writes `build\bin\Flow-amd64-installer.exe`.

### 2. Run it

Double-click the installer and accept the UAC prompt. It needs administrator rights because it
registers a background service — that is the part that survives closing the app, killing the
process, and rebooting.

> **Windows will warn you that the publisher is unknown.** Click **More info → Run anyway**.
> The installer is not code-signed, because signing needs a certificate this project does not have.
> That warning is accurate and you should treat it with the suspicion you would give any unsigned
> installer: read `build-release.ps1` and `build\windows\installer\project.nsi`, or build it
> yourself from source, which is the same thing this installer does.

It installs to `C:\Program Files\Flow` and sets up:

| | |
|---|---|
| `Flow.exe` | the window — desktop and start-menu shortcuts |
| `flowd.exe` | the daemon, registered as the service **Flow**, starting automatically at boot |
| `flowctl.exe` | a read-only debug CLI |
| `extension\` | the browser extension, for you to load manually |

### 3. The browser extension (optional)

The extension is **additive**. Without it, blocking still works at the network level; with it, two
things become possible that a network layer structurally cannot do — blocking `youtube.com/watch`
while a channel URL stays reachable, and closing a tab that was already open when a session started.

1. Open `chrome://extensions`
2. Turn on **Developer mode** (top right)
3. **Load unpacked** → `C:\Program Files\Flow\extension`

Chrome and Edge only for now. It has not been tested in Firefox.

---

## Using it

**Focus** — one dial. Pick a duration, tap to commit. You get fifteen seconds to back out, and the
blocks are already live during those fifteen seconds. After that the tap does nothing and the
countdown runs to zero.

**Blocking** — categories that stay on with no timer. Turning one **on** is instant. Turning one
**off** takes fifteen minutes, and it stays fully enforced for every one of them. That asymmetry is
the entire point: a switch you can flip at 11pm is a decoration, not a blocker.

**Custom sites** — the sites the presets do not cover. Paste a domain or a whole URL and it is
blocked immediately, subdomains included. Removing one is refused while the list is enforced; turn
**Custom sites** off first, which takes the usual fifteen minutes, then edit.

**The mini timer** — the button in the title bar shrinks the window to just the countdown and pins
it above everything else, like a sticky note. It is deliberately inert: somewhere to glance, not a
second place to start or stop anything.

---

## Getting out

**Uninstall works in every state, including mid-session.** This is not a loophole, it is a
requirement — never fight the administrator.

Either:

* **Settings → Apps → Installed apps → Flow → Uninstall**, or
* from an elevated prompt: `"C:\Program Files\Flow\flowd.exe" uninstall`

Both deregister the service, remove the `hosts` entries, restore your original DNS servers, and
delete `C:\ProgramData\Flow`. Nothing is left behind.

**To end a single session early**, use "end early" on the Focus screen. It starts a fifteen-minute
countdown during which everything stays blocked, then asks you to type a 100-character string
exactly. The delay cannot be shortened, disabled, or configured away by any setting. An escape hatch
that can be removed is a trap.

---

## What this does not protect you from

Stated plainly, because discovering these yourself and concluding the app is broken is worse:

* **Your phone.** No desktop app can do anything about the device in your pocket.
* **A VPN, or tethering to another network.** Undetected.
* **Another computer.** Obviously.
* **Safe Mode.** Deliberately out of scope — a service that mishandles boot-critical registration
  can produce a machine that will not start, and a bricked laptop is a far worse outcome than ten
  more minutes of Reddit.
* **You, with administrator rights and twenty minutes.** Uninstall always works, by design.
* **A tab that is already open.** Blocking bites when something needs to look up a name, so an
  established connection can survive a minute or two into a session. The browser extension closes
  this gap; the network layer alone does not.

A commitment device raises the effort of a bad habit above the effort of the thing you meant to do.
That is the whole mechanism. It is not a prison, and building it as one would make it a trap.

---

## What it does to your machine

No hiding, and nothing you cannot inspect:

* A service named **Flow**, running as `LocalSystem`, visible in `services.msc` under its real name.
* Signed state in `C:\ProgramData\Flow`. Signing makes casual database edits fail loudly — it is
  tamper-**evident**, not tamper-proof, because the key sits on the same disk as the data.
* A marked block in your `hosts` file, removed on uninstall.
* Your DNS servers pointed at a resolver running on `127.0.0.1`, with the originals backed up to
  `C:\ProgramData\Flow\dns-backup.json` and restored on uninstall.
* Firewall filters that force DNS through that resolver, so a browser cannot quietly use its own.

Check any of it with `flowctl health` and `flowctl verify`. Both are read-only.

---

## Development

The architecture, the threat model, and the reasoning behind each decision are in
[`claude.md`](claude.md), which is the binding spec.

```powershell
# daemon, console mode. -dev logs enforcement instead of applying it — use it.
go run ./cmd/flowd -dev -port 8787

# UI, in another terminal
cd ui
$env:NEXT_PUBLIC_FLOW_URL   = "http://127.0.0.1:8787"
$env:NEXT_PUBLIC_FLOW_TOKEN = (Get-Content "$env:ProgramData\Flow\token").Trim()
npm run dev                 # http://localhost:3000

# the desktop app, which needs no token — it proxies the API and adds the header itself
wails build -platform windows/amd64 ; .\build\bin\Flow.exe
```

`FLOW_DATA_DIR` overrides `C:\ProgramData\Flow`, which is how you run a throwaway instance without
touching the real one.

On a fresh clone run `cd ui; npm run build` once before any Go build: the desktop shell embeds
`ui/out`, and `go:embed` fails on a directory that does not exist.

Full checks:

```powershell
go test . ./cmd/... ./internal/... ; go vet . ./cmd/... ./internal/...
cd extension ; npm test
cd ../ui ; npm test ; npm run typecheck ; npm run lint ; npm run build
```
