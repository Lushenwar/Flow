# CLAUDE.md — Flow (Commitment Blocker + Focus Timer, Desktop)

> **Name: `Flow`.** Named for the state it exists to protect, not for the mechanism that protects it.
> `Portcullis` had the metaphor backwards (it keeps invaders out; this app keeps you in), and both
> `Mast` and `Rubicon` named the restraint rather than the point of it — the lock is the means, an
> hour of uninterrupted attention is the end.
> Baked into `paths.AppName`, the service registration, `%ProgramData%\Flow`, and the binaries
> `flowd` / `flowctl`.

## WORKFLOW: BRANCH + PR ONLY

No direct commits to `main`. Every change goes: `git checkout -b <branch>` → commit → `gh pr create`.
A pre-commit hook (`.git/hooks/pre-commit`) enforces this locally by rejecting commits made while on `main`.

## CURRENT STATUS

╔══════════════════════════════════════════════════════════╗
║  FLOW BUILD PROGRESS                      7/8 DONE ║
║  ██████████████████████████░░  ALL PHASES BUILT, 1 & 7 [~]║
║  Phase 0: Daemon, Service Install, Signed Store  [DONE]  ║
║  Phase 1: Enforcement Core & Reconciliation      [~   ]  ║
║  Phase 2: Session State Machine & Time Authority [DONE]  ║
║  Phase 3: UI Shell + Blocking Screen (baseline)  [DONE]  ║
║  Phase 4: Focus Screen — the Dial & Grace Window [DONE]  ║
║  Phase 5: Escape Hatches & Tamper Event Log      [DONE]  ║
║  Phase 6: Time Bank & Scheduled Hard-Locks       [DONE]  ║
║  Phase 7: Browser Extension & Installer          [~   ]  ║
╚══════════════════════════════════════════════════════════╝

Phase: all eight built; 1 and 7 remain `[~]`.

**Phase 6.** Time bank credits 0.2 recreation minutes per focus minute, on the transition into
COMPLETE only, in the same signed write as the state — so a crash between the two cannot double-pay
or skip. Aborted and escaped sessions earn nothing. A spend is the one path in the app that lifts
enforcement without waiting out a delay, so it is hemmed in: IDLE only, deducted up front, and no
cancel, because "spend 30, use 2, refund 28" is an off switch with extra steps.

Scheduled locks arm from the daemon's own tick with no UI involved, and are attributed to
`schedule`, so a session ending never lifts them.

**Timezone pinning had a hole worth remembering:** `time.Local.String()` returns the literal
`"Local"`, and `LoadLocation("Local")` resolves to whatever the machine claims right now — so storing
that name pinned nothing at all, which is exactly the attack it was meant to stop. Schedules now
capture the UTC offset at creation and fall back to it whenever the stored name is `"Local"`, empty,
or unknown. Go has no stdlib Windows-to-IANA mapping, so the offset is the portable anchor. Cost: a
window pinned that way does not follow DST.
Status: The escape hatch is complete — request, 15-minute countdown with everything still enforced,
100-character case-sensitive typed challenge, release. Every drift event, repair, and escape is in
the log, surfaced as "Recent activity" at the bottom of the Blocking screen.

`POST /api/session/release` is gone; `GET /api/session/escape/challenge` and
`POST /api/session/escape/verify` replace it. The challenge is refused until `availableAt` has
passed, so it cannot be fetched early and pre-typed, and it is held in memory rather than the store —
a daemon restart costs a retype and never shortens the wait.

The history list lives on the Blocking screen rather than a third tab. Two screens is a hard
constraint, and a list is not a control.

`GET /api/state` ships `durationSeconds` and `graceSeconds` alongside the remaining counts. Without
the denominators the UI would have to remember what it asked for, and a reopened window would draw
the wrong ring.

**Deviation from the file layout above:** baseline rules live in `internal/session/baseline.go`, not
`internal/baseline/`. They consume `Delay`, `Delay` consumes `Clock`, and a separate package would
need a third one holding the time primitives purely to break the import cycle — more structure than
one struct earns.

The watchdog is the SCM's own recovery configuration (3 restarts, 60s reset window), not a respawn
loop of our own. It does not fire on a deliberate SCM stop, which is exactly the required semantics
with no code to get wrong.

**Verified elevated (2026-08-05).** All four layers report `active`. `youtube.com`, `www.youtube.com`
and `googlevideo.com` sink to 127.0.0.1 via `hosts`; `m.youtube.com` NXDOMAINs via the sink's suffix
match, which is the layering doing exactly what it was split up to do. `cdc.gov`, `988lifeline.org`,
`go.dev` and `wikipedia.org` all still resolve. `1.1.1.1:443` and `8.8.8.8:443` are refused while
`wikipedia.org:443` stays open. Deleting the `hosts` block by hand restores it within 6s with a
`reconcile_repaired` event. Service installs as `LocalSystem`, `Automatic`, with SCM recovery
actions 5s/15s/30s over a 60s window; `Stop-Process` on the service PID brings back a new one.
Uninstall leaves no service, no data dir, no `hosts` block, and the original resolvers restored.

**Verified in Chrome (2026-08-05).** With `preset.video` and `preset.doomscroll` enforced:
`reddit.com` and `music.youtube.com` fail immediately; `wikipedia.org` and `cdc.gov` load normally;
`youtube.com` works again the moment the daemon is torn down.

**Phase 1 stays `[~]`.** Its exit criterion says "an already-open stream is terminated on rule
application", and the network layer still does not do that — a YouTube tab open before enforcement
started kept serving for about two minutes. The extension now closes that gap in Chrome, but Phase 1
is about the enforcer, and a browser without the extension is still exposed. See Correction 1.

**Phase 7 (Chrome only) is built.** `GET /api/rules` is unauthenticated by design — an extension
cannot read `%ProgramData%\Flow\token`, and the endpoint is loopback-only, read-only, and grants no
authority. The extension adds the two things the network layer structurally cannot do: URL-path
granularity, and closing tabs that were already open when a session started.

