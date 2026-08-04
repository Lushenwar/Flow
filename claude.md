# CLAUDE.md — Mast (Commitment Blocker + Focus Timer, Desktop)

> **Codename resolved at Phase 0: `Mast`.** Odysseus tied himself to it — chosen restraint, not
> imprisonment, which is the product thesis in one word. `Portcullis` was backwards (it keeps
> invaders out; this app keeps you in) and `Rubicon` is one-way, which the escape hatch is not.
> The name is baked into `paths.AppName`, the service registration, `%ProgramData%\Mast`, and the
> binaries `mastd` / `mastctl`.

## WORKFLOW: BRANCH + PR ONLY

No direct commits to `main`. Every change goes: `git checkout -b <branch>` → commit → `gh pr create`.
A pre-commit hook (`.git/hooks/pre-commit`) enforces this locally by rejecting commits made while on `main`.

## CURRENT STATUS

╔══════════════════════════════════════════════════════════╗
║  MAST BUILD PROGRESS                      1/8 DONE ║
║  ███░░░░░░░░░░░░░░░░░░░░░░░░░  PHASE 0 COMPLETE          ║
║  Phase 0: Daemon, Service Install, Signed Store  [DONE]  ║
║  Phase 1: Enforcement Core & Reconciliation      [    ]  ║
║  Phase 2: Session State Machine & Time Authority [    ]  ║
║  Phase 3: UI Shell + Blocking Screen (baseline)  [    ]  ║
║  Phase 4: Focus Screen — the Dial & Grace Window [    ]  ║
║  Phase 5: Escape Hatches & Tamper Event Log      [    ]  ║
║  Phase 6: Time Bank & Scheduled Hard-Locks       [    ]  ║
║  Phase 7: Browser Extension & Installer          [    ]  ║
╚══════════════════════════════════════════════════════════╝

Phase: 1 (next).
Status: Daemon runs, signs its state, and serves loopback health. Nothing enforces anything yet —
the enforcer layers all report `not built` in `/api/health`, which is deliberate: a green light for
a layer that does not exist is worse than no light.
Update this as you finish each step.

**Checks:** `go test ./... && go vet ./... && cd ui && npm test && npm run typecheck && npm run lint && npm run build`

---

## WHAT THIS FILE IS

The authoritative guide for building Mast: a desktop commitment device combining a hard-locked
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
   `hosts` as a secondary layer only. WFP additionally triggers ALE reauthorization on rule change,
   which forcibly terminates *already-open* connections — so committing to a session kills a
   mid-stream YouTube tab instead of letting it play out. `hosts` cannot do that.

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
* **Store:** SQLite (`modernc.org/sqlite`, cgo-free) at `%ProgramData%\Mast\state.db`, every
  mutable row HMAC-signed, key DPAPI-wrapped.
* **Local API:** HTTP on `127.0.0.1` only, bearer token, JSON.
* **UI:** Next.js (App Router, TypeScript strict, Tailwind, Lucide) static export, wrapped in
  **Wails** for tray + autostart. Wails over Tauri because it's Go-native — no second toolchain for
  what amounts to a tray icon and a window.
* **Process monitoring:** WMI `__InstanceCreationEvent` or a 1s process-list poll. Not ETW — Go's ETW
  story is weak and nobody launches Steam in under a second.

---

## ARCHITECTURE

```text
mast/
├── cmd/
│   ├── mastd/            # service: main, SCM integration, watchdog
│   └── mastctl/          # debug CLI (read-only in release builds)
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
│       │   └── blocking/       # Blocking screen — baseline toggles
│       ├── components/
│       │   ├── Dial.tsx        # ring, countdown, commit/arming tap target
│       │   ├── DurationChips.tsx
│       │   ├── BaselineRow.tsx # switch + pending-disable countdown
│       │   ├── ScheduleRow.tsx
│       │   └── CommitDialog.tsx
│       └── lib/                # mast-client.ts, use-poll.ts, state.ts (pure)
├── extension/                  # Phase 7
└── MAST.md               # architecture notes below the API surface
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
`%ProgramData%\Mast\token` (ACL: Administrators + interactive user).

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
* `GET|POST /api/blocklists`, `PUT|DELETE /api/blocklists/{id}` — mutations evaluated for **direction**
  while anything is active: adding is allowed, removing returns `409 would_weaken`.
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
with token auth, writes an HMAC-signed row that `mastctl verify` confirms and rejects after a
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
* `lib/mast-client.ts` + `use-poll.ts`, ported from Parallax.
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

---

## RUNNING BOTH HALVES

```powershell
# Terminal 1 — daemon, dev mode (console, enforcement DRY-RUN unless elevated)
go run ./cmd/mastd -dev -port 8787

# Terminal 2 — UI
cd ui; npm run dev          # http://localhost:3000

# Inspect a running daemon (read-only)
go run ./cmd/mastctl health
go run ./cmd/mastctl verify

# Install as a real service (ELEVATED PROMPT REQUIRED — SCM access is denied otherwise)
go build -o mastd.exe ./cmd/mastd
.\mastd.exe install
.\mastd.exe uninstall
```

`MAST_DATA_DIR` overrides `%ProgramData%\Mast`. Use it to run a throwaway instance without touching
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
* Reconciliation is a fixed 3s poll, not event-driven — a ~3s window after deleting a rule. Closing it
  needs WFP callouts or `NotifyRouteChange` subscriptions.
* No rate limit on session starts; ARMING/abort churn writes a lot of signed rows.