**Verified in Chrome (2026-08-05), extension loaded:**

| Check | Result |
|---|---|
| `reddit.com` | redirected to the blocked page |
| `youtube.com/watch?v=…` | blocked |
| `youtube.com/@veritasium` | reachable — the exit criterion |
| `amazon.com` open, then commit a session blocking it | swept to the blocked page within 6s |

That last row is the Correction 1 gap closed, by the extension rather than the network layer, which
still cannot do it.

**Two bugs found by loading it, both invisible to the tests that existed:**

1. MV3 suspends the service worker after ~30s idle. `checkTab` evaluated against `rules === null`,
   so the first navigation after every wake sailed through. Rules now cache in
   `chrome.storage.session` and every check awaits a priming promise.
2. Fixing (1) introduced a deadlock: `prime()` → `fetchRules()` → first fetch always "changed" →
   `sweepAllTabs()` → `checkTab()` → `await prime()`, still pending. Nothing was ever blocked, while
   `setInterval` kept polling so the daemon logged a healthy poll every 2 seconds. **The obvious
   health signal was the one thing still working.** Priming now refreshes with `sweep: false`.

The orchestration moved to `coordinator.js` with the chrome APIs injected, because neither bug was
reachable from the pure matcher tests. Its deadlock guard rejects rather than hanging, so a
recurrence fails the run instead of looking green.

`[~]` only because `install.ps1` is not the signed installer the exit criterion asks for.

**The desktop shell is built.** `Flow.exe` is a Wails v2 window over the same static export, and
`build-release.ps1 -Arch amd64` produces an NSIS installer that drops `flowd.exe`, `flowctl.exe` and
the extension, then registers the service in one elevated pass. Before this, "run the app" meant Go,
Node, a dev server, and a token pasted into an env var — there was no artifact to hand anyone.

**The browser build could never have shipped, for a reason that only shows up when you package it.**
`NEXT_PUBLIC_FLOW_TOKEN` is baked in at compile time, so a build is bound to one machine's token.
The shell fixes this by not giving the frontend a token at all: `proxy.go` forwards `/api/*` to the
daemon and attaches the bearer header in Go. The token never enters the bundle.

That proxy also closes a hole that would have shipped broken. Wails serves the page from
`wails.localhost`, so a direct fetch to `127.0.0.1:8787` is **cross-origin** — and `devCORS` sends
headers only in dev builds, on the stated assumption that "in release the UI is same-origin". That
assumption was false the moment the UI moved into a webview. Proxying makes it true again, which is
the right direction: the alternative was widening the daemon's CORS in release to make the frontend's
mistake work.

**Mini mode** is the sticky-note timer: `/mini` renders the bare dial, and `SetMini` shrinks the
window to 220×250, pins it always-on-top, and parks it in the top-right. Always-on-top applies *only*
in mini mode — a full-size window that refuses to go behind anything is an irritant, not a
commitment device. `Frameless` is build-time in Wails v2, so the full-size window draws its own title
bar too. The Document PiP pop-out stays for the browser build and is hidden in the shell; two buttons
for one job is worse than either.

**Phase 7 stays `[~]`.** The installer is not signed — Authenticode needs a certificate this project
does not have, so every user gets a SmartScreen warning on first run. Autostart for the window is
also not wired (the *daemon* auto-starts; the window does not).

**Still unverified:** Firefox and Edge, reboot survival of a locked session, and the installer itself
— it has been built but never run, so the service-registration and teardown paths inside `project.nsi`
are unexercised. Only `amd64` matters for real users; the local builds have been `arm64`.

**Known gap found during that run:** the WFP DoH blocklist is by IP, and Firefox's default endpoint
is `mozilla.cloudflare-dns.com` — a CDN address, not `1.1.1.1`. Blocking the well-known resolver IPs
does not stop it. Closing this needs the DoH bootstrap *hostnames* NXDOMAINed at the sink.
Update this as you finish each step.

**Checks:** `go test . ./cmd/... ./internal/... && go vet . ./cmd/... ./internal/... && (cd extension && npm test) && cd ui && npm test && npm run typecheck && npm run lint && npm run build`

Named packages rather than `./...` because `ui/node_modules` ships a stray Go package
(`flatted/golang`) that `./...` picks up as part of this module. The leading `.` is the Wails
desktop shell at the module root — it holds the API proxy, so leaving it out of the check line
means the one component that handles the bearer token is the one nothing verifies.

**On a fresh clone, run `(cd ui && npm run build)` once before anything Go.** The root package
embeds `ui/out`, and `go:embed` fails on a directory that does not exist. Nothing is committed there
to prevent it because `next build` deletes and recreates that directory, so a placeholder would
disappear on first use. `wails build` and `build-release.ps1` both build the frontend first and are
unaffected.

---

## WHAT THIS FILE IS

The authoritative guide for building Flow: a desktop commitment device combining a hard-locked
focus timer with OS-level distraction blocking. Binding spec for the daemon, the local API, and the
UI. Where this file and a code comment disagree, this file wins until someone edits this file.

The product thesis in one line: **the app's value is that you cannot turn it off.** Everything else —
the dial, the stats, the presets — is packaging around that single property. Any change that makes a
lock easier to release is a regression, not a convenience feature.

---

## THE CENTRAL ARCHITECTURE: TWO SURFACES, ONE ENFORCER

The app has exactly two screens, and they are not two views of the same thing. They control two rule
sets with different lifetimes, different psychology, and different friction models.

```text
   ┌─────────────────────────┐     ┌─────────────────────────┐
   │      FOCUS SCREEN       │     │    BLOCKING SCREEN      │
   │                         │     │                         │
   │   the dial — one big    │     │   category toggles,     │
   │   control, tap to       │     │   no timer, indefinite  │
   │   commit, counts down   │     │   on/off                │
   │                         │     │                         │
   │   SESSION RULES         │     │   BASELINE RULES        │
   │   timed, hard-locked    │     │   permanent until you   │
   │   lift at 0:00          │     │   wait out the delay    │
   └───────────┬─────────────┘     └───────────┬─────────────┘
               │                               │
               └───────────────┬───────────────┘
                               ▼
                    ┌─────────────────────┐
                    │   ENFORCER (daemon) │
                    │   applies UNION of  │
                    │   both rule sets    │
                    └─────────────────────┘
```

### Composition rules

* **Effective blocklist = baseline ∪ session.** Never an override, never a precedence chain. A union
  is the only composition that cannot accidentally unblock something.
* **Session end lifts only session-attributed rules.** If `preset.video` is on in baseline *and* in
  the session, it stays blocked at 0:00. Attribution is tracked per rule, per source.
* **Baseline is not "off".** The Focus screen's IDLE state still has every baseline rule live. A user
  in IDLE with gambling and adult content on baseline is being protected right now; the dial reading
  "not focusing" must never imply nothing is enforced.
* **Neither surface can weaken the other.** Turning a baseline category off during an active session
  is rejected outright (`409 would_weaken`) — the delayed-disable path doesn't even start.

### Asymmetric friction — the single most important rule on the Blocking screen

A plain toggle is not a blocker. At 11pm you want Reddit, you flip the switch, you're on Reddit, and
the screen is decorative inside a week.

> **Turning a baseline rule ON is instant. Turning it OFF takes 15 minutes, during which it stays
> fully enforced.**

The disable request is visible and cancellable ("unblocks in 14:32 · cancel"), cancelling is instant
and free, and re-enabling during the countdown cancels it too. This is the same delayed-cancel
machinery as the session escape hatch — **build it once in `session/escape.go`, use it in both
places.** Do not write a second implementation for baseline.

### Screen responsibilities, precisely

| | Focus screen | Blocking screen |
|---|---|---|
| Primary control | The dial. One tap commits. | Rows of switches. |
| Timer | Yes — it *is* the UI | None |
| Blocklist editing | **No.** Read-only summary of what this session covers. | Yes, this is the whole screen |
| Schedules | No | Yes — third row type (bedtime, delivery windows) |
| Lift condition | Countdown reaches zero | 15-minute disable delay elapses |

Choosing a session's blocklist *on the dial* would be a bypass: a user mid-craving would deselect
YouTube and commit to a session that blocks nothing. Session composition lives in a settings
sub-screen, edited when calm, not at the moment of commitment.

### The third state Nord doesn't have

A VPN power button has two states. The dial has three, and the middle one is the whole reason the app
is usable:

* **IDLE** — dial shows a duration, "tap to commit". Baseline still enforced.
* **ARMING** — 15 seconds. The ring sweeps. Tap again to back out, free and instant. **Session blocks
  are already applied** — the grace window releases the *lock*, not the *blocks*, or the window
  becomes the bypass.
* **FOCUS** — the dial counts down and the tap does nothing. There is no control to render.

---

## CORRECTIONS TO THE ORIGINAL BRIEF

Five things in the brief are wrong or under-specified; building them as written produces something
that either doesn't work or shouldn't ship.

1. **Holding a write-lock on `hosts` is the wrong primitive.** `LockFileEx` blocks a text editor and
   nothing else. DNS-over-HTTPS (default in Firefox, rolling out in Chrome), raw IPs, VPNs, and a
   second browser profile all route around it. Enforcement lives in **WFP filters** (user-mode
   `fwpuclnt.dll`, no kernel driver, via `github.com/tailscale/wf`) plus a **local DNS sink**, with
   `hosts` as a secondary layer only.

   ~~WFP additionally triggers ALE reauthorization on rule change, which forcibly terminates
   *already-open* connections — so committing to a session kills a mid-stream YouTube tab instead of
   letting it play out.~~ **This does not happen, and cannot with the design we chose.** ALE
   reauthorization only terminates connections that *match a filter*, and our filters match DNS
   (ports 53/853, DoH endpoints), never the blocked sites themselves — because per-domain IP blocking
   was rejected for shared CDN addresses. Measured in Chrome on 2026-08-05: a YouTube tab open before
   the daemon started kept serving for roughly two minutes, until Chrome's own host cache expired and
   it needed a fresh resolve. Cold domains (`reddit.com`, `music.youtube.com`) failed immediately.

   **This is the biggest gap between the thesis and the build.** The product is for the moment of
   craving, and at that moment the tab is already open. Closing it means blocking resolved IPs, with
   the shared-CDN damage that implies, or a browser extension that can close tabs (Phase 7).

2. **Monotonic clocks do not survive reboot.** `QueryPerformanceCounter` resets when the machine
   does, so it cannot carry a 2-hour lock across a restart alone. See TIME AUTHORITY.

3. **HMAC-signed state is tamper-*evident*, not tamper-*proof*.** The key lives on the same disk as
   the data. Sign anyway — casual `sqlite3` edits fail loudly and corruption becomes detectable — but
   never claim otherwise in code comments, the README, or an interview.

4. **Safe Mode survival via boot-critical driver registration is permanently out of scope.** A
   service registered under `SafeBoot\Minimal` that mishandles a failure produces an unbootable
   machine. Bricked laptop vs. ten fewer minutes of Reddit is a bad trade.

5. **The tamper penalty is capped and pre-disclosed.** +15 minutes, at most once per session, shown
   in the commit dialog before starting. Open-ended punishment turns a dead CMOS battery, a VM
   suspend, or a dual-boot into an unescapable lock on the user's own computer.

---

## THREAT MODEL

The adversary is **you, forty minutes from now, with depleted willpower and a specific urge.** That
person clicks around for a stop button, closes the window, kills the process, maybe googles a bypass,
and gives up in about ninety seconds because the urge outlasts the patience.

The adversary is **not** you with a debugger and a spare afternoon. That person always wins on a
machine they administer. Every hour spent fighting them is an hour not spent on friction that works.

| # | Attempt | Defense | Holds? |
|---|---|---|---|
| 1 | Look for an off switch during FOCUS | No stop verb exists in the API | Yes |
| 2 | Toggle the category off on the Blocking screen | 15-minute delay, blocks stay live | Yes |
| 3 | Close the window / kill the tray app | UI is a thin client; daemon owns everything | Yes |
| 4 | End Task on the daemon | `SYSTEM` service, needs UAC elevation | Mostly |
| 5 | Reboot | Auto-start + persisted target timestamp | Yes |
| 6 | Move the system clock forward | Slowest-credible-source rule | Yes |
| 7 | Edit the session blocklist mid-session | Not editable from the dial; weakening rejected | Yes |
| 8 | Different browser / incognito | Enforcement is at the network layer | Yes |
| 8b | **Keep using the tab that was already open** | None — DNS enforcement does not touch live connections | **No** (~1–2 min) |
| 9 | DNS-over-HTTPS to a public resolver | WFP filters on known DoH endpoints | Partly |
| 10 | Uninstall from an elevated prompt | **Intentionally allowed** | No, by design |
| 11 | VPN, phone, second machine, Safe Mode | Not defended | No |

Row 11 is not a failure. A commitment device raises the activation energy of a bad habit above that
of the thing you meant to do. That is the entire mechanism.

---

## NON-NEGOTIABLES

A PR violating one does not merge, regardless of what it improves.

* **Never fight the administrator.** Elevated uninstall always works, in every state, mid-session
  included, documented in the README, reachable in under two minutes. It removes WFP filters,
  firewall rules, `hosts` entries, and the service completely.
* **Never hide.** Real process name, service visible in `services.msc`, documented install path. No
  name spoofing, no rootkit techniques, no anti-debug, no AV evasion, no behaving differently under
  inspection. Those are malware techniques — they get the binary flagged by Defender and would be
  indefensible in a code review.
* **Watchdogs monitor, they do not resurrect indefinitely.** Cap respawns at 5/minute and stop
  entirely when the Service Control Manager was used to stop the service by an elevated user.
* **Permanent, non-removable allowlist.** Never blockable by any list, preset, schedule, or session,
  and not user-editable: emergency services info, local government, hospital and health-authority
  domains, crisis and helpline services, OS update and security endpoints, DNS root, localhost. A
  blocker standing between a person and a crisis line has failed catastrophically regardless of uptime.
* **The escape hatch always exists.** Delayed-cancel cannot be disabled by any setting, hardcore mode,
  or config flag, and maxes at 30 minutes. This is the line between a tool and a trap.
* **No blocking of system-critical or accessibility software** — screen readers, input devices, AV,
  Windows Update, the OS shell.

---

## TECH STACK

* **Daemon:** Go 1.22+, Windows Service under `LocalSystem`, single static binary. Platform specifics
  behind `enforce.Enforcer` so macOS/Linux are additive, not a rewrite.
* **WFP:** `github.com/tailscale/wf` — production-tested, saves ~1,500 lines of hand-rolled
  `unsafe.Pointer` struct marshalling for `FWPM_FILTER0` and friends.
* **Store:** SQLite (`modernc.org/sqlite`, cgo-free) at `%ProgramData%\Flow\state.db`, every
  mutable row HMAC-signed, key DPAPI-wrapped.
* **Local API:** HTTP on `127.0.0.1` only, bearer token, JSON.
* **UI:** Next.js (App Router, TypeScript strict, Tailwind, Lucide) static export, wrapped in
  **Wails v2**. Wails over Tauri because it's Go-native — no second toolchain for what amounts to a
  window. Two corrections to the original plan, both found by building it:
  * **Wails v2 has no system tray.** That was v1, and it returns in v3. "Tray + autostart" is not
    something v2 gives you; the always-on-top mini window covers the same need, and autostart would
    be a `Run` key we write ourselves.
  * **Wails v2 is single-window,** which is why mini mode is a *route* (`/mini`) that resizes and
    pins the one window, rather than a second floating widget. Multi-window needs v3 (alpha).
* **Process monitoring:** WMI `__InstanceCreationEvent` or a 1s process-list poll. Not ETW — Go's ETW
  story is weak and nobody launches Steam in under a second.

---

## ARCHITECTURE

```text
flow/
├── main.go               # Wails desktop shell — at the module root because
├── app.go                #   go:embed cannot reach up out of its own directory,
├── proxy.go              #   and the embedded assets live in ui/out
├── wails.json
├── build-release.ps1     # builds flowd+flowctl+app+NSIS installer for one arch
├── cmd/
│   ├── flowd/            # service: main, SCM integration, watchdog
│   └── flowctl/          # debug CLI (read-only in release builds)
├── internal/
│   ├── session/
│   │   ├── machine.go          # IDLE→ARMING→FOCUS→COMPLETE
│   │   ├── clock.go            # TIME AUTHORITY: multi-source elapsed
│   │   └── delay.go            # delayed-release primitive — used by BOTH surfaces
│   ├── baseline/
│   │   ├── rules.go            # indefinite on/off rules, per-rule disable timers
│   │   └── attribution.go      # which source owns which effective rule
│   ├── enforce/
│   │   ├── enforcer.go         # platform-agnostic interface; takes the UNION
│   │   ├── wfp_windows.go      # tailscale/wf — primary network layer
│   │   ├── dns_sink.go         # local resolver, NXDOMAIN for blocked zones
│   │   ├── hosts_windows.go    # secondary layer, best-effort
│   │   ├── procwatch_windows.go# process rules (games, launchers, delivery apps)
│   │   └── reconcile.go        # 3s loop: read actual rules, repair drift
│   ├── store/                  # db.go, sign.go, checkpoint.go
│   ├── api/                    # server.go (loopback + token), handlers.go
│   ├── blocklist/              # presets.go, allowlist.go (permanent)
│   └── schedule/cron.go        # wall-clock recurring locks
├── ui/
│   └── src/
│       ├── app/
│       │   ├── page.tsx        # Focus screen — the dial
│       │   ├── blocking/       # Blocking screen — baseline toggles
│       │   └── mini/           # the pinned sticky-note timer (desktop only)
│       ├── components/
│       │   ├── Dial.tsx        # ring, countdown, commit/arming tap target
│       │   ├── DurationChips.tsx
│       │   ├── BaselineRow.tsx # switch + pending-disable countdown
│       │   ├── ScheduleRow.tsx
│       │   └── CommitDialog.tsx
│       └── lib/                # flow-client.ts, use-poll.ts, state.ts (pure)
├── extension/                  # Phase 7
└── FLOW.md               # architecture notes below the API surface
```

**Ownership rule:** the daemon owns all authority; the UI owns zero. Delete the UI mid-session and
enforcement is unaffected. The UI never greys out a control because it decided to — only because
`canRelease: false` came back from the API.

---

## SESSION STATE MACHINE

```text
     ┌──────┐  commit   ┌────────┐  grace ends  ┌───────┐  ack   ┌──────────┐
     │ IDLE │──────────▶│ ARMING │─────────────▶│ FOCUS │───────▶│ COMPLETE │
     └──────┘           └────────┘              └───────┘        └──────────┘
        ▲                    │                      │                  │
        │  abort (free)      │          escape granted│                 │
        └────────────────────┘                      ▼                  │
                                              ┌───────────┐            │
                                              │ RELEASING │────────────┘
                                              └───────────┘
```

| State | Session rules | Baseline rules | Dial tap |
|---|---|---|---|
| IDLE | none | enforced | commits |
| ARMING | **enforced** | enforced | aborts (15s) |
| FOCUS | enforced | enforced | inert |
| RELEASING | still enforced | enforced | inert |
| COMPLETE | lifted | enforced | commits |

Two things easy to get wrong:

* **Blocks apply at commit, not at grace-end** (see "third state" above).
* **Time bank credits on COMPLETE only, exactly once,** in the same transaction as the transition.
  Aborted and escaped sessions earn nothing, or "start, escape immediately" becomes a minute farm.

### Modes

| Mode | Shape | Lock behaviour |
|---|---|---|
| Commitment | duration chip (25m/50m/2h/custom) | Locked until elapsed |
| Hard Pomodoro | N × (focus + break) | Locked during focus; breaks are IDLE-with-baseline; next interval auto-arms |
| Time Bank | 0.2 recreation min per 1 focus min | Spending opens the blocklist for the spent minutes, then hard re-locks. Cannot spend during FOCUS. Balance signed, never negative. |
| Scheduled | wall-clock recurrence | Auto-enters FOCUS at the boundary; lives on the **Blocking** screen |

Scheduled locks are the one place wall-clock is legitimately authoritative — "bedtime" means 23:00
local, not "seven hours of monotonic ticks." That makes them defeatable by changing timezone, so: pin
the TZ at creation, sign it, and treat a TZ change during an active scheduled lock as a tamper event
that holds until the original window would have closed in the pinned zone.

---

## TIME AUTHORITY

> **Elapsed advances at the rate of the slowest credible source. Remaining time is never shortened on
> the word of one unverified clock.**

Sources, in trust order:

1. **Monotonic within a boot** (`QueryUnbiasedInterruptTime`). Authoritative while up, immune to clock
   changes. Use the **biased** variant so sleep counts as elapsed — a sleeping laptop is not one
   you're doomscrolling on. Record both.
2. **Signed remote timestamp** at session start and on every reconnect. Store
   `(server_start, wall_start, monotonic_start, boot_id)` as one signed row. `server_now − server_start`
   is the ceiling on credited elapsed.
3. **Wall clock, across reboots only.** The 5s checkpoint gives `wall_at_shutdown`. Offline credit is
   `clamp(wall_now − wall_at_shutdown, 0, 4h)` per gap. Uncapped, "shut down, set BIOS clock to 2030,
   boot" ends any session.

```
credited_elapsed = min(
    monotonic_this_boot + credited_offline_from_previous_boots,
    server_elapsed            // only when a signed remote timestamp exists
)
remaining = max(0, target − credited_elapsed)
```

If `|wall_delta − monotonic_delta| > 5 min` within one boot: write a `clock_drift` event, keep using
monotonic, apply the capped +15m **only if penalties were accepted at commit.** Show the event either
way — the log is more useful than the punishment.

Persist the **target completion instant**, not a countdown. A countdown decremented in memory resets
when the process does.

---

## LOCAL API SPECIFICATION

`http://127.0.0.1:<port>`, loopback bind only, bearer token from
`%ProgramData%\Flow\token` (ACL: Administrators + interactive user).

The token being user-readable is deliberate and safe, because **no verb in this API ends a locked
session or instantly disables a baseline rule.** Authority is in the state machine, not the transport.
Anyone who can call the API can start a session, read state, and strengthen rules — nothing more.

### `GET /api/state` — everything both screens need in one poll
```json
{
  "session": {
    "state": "FOCUS",
    "mode": "pomodoro",
    "targetAt": "2026-08-04T14:52:11Z",
    "remainingSeconds": 1783,
    "canRelease": false,
    "graceRemainingSeconds": 0,
    "blocklistIds": ["preset.video", "preset.doomscroll"],
    "escape": { "requested": false, "availableAt": null },
    "cycle": { "index": 2, "of": 4, "phase": "focus" }
  },
  "baseline": [
    { "id": "preset.adult",     "enabled": true,  "pendingDisableAt": null },
    { "id": "preset.gambling",  "enabled": true,  "pendingDisableAt": null },
    { "id": "preset.doomscroll","enabled": false, "pendingDisableAt": null }
  ],
  "effective": {
    "blockedIds": ["preset.adult", "preset.gambling", "preset.video", "preset.doomscroll"],
    "attribution": { "preset.doomscroll": "session", "preset.adult": "baseline" }
  }
}
```
`canRelease` and `attribution` are computed daemon-side. The UI must never derive either — attribution
is what stops the Blocking screen from appearing to lift a permanent rule when a session ends.

### Session
* `POST /api/session` — `{mode, durationMinutes, blocklistIds, graceSeconds, acceptTamperPenalty}`.
  `409` if active, `400` if duration outside `[5, 480]`.
* `DELETE /api/session` — **valid only in ARMING.** `409 {"error":"locked"}` otherwise. This exists so
  the grace window works; it is not a stop button and must never become one.
* `POST /api/session/escape` — starts delayed cancel (15–30 min, never disableable), moves to
  RELEASING. Idempotent; a second call does not shorten the delay.
* `POST /api/session/escape/verify` — `{challengeId, typed}`. Only accepted at/after `availableAt`.
  Challenge generated daemon-side, case-sensitive. UI disables paste (friction, not security — the
  daemon cannot know how the characters arrived).

### Baseline
* `GET /api/baseline`
* `POST /api/baseline/{id}/enable` — **immediate**, always allowed, cancels any pending disable.
* `POST /api/baseline/{id}/disable` — starts the 15-minute delay, returns `pendingDisableAt`. Rule
  stays fully enforced throughout. `409 {"error":"would_weaken"}` if a session is active.
* `DELETE /api/baseline/{id}/disable` — cancels a pending disable, immediate and free.

### Lists, schedules, bank, events
* `GET|POST /api/blocklists`, `DELETE /api/blocklists/{domain}` — the user's own sites, for the
  things no preset covers. Mutations are evaluated for **direction**: adding strengthens and is
  always allowed, mid-session included; removing weakens and returns `409 would_weaken`.
  * **The id in the DELETE path is a domain, not a list id.** There is exactly one user list, so
    addressing the list would leave no way to name the entry being removed.
  * `POST` takes `{"domains": [...]}` and accepts whole URLs, because the address bar is where the
    decision to block something gets made. Everything after the host is discarded and a leading
    `www.` is stripped — matching is by label-boundary suffix, so one entry covers the subdomains.
    A path is *not* silently honoured: HTTPS means the network layer sees `reddit.com` and never
    `/r/all`, and accepting a path while blocking the whole domain would leave the user believing
    they had scoped something they had not.
  * A bad entry rejects the **whole batch**. "3 of 5 added" on a screen whose job is telling you
    what is enforced is worse than refusing.
  * The permanent allowlist is refused with `allowlisted` rather than dropped. `Resolve` strips it
    anyway, so a silent accept would leave the entry sitting in the list looking enforced.
  * **Removing is refused while the list is enforced**, and the way out is the ordinary one: turn the
    Custom sites row off, which takes the usual fifteen minutes, then edit. This needs no new
    machinery — the list reaches the union as one list carried by a baseline rule, so it inherits
    that rule's attribution and its delay. Checked on the rule rather than the effective set, so a
    bank spend is not a window you can edit through.
* `GET|POST /api/schedules`
* `GET /api/bank`, `POST /api/bank/spend` — requires IDLE; a spend is itself a small locked window that
  hard re-locks at expiry, and cannot be cancelled to bank the remainder.
* `GET /api/events?since=` — tamper log, transitions, reconciliation repairs
* `GET /api/health` — uptime, per-layer enforcer status, last reconcile, signature status

---

## VISUAL DESIGN

Binding for Phases 3 and 4. Numbers here are the spec, not suggestions — the whole point of writing
them down is that nobody re-litigates padding at 1am.

### Why it looks like this

* **One control per screen.** Focus is a dial; Blocking is a list. Neither screen has two competing
  primary actions.
* **The countdown is the only large text in the app.** Everything else is 11–14px. The hierarchy is
  severe on purpose — there is exactly one number you care about.
* **Color appears exactly twice:** the progress arc and on-switches. Both mean the same thing,
  "enforcement is live." Nothing else is tinted, so color carries signal instead of decoration.
* **Consequence text sits adjacent to its cause.** People read the label nearest the thing they're
  about to click and nothing else.
* **No streaks, no scores, no charts.** The app competes with YouTube for attention and loses that
  fight, so it shouldn't enter it.

### Shell

Text-only tabs in the top-left of each screen — "Focus" and "Blocking", 13px. Active tab is weight 500
with a 2px bottom border in primary text color; inactive is muted with no border. No sidebar, no icons,
no logo, no window chrome beyond that. Two words is the entire navigation.

Each screen sits in a card: elevated surface background, 0.5px border, 12px radius, ~20px padding.
Flat — no gradients, no shadows.

### Focus screen

The dial is a 180×180 SVG, centered, and the only interactive element on the screen.

* **Track:** circle at `r=76`, `stroke-width 8`, hairline border color.
* **Progress arc:** same radius and width, accent color, `stroke-linecap: round`, rotated −90° so it
  starts at twelve o'clock. Circumference `2πr ≈ 477.5`, so `stroke-dasharray = [477.5 × fraction] [477.5]`.
* **Center stack, three lines:** 30px monospace countdown, 12px secondary status word, 11px muted hint.

The monospace on the countdown is not decoration. Proportional digits change width as they tick and the
whole number jitters left-right once a second, which you notice in your peripheral vision for an hour.
Mono fixes it.

**Duration chips** below the dial: pill row, 12px text, 5px×11px padding, fully rounded. Unselected
chips get a 0.5px border on transparent; the selected chip drops the border and takes an accent-tint
background with accent text.

**Footer strip:** hairline divider, then 12px muted text with a lock icon — "no off switch until 0:00".
Directly under the control that causes it.

#### Dial states

| State | Arc | Countdown | Status (12px) | Hint (11px) | Chips |
|---|---|---|---|---|---|
| IDLE | empty | chosen duration, `50:00` | "not focusing" | "tap to commit" | enabled |
| ARMING | animating, live | live countdown | "starting in 12s" | **"tap to cancel"** | disabled |
| FOCUS | proportional to elapsed | live countdown | "locked in" | **absent**, or "no way to stop this" | disabled |
| COMPLETE | full, neutral tone | `00:00` | "done" | — | enabled |

Two traps:

* **The ARMING hint is the only thing telling the user a second tap works.** It cannot be subtle.
* **FOCUS must not inherit the IDLE hint.** A "tap to commit" line under "locked in" contradicts the
  state directly above it and invites tapping at the one moment the tap is inert.

IDLE needs a **fourth line** the other states don't: baseline coverage, e.g. "3 blocks always on".
Without it the screen implies nothing is protected while gambling and adult content are still enforced.

### Blocking screen

Same card, same tab row, then a 12px secondary caption before anything else:
**"Always on, no timer. Off takes 15 minutes."** The rule is stated before the user touches a switch,
not discovered afterward.

**Rows:** flex, space-between, 10px vertical padding, hairline divider between rows and none after the
last. Left: 14px label. Right: 34×20 pill switch with a 16px knob inset 2px. On = accent fill, knob
right. Off = neutral fill, knob left.

#### Row states

| State | Label | Right side |
|---|---|---|
| On | 14px primary | switch on, accent fill |
| Off | 14px primary | switch off, neutral fill |
| Pending-disable | 14px primary | "unblocks in 14:32 · cancel" in warning text, **switch still rendered on** but visually distinct from a settled on-state |
| Session-owned | muted, "Video — in this session" | clock icon + "48m". **No switch.** |

The pending-disable switch must not read as off, because it isn't — the rule is fully enforced for the
whole countdown.

The session-owned row is attribution made visible: a session rule appears in the list so you can see
it's enforced, but it has no switch because you don't control it right now.

---

## IMPLEMENTATION PHASES

### PHASE 0 — DAEMON, SERVICE INSTALL, SIGNED STORE
**Exit:** installs as a Windows Service, auto-starts on boot, serves `GET /api/health` on loopback
with token auth, writes an HMAC-signed row that `flowctl verify` confirms and rejects after a
manual `sqlite3` edit.
* SCM integration (`x/sys/windows/svc`), install/uninstall, boot auto-start.
* Store, migrations, DPAPI-wrapped key, `sign.go` round-trip tests.
* Loopback API, token file ACL, `/api/health`.
* **Uninstaller now, not at the end** — test: install, uninstall, assert zero residue.

### PHASE 1 — ENFORCEMENT CORE & RECONCILIATION
**Exit:** with a hardcoded list and no session logic, `youtube.com` fails to resolve and connect in
Chrome, Firefox, and Edge; an already-open stream is terminated on rule application; manually deleting
`hosts` entries and firewall rules self-repairs within 5 seconds; the permanent allowlist is
unreachable by any list edit.
* `enforce.Enforcer` interface taking a **union** of rule sets with attribution.
* `tailscale/wf` filters — primary layer, dynamic session so rules die with the process.
* DNS sink; force the resolver via WFP so public DoH fails closed.
* `hosts` as secondary. No `LockFileEx` theatre — write, then reconcile.
* Process rules via WMI events.
* `reconcile.go`, 3s, diffs intended vs. actual and repairs, logging every repair. **This loop is the
  actual anti-tamper mechanism**; file locking was never going to be.

### PHASE 2 — SESSION STATE MACHINE & TIME AUTHORITY
**Exit:** a 10-minute session survives (a) closing the UI, (b) `taskkill /F` on the tray app, (c) a
reboot, (d) the clock moved forward 2 hours, and (e) a 3-minute shutdown — ending within ±5s of the
true target in every case except (e), which ends 3 minutes early by design.
* `machine.go`, five states, every transition a signed write.
* `clock.go` with the slowest-credible-source rule. **Unit-test with an injectable clock** — the one
  component where a subtle bug silently voids the entire product.
* `delay.go` — the generic delayed-release primitive. Written here, consumed by Phase 3 and Phase 5.
* 5s checkpointing, boot recovery, watchdog with respawn cap and SCM-shutdown stop.

### PHASE 3 — UI SHELL + BLOCKING SCREEN
**Exit:** two-tab shell; the Blocking screen lists categories with switches; enabling blocks
immediately; disabling shows a live "unblocks in 14:32 · cancel" countdown while the site stays
blocked; cancelling is instant; the countdown survives an app restart.
* `lib/flow-client.ts` + `use-poll.ts`, ported from Parallax.
* `lib/state.ts` — pure derivations (countdown strings, progress fraction). Unit tested. **No
  component computes time.**
* Shell: two 13px text tabs top-left, card container. Spec in **VISUAL DESIGN → Shell**.
* `BaselineRow` with the four visual states: on, off, pending-disable, session-owned. Spec in
  **VISUAL DESIGN → Blocking screen**. The pending-disable switch renders **on**, not off.
* The "Always on, no timer. Off takes 15 minutes." caption ships in this phase, not later — it's the
  only place the asymmetry is explained.
* Schedule rows on this screen.
* This phase before the dial deliberately: it exercises the enforcer end-to-end without depending on
  the state machine, so a bug is unambiguously in one layer or the other.

### PHASE 4 — FOCUS SCREEN: THE DIAL
**Exit:** tap commits, the ring sweeps through a 15-second arming window with a working second-tap
abort, blocks are live from the first tap, and after arming the tap is inert and the countdown is
derived from `targetAt` rather than a local `setInterval` accumulator.
* `Dial.tsx` — 180×180 SVG, `r=76`, four visual states, one tap target. Geometry and the per-state
  text table are in **VISUAL DESIGN → Focus screen**.
* Countdown in monospace. Non-negotiable: proportional digits jitter the number once a second.
* IDLE carries a baseline-coverage line ("3 blocks always on"); FOCUS drops the hint line entirely.
* `DurationChips` disabled in ARMING and FOCUS.
* `CommitDialog` states plainly: duration, what gets blocked, that there is no off switch, how long
  escape takes, whether a tamper penalty is accepted.
* Read-only session-coverage summary. **No blocklist editing on this screen.**
* Design: muted, one screen, large countdown, no gamification, no streak guilt. The app is competing
  with YouTube for attention and will lose, so it should not try.

### PHASE 5 — ESCAPE HATCHES & TAMPER LOG
**Exit:** escape request → 15-minute countdown with blocks fully active → 100-character typed
challenge → release; history shows every drift event, repair, and escape.
* Reuses `delay.go` from Phase 2.
* Non-judgmental copy: "Session ended early", not "You failed".

### PHASE 6 — TIME BANK & SCHEDULED HARD-LOCKS
**Exit:** 50 locked minutes credits exactly 10.0 recreation minutes once, verified across a mid-session
reboot; an 18:00–21:00 schedule auto-arms even if the UI was never opened; a TZ change mid-lock does
not end it early.

### PHASE 7 — BROWSER EXTENSION & INSTALLER
**Exit:** `youtube.com/watch` blocked while an allowlisted channel URL is reachable; signed installer
sets up service, autostart, and extension in one pass.
* **Why this phase exists:** HTTPS means the network layer sees `youtube.com`, never `/watch` vs
  `/@channel`. Domain-level blocking is all you get without an extension or a MITM cert, and
  installing a MITM root CA to enforce a focus timer is not a trade worth making.
* The extension is **additive**. Uninstall it and domain-level blocking still holds.

---

## PRESETS

| Preset | Default home | Notes |
|---|---|---|
| `preset.adult` | Baseline, on | |
| `preset.gambling` | Baseline, on | |
| `preset.bedtime` | Baseline, scheduled 23:00–07:00 | Everything except allowlist |
| `preset.delivery` | Baseline, scheduled 18:00–21:00 | |
| `preset.shopping` | Baseline, off | |
| `preset.doomscroll` | Either | X, Reddit, Instagram, aggregators |
| `preset.video` | Session | YouTube, Twitch, Netflix, TikTok |
| `preset.gaming` | Session | Steam, Epic, Riot, Battle.net — process rules |
| `preset.work` | Session | video ∪ doomscroll ∪ gaming |
| `preset.study` | Session | `work` minus a user allowlist for course sites |
| `preset.offline` | Session | Everything except allowlist |

Presets are seed data, not code. A user edit forks the preset into a custom list.

**`custom.blocked` is the user's own list**, and it is not a fourth rule source — it is content
carried by a baseline rule, so the union, the attribution, and the 15-minute disable delay all apply
to it unchanged. See `GET|POST /api/blocklists` above. It exists because the presets will never
cover everything, and a blocker you cannot point at your own problem site is one you stop using.

---

## RUNNING BOTH HALVES

```powershell
# Terminal 1 — daemon, dev mode (console, enforcement DRY-RUN unless elevated)
go run ./cmd/flowd -dev -port 8787

# Terminal 2 — UI. The token is in %ProgramData%\Flow\token (or $env:FLOW_DATA_DIR\token).
cd ui
$env:NEXT_PUBLIC_FLOW_URL   = "http://127.0.0.1:8787"
$env:NEXT_PUBLIC_FLOW_TOKEN = (Get-Content "$env:ProgramData\Flow\token").Trim()
npm run dev                 # http://localhost:3000

# Inspect a running daemon (read-only)
go run ./cmd/flowctl health
go run ./cmd/flowctl verify

# Install as a real service (ELEVATED PROMPT REQUIRED — SCM access is denied otherwise)
go build -o flowd.exe ./cmd/flowd
.\flowd.exe install
.\flowd.exe uninstall
```

`FLOW_DATA_DIR` overrides `%ProgramData%\Flow`. Use it to run a throwaway instance without touching
the real one.

`-dev` logs every enforcement action instead of applying it. **Use it.** The first person this app
traps will be the person writing it.

---

## DEFERRED

* **URL-path granularity doesn't exist before Phase 7, so `preset.study` blocks all of YouTube
  including lecture content.** Most consequential gap: the preset most likely to be used daily is the
  one most damaged by domain-only blocking, and the workaround — turning it off — is exactly the
  behaviour the app exists to prevent.
* Baseline disable delay is a single global constant (15 min). Per-category delays ("adult content
  takes 24 hours to disable, shopping takes 15 minutes") are the obvious next step and are not built.
* Safe Mode undefended, deliberately.
* No mobile enforcement. The phone in your pocket bypasses everything here and no desktop app can
  close that. Say so in the README rather than letting users discover it and conclude it's broken.
* VPNs and tethering undetected. WFP could block known VPN client processes; it would also break work
  VPNs, so it's off until there's a per-profile answer.
* macOS/Linux enforcers are interface stubs (`pf` + LaunchDaemon, `nftables` + systemd).
* No history charts. Every number is an instantaneous read; sessions are a flat event log.
* HMAC key is DPAPI-wrapped to the machine, not a hardware root. TPM sealing would raise the bar and
  also brick sessions on hardware change. Not yet.
* **A tab that is already open survives the commit.** DNS-layer enforcement only bites when something
  needs to resolve a name, so an established connection plus a warm browser DNS cache buys roughly
  one to two minutes of the thing you just committed to avoid. Measured in Chrome. This is the most
  consequential gap in the product, because the moment of craving is exactly when the tab is already
  open. Not fixable without IP blocking or the Phase 7 extension.
* **WFP does DNS containment, not per-domain IP blocking.** Resolving blocked domains to addresses
  and filtering those was considered and rejected: CDN addresses are shared, so blocking one takes
  out unrelated sites, and they rotate faster than a 3s reconcile. A user who types a raw IP, or
  whose browser has a cached address, is not stopped by the network layer.
* Reconciliation is a fixed 3s poll, not event-driven — a ~3s window after deleting a rule. Closing it
  needs WFP callouts or `NotifyRouteChange` subscriptions.
* No rate limit on session starts; ARMING/abort churn writes a lot of signed rows.