# CLAUDEV2.md — Flow, the road from "built" to "shipped"

> **This file does not replace `claude.md`. It continues it.**
> `claude.md` is the binding spec for what Flow *is*: the two surfaces, the union, the threat model,
> the non-negotiables. Every one of those still holds and none of them are re-litigated here. Where
> this file and `claude.md` disagree about the product, `claude.md` wins. Where they disagree about
> *what is left to do*, this file wins, because `claude.md`'s status box is a record of what was
> built and this is a plan for what is not.
>
> The one-line thesis is unchanged: **the app's value is that you cannot turn it off.** Everything
> below is either a defect against that thesis, a gap between the spec and the code, or the work of
> turning a thing that runs on the author's machine into a thing a stranger can install.

---

## THE HONEST STATE OF IT

Eight phases are built. Two are `[~]`. That undersells the gap, because "built" and "shipped" are
different words and this project has only done the first one. Specifically:

* **Nobody who is not the author has ever run this.** There is no release, no CI, and the installer
  has been *built* but never *executed* — the service-registration and teardown paths inside
  `project.nsi` are unexercised code that runs elevated on a stranger's machine.
* **Four features in the spec do not exist in the code.** Hard Pomodoro, spending the time bank from
  the UI, creating or editing a schedule, and choosing what a session covers. Three of them have
  daemon support already and are missing only a surface; one (Pomodoro) has a `Mode` constant, two
  struct fields, and no implementation at all — `POST /api/session` accepts `mode: "pomodoro"` today
  and silently runs a commitment session.
* **Seven defects were found by reading the code end to end.** They are listed first because a
  roadmap that adds features on top of a daemon that logs a repair event every three seconds is a
  roadmap written by someone who did not read the daemon.

**Marked honestly throughout:** items tagged `[read]` were found by reading the source and are
reasoned from the code, not observed on a running machine. Items tagged `[measured]` come from the
verification runs already recorded in `claude.md`. Nothing here has been reproduced on hardware in
this pass — the fixes need a run to confirm, and the plan says so rather than implying otherwise.

> **Correction, made while starting the work.** The first draft of this file was written against the
> `wails-desktop` branch, which was two merged PRs behind `main`. Items 13 (DoH bootstrap hostnames)
> and 16 (Firefox port) were **already built** on `main` and are struck from the queue below. `main`'s
> DoH fix is also better than the one proposed here: it leads with `use-application-dns.net`,
> Mozilla's documented canary, so Firefox disables DoH *itself* rather than being fought endpoint by
> endpoint. Item 10 (signing) is likewise half-done — `sign.ps1` exists and only the certificate is
> missing. Reading the branch you are on is not the same as reading the project.

```text
╔══════════════════════════════════════════════════════════════════════╗
║  FLOW V2 — FROM BUILT TO SHIPPED                          0/7 DONE   ║
║  ░░░░░░░░░░░░░░░░░░░░░░░░░░░░                                        ║
║  V0: Correctness debts — seven defects, all cheap        [    ]      ║
║  V1: Ship it — run the installer, sign it, CI, release   [    ]      ║
║  V2: Close the enforcement gaps — DoH, Firefox, warm tabs[    ]      ║
║  V3: Finish the spec'd product — the four missing screens[    ]      ║
║  V4: Durability — the event log grows without bound      [    ]      ║
║  V5: Trust — a11y, focus traps, UI tests, rules leak     [    ]      ║
║  V6: Platform — macOS/Linux, Wails v3, the tray          [    ]      ║
╚══════════════════════════════════════════════════════════════════════╝
```

---

## THE QUEUE

One ordered list, most urgent first. Everything below this section is detail on an item in it. If
there is ever a question about what to pick up next, it is the lowest-numbered unfinished line.

| # | Item | Phase | Cost | Why it is here |
|---|---|---|---|---|
| 1 | DNS sink never stops when the rule set empties | V0 | ~10 lines | Logs a false repair every 3s, forever |
| 2 | Bank spend unblocks adult and gambling too | V0 | ~6 lines | A protective baseline is not recreation |
| 3 | Timezone tamper event is dead on Windows | V0 | ~15 lines | The detection for the attack it was written for never fires |
| 4 | Extension hardcodes port 8787; daemon may not use it | V0 | ~20 lines | Silent total loss of extension enforcement |
| 5 | `GET /api/events` is unbounded and polled every 5s | V0 | ~25 lines | The whole log, HMAC-verified, every five seconds |
| 6 | `mode: "pomodoro"` is accepted and ignored | V0 | ~5 lines | The API lies about what it did |
| 7 | Cascaded transitions log only the final state | V0 | ~8 lines | The tamper log loses the middle of a reboot recovery |
| 8 | Run the installer on a clean machine | V1 | half a day | Unexercised elevated code |
| 9 | CI: the check line on every push | V1 | ~60 lines YAML | Nothing but a human runs the tests |
| 10 | Authenticode signing, or an honest substitute | V1 | cost/blocked | Every user gets a SmartScreen warning |
| 11 | Publish a release with both architectures | V1 | ~1 hour | "Build it yourself" is not a distribution |
| 12 | Window autostart (the daemon already autostarts) | V1 | ~30 lines | The timer you cannot see is a timer you forget |
| ~~13~~ | ~~NXDOMAIN the DoH bootstrap hostnames~~ | V2 | — | **Already done on `main`** — see note below |
| 14 | Verify Firefox and Edge | V2 | ~2 hours | Ported but never loaded and driven |
| 15 | Verify reboot survival on real hardware | V2 | ~1 hour | Harness exists; needs a machine |
| ~~16~~ | ~~Port the extension to Firefox and Edge~~ | V2 | — | **Already done on `main`** — `browser.js` |
| 17 | Hard Pomodoro | V3 | ~2 days | A mode in the spec with no implementation |
| 18 | Spend the bank from the UI | V3 | ~half a day | Earnable, not spendable |
| 19 | Create and edit schedules | V3 | ~1 day | Only the two seeded rows can be toggled |
| 20 | Session composition sub-screen | V3 | ~1 day | `SESSION_LISTS` is a hardcoded const |
| 21 | Default-deny for `bedtime` and `offline` | V3 | ~1 day | They claim "everything" and mean "eleven lists" |
| 22 | Event log retention and pruning | V4 | ~half a day | `state.db` grows forever |
| 23 | Health's `Verify()` cost scales with history | V4 | ~2 hours | O(n) HMAC on a route meant to be cheap |
| 24 | Rate-limit ARMING/abort churn | V4 | ~2 hours | Every tap is a signed write |
| 25 | `/api/rules` leaks the blocklist to any web page | V5 | ~20 lines | A site can fingerprint what you block |
| 26 | Modals: focus trap, Escape, `role="dialog"` | V5 | ~half a day | Two dialogs, neither reachable by keyboard alone |
| 27 | Component tests for the UI | V5 | ~1 day | Only `lib/state.ts` is tested |
| 28 | An end-to-end daemon test | V5 | ~1 day | Every layer is tested; the stack is not |
| 29 | Signed remote time source | V5 | ~half a day | `Anchor.ServerStart` is always nil |
| 30 | macOS and Linux enforcers | V6 | ~2 weeks | Interface stubs today |
| 31 | Wails v3: real tray, real multi-window | V6 | blocked | v3 is alpha |

---

# PHASE V0 — CORRECTNESS DEBTS

**Why first:** every one of these is under thirty lines, and four of them are wrong in the direction
that matters — they either weaken enforcement or fill the log that proves enforcement is working.
Building V1 on top of them means shipping them to strangers.

**Exit:** all seven fixed, each with a test that fails without the fix, and `go test . ./cmd/...
./internal/...` green.

---

### 1. The DNS sink never stops when the rule set empties `[read]`

`internal/enforce/dns_sink.go:86` returns early from `Apply` when there is nothing to block, without
closing the socket or unpinning the resolver. `dns_sink.go:105` reports drift whenever the set is
empty *and* the sink is running. Reconcile therefore does this, every three seconds, forever:

```text
Drifted(empty) → true → Apply(empty) → returns nil, changes nothing → repaired++ → event
```

Every tick writes a `reconcile_repaired` row claiming a repair that did not happen. WFP does not
have this bug — `wfp_windows.go:63` calls `Clear()` on an empty set and self-corrects on the next
tick — and neither does hosts. Only the sink loops.

**When it fires in practice:** any state where the effective set is empty and the sink is up. Turn
off every baseline row, or — far more likely — **spend the time bank**, which returns `nil` rules by
design (`manager.go:315`). A ten-minute recreation window writes roughly two hundred false repair
events, and "Recent activity" on the Blocking screen fills with "Blocking rules repaired" at the one
moment nothing is being enforced.

**Fix.** Make `Apply` symmetric with `Drifted`: an empty set means stop.

```go
if len(want) == 0 {
    if running {
        return d.Stop()  // releases the port AND unpins the resolver
    }
    return nil
}
```

The ordering already works — `Stop()` unpins from the on-disk backup before closing the socket, so a
machine never ends up pointed at a resolver that is not listening.

**Test:** apply a non-empty set, then an empty one, then assert `Drifted(empty) == false` and that
the port is free. `TestSinkStopReleasesThePort` is the shape to copy.

---

### 2. A bank spend unblocks adult content and gambling `[read]`

`manager.go:314`:

```go
func (m *Manager) rulesLocked() []enforce.Rule {
    if m.bank.Spending(m.clock, nil) {
        return nil          // ← every rule, from every source
    }
```

Spending recreation time drops the *entire* union, baseline included. `claude.md` is explicit that
this is wrong in spirit even though the spend mechanism itself is right:

> **Baseline is not "off".** A user in IDLE with gambling and adult content on baseline is being
> protected right now.

Earning ten minutes by focusing should open the things you are avoiding *for productivity*, not the
things you asked to be permanently protected from. The two are different categories of commitment
and collapsing them makes the bank a supported path to the one outcome the app exists to prevent.

**Fix.** A spend suppresses `Session` and `Schedule` attribution only. Baseline survives.

```go
spending := m.bank.Spending(m.clock, nil)
var rules []enforce.Rule
for _, id := range m.baseline.EnabledIDs() {
    rules = append(rules, enforce.Rule{ListID: id, Source: enforce.Baseline})
}
if spending {
    return rules   // baseline holds; sessions and schedules are what you bought
}
```

Note this also removes the empty-set case that triggers defect 1 in the common configuration, but
fix both — a user with no baseline rules at all still exists.

**Open question for the user, decided in favour of the stricter reading:** an argument exists that
a spend should open everything because the user explicitly earned and paid for it, and that
splitting the two is paternalism. It loses to the sentence quoted above, which is already binding
spec. If the intent was the looser reading, say so and this becomes a one-line revert — but it needs
to be a decision, not a default.

---

### 3. The timezone tamper event can never fire on Windows `[read]`

`manager.go:653`:

```go
system := time.Local.String()          // "Local" on Windows, always
for _, s := range m.schedules.Active(now) {
    if s.TZ != system { ... }          // s.TZ is also "Local"
}
```

`schedule.New` is called with `time.Local` from both `Defaults()` and `putSchedule`, so `s.TZ` is
the literal string `"Local"`. So is `system`. They are always equal and the branch is dead.

This is the *same* trap `claude.md` already documents catching once — the fix at the time was to
capture `OffsetSeconds` and use it for evaluation, which was correct and does hold the window. But
the detection event was left comparing names, so the enforcement works and the log that proves it is
silent. The user changes timezone, the lock correctly does not move, and nothing tells them why.

**Fix.** Compare offsets, which is what the pinning actually uses.

```go
_, systemOffset := now.Zone()
for _, s := range m.schedules.Active(now) {
    if s.OffsetSeconds != systemOffset { ...log... }
}
```

**Test:** build a schedule with `OffsetSeconds: -5*3600`, evaluate at an instant where the machine
reports UTC, assert a `schedule_timezone_changed` event exists. The current test suite has
`TestPinnedOffsetHoldsTheWindowWhenTheMachineMoves` for the enforcement half and nothing for the log
half, which is exactly why this survived.

---

### 4. The extension hardcodes 8787; the daemon does not promise it `[read]`

`extension/background.js:20` is `http://127.0.0.1:8787/api/rules`. `internal/api/server.go:182`:

```go
ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
if err != nil {
    if ln, err = net.Listen("tcp", "127.0.0.1:0"); err != nil {   // ← ephemeral fallback
```

If 8787 is taken — another dev server, a second daemon, a stale socket — the daemon quietly takes a
random port, writes it to `%ProgramData%\Flow\port`, and keeps working. `proxy.go:50` reads that
file and is fine. `flowctl` reads it and is fine. **The extension cannot read files and has no way to
find out.** It polls 8787, gets a connection refused, and by its own deliberate design does nothing
rather than fail closed. URL-path granularity and warm-tab closing both silently stop, and the daemon
logs nothing because from its side nothing is wrong.

This is the same shape as the MV3 deadlock already recorded in `claude.md`: *the obvious health
signal is the one thing still working.*

**Fix, two halves, both cheap:**

1. **Make the fallback loud.** An ephemeral port is a degraded state, not a success. Write a
   `port_fallback` event and log a warning naming the requested and actual ports. Silent degradation
   is what made this invisible.
2. **Give the extension a way to find the daemon.** Scan a small fixed range on failure —
   `8787, 8788, 8789` — and have `Listen` prefer those in order before going ephemeral. That keeps
   the extension's search space to three probes and removes the ephemeral case from the common path
   entirely.

**Rejected alternative:** a native messaging host so the extension can read the port file. It is the
"correct" answer and it costs a registry key, a manifest, a second binary, and a per-browser
install path — for a problem three sequential ports solve.

**Test:** occupy 8787, start the daemon, assert it landed on 8788 and that the event exists.

---

### 5. `GET /api/events` is unbounded, and the UI polls it every five seconds `[read]`

`History.tsx:16` calls `api.events()` — no `since` — on a five-second interval.
`flow-client.ts:94` defaults `since = 0`. `server.go:160` passes that to `store.Events(0)`, which
selects **every row ever written** and verifies an HMAC on each one before returning it. The client
then reverses the array and renders the first eight (`state.ts:143`, `History.tsx:25`).

So: the whole history, over HTTP, HMAC-verified row by row, twelve times a minute, to draw eight
lines. Every signed transition, every reconcile repair, and — until defect 1 is fixed — every false
repair, forever. A month of ordinary use is tens of thousands of rows.

**Fix:**

* Add `limit` to `GET /api/events`, default something small (100), and have `Store.Events` take it
  through to `LIMIT` in SQL so the HMAC work is bounded too.
* Order `DESC` in the query and drop the client-side `reverse()`. Newest-first is what every caller
  wants and doing it in SQL means the `LIMIT` keeps the right end.
* Have `History` request `?limit=20` and stop asking for what it throws away.

**Do not** cache this client-side to paper over it — the log is a live tamper record and a stale one
is worse than a slow one.

---

### 6. `mode: "pomodoro"` is accepted and silently ignored `[read]`

`session.go:132` takes whatever string arrives, `machine.go:22` defines `ModePomodoro`, and
`Session.CycleIndex` / `CycleOf` exist and are never written by anything. `claude.md`'s `/api/state`
example ships a `cycle` object; `sessionView` (`internal/api/session.go:45`) has no such field.

A caller can ask for a Pomodoro session, get `200 OK`, see `"mode":"pomodoro"` echoed in state, and
receive a plain commitment session. **An API that reports success for work it did not do is worse
than one that refuses.**

**Fix now (five lines):** reject unknown and unimplemented modes at the handler with
`400 unsupported_mode`. **Fix properly in V3 item 17.** Doing the reject first means the roadmap can
slip without the API lying in the meantime.

---

### 7. A cascaded transition logs only where it landed `[read]`

`Session.Tick` (`machine.go:139`) deliberately cascades so a daemon that was down through both the
grace window and the whole session lands in `COMPLETE` in one call. That behaviour is correct and
tested. But `manager.go:242` logs once, after the cascade:

```go
next, moved := m.sess.Tick(m.clock, nil)
if moved {
    m.sess = next
    m.event("session_"+string(next.State), "{}")   // only the final state
```

A reboot recovery that crosses `ARMING → FOCUS → COMPLETE` writes one `session_COMPLETE` row. The
`session_FOCUS` transition — the moment the lock actually became irreversible — is not in the log.
For a file whose entire job is being a defensible record, losing the middle of a state cascade is
the wrong kind of gap.

**Fix.** Have `Tick` return the states it passed through, and log each. The cascade is bounded by
`len(allStates)` so the slice is at most five long.

```go
func (s Session) Tick(c Clock, serverNow *time.Time) (Session, []State)
```

Callers that only care whether anything moved check `len(states) > 0`.

---

# PHASE V1 — SHIP IT

**Why second:** V0 is about not shipping something broken. V1 is about shipping at all. Right now
the distribution story is "clone the repo, install Go, Node, the Wails CLI and NSIS, and run a
PowerShell script" — which is a build instruction, not a release.

**Exit:** a stranger with a Windows machine and no toolchain can download one file, run it, and have
a working install; a push to `main` runs every check; and a failing test blocks a merge.

---

### 8. Run the installer on a clean machine `[measured gap]`

`claude.md` states it plainly: *"it has been built but never run, so the service-registration and
teardown paths inside `project.nsi` are unexercised."* That is elevated code, executing
`flowd.exe install` and `flowd.exe uninstall` on somebody else's computer, that has never once been
observed doing so.

**Do this before anything else in V1**, because the outcome changes what the rest of V1 is.

| Check | What "pass" means |
|---|---|
| Install on a clean Windows VM, amd64 | Service `Flow` present in `services.msc`, `Automatic`, `LocalSystem` |
| Reboot | Service running before login; a locked session still locked |
| `flowctl health` | All four layers `active`, signature `ok` |
| Blocked site in a browser | Fails; `wikipedia.org` and `cdc.gov` still resolve |
| Uninstall via Settings → Apps | No service, no `%ProgramData%\Flow`, no hosts block, DNS restored |
| Uninstall **mid-session** | Same, and the session does not resist it |
| Install over an existing install | Either upgrades cleanly or refuses with a readable message |
| Install with the daemon already registered | `flowd install` already errors on this — confirm the installer surfaces it |

The last two are the ones most likely to be broken: `project.nsi:112` runs `flowd.exe install`
unconditionally, and `install()` in `service_windows.go:72` returns an error if the service exists.
The installer catches a non-zero exit and shows a MessageBox, which is the right shape — but on an
*upgrade* that message tells a user their install failed when it did not.

**Likely outcome:** the upgrade path needs `flowd uninstall` before `flowd install`, exactly as
`install.ps1:48` already does. Note that `install.ps1` and `project.nsi` are two implementations of
the same install, and only one of them has that line.

---

### 9. CI: run the check line on every push

There is no `.github/` in this repository. `claude.md` documents the full check line and nothing
enforces it. That is fine for one author with good habits and untenable for anything else.

```yaml
# .github/workflows/check.yml — sketch, not final
on: [push, pull_request]
jobs:
  check:
    runs-on: windows-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
      - uses: actions/setup-node@v4
      # The root package embeds ui/out, so the frontend builds FIRST or go:embed fails.
      - run: cd ui && npm ci && npm run build
      - run: go test . ./cmd/... ./internal/...
      - run: go vet . ./cmd/... ./internal/...
      - run: cd extension && npm test
      - run: cd ui && npm test && npm run typecheck && npm run lint
```

Two things that will bite:

* **`windows-latest` is required**, not preferred. `wfp_windows.go`, `resolver_windows.go`,
  `procwatch_windows.go`, `key_windows.go` and `service_windows.go` are all build-tagged, so a Linux
  runner compiles the stubs and proves nothing about the code that ships.
* **`ui && npm run build` comes before any Go step.** This is already documented as the fresh-clone
  requirement and CI is a fresh clone every time.

Add a second job on `ubuntu-latest` that runs `go build ./...` only — it costs nothing and keeps the
non-Windows stubs from rotting before V6 needs them.

---

### 10. Authenticode signing, or an honest substitute `[blocked on cost]`

Every user gets a SmartScreen warning on first run. `README.md` handles this exactly right — it
tells the user the warning is accurate and to treat it with suspicion — and that honesty is worth
keeping whatever happens here.

Options, in order of preference:

| Option | Cost | What it buys |
|---|---|---|
| OV code-signing certificate | ~$200–400/yr | Removes the "unknown publisher" text; SmartScreen reputation still accrues slowly |
| EV certificate (hardware token) | ~$300–600/yr | Immediate SmartScreen reputation |
| Azure Trusted Signing | ~$10/mo | Same as OV, no hardware, needs a verified identity |
| Do nothing, document loudly | free | What is happening today |

`project.nsi:75` already has the `!finalize 'signtool ...'` hooks commented out, so the mechanical
part is one uncomment and a secret in CI. **This is a purchasing decision, not an engineering one.**
Treat it as blocked until someone decides to spend money, and do not let it block items 11 and 12.

---

### 11. Publish a release

`README.md` says there is no published download. Fix that with items 8 and 9 done:

* Tag `v0.1.0`.
* CI builds `Flow-amd64-installer.exe` and `Flow-arm64-installer.exe` and attaches both.
* Release notes state, in the README's voice: what it does to the machine, that the installer is
  unsigned and why, and that uninstall always works.
* Publish SHA-256 hashes. An unsigned installer with a published hash is meaningfully better than an
  unsigned installer without one, and it costs one line of YAML.

**Both architectures matter.** `claude.md` records that the author's machine is ARM64 (Snapdragon X)
and that the majority of users are amd64. Shipping one is shipping to half the world.

---

### 12. Autostart the window

The daemon autostarts — that is `mgr.StartAutomatic` in `service_windows.go:80` and it is the part
that matters. The *window* does not, and `claude.md` names this as one of the two reasons Phase 7 is
still `[~]`.

Wails v2 has no tray (that is v1 and v3), so the whole feature is a `Run` key:

```text
HKCU\Software\Microsoft\Windows\CurrentVersion\Run
    Flow = "C:\Program Files\Flow\Flow.exe"
```

Written by the installer, removed by the uninstaller, and — this is the part worth getting right —
**exposed as a checkbox the user controls**, because a blocker that adds itself to startup without
asking is the behaviour of the software this app is trying not to be. Default it off.

Start minimised, or start in mini mode: a full window appearing over whatever you opened your laptop
to do is an irritant. Mini mode is already a route and already parks itself in a corner, so
`Flow.exe --mini` calling `SetMini(true)` on startup is a handful of lines.

---

# PHASE V2 — CLOSE THE ENFORCEMENT GAPS

**Why third:** these are the places where the app *claims* to block something and does not. They rank
below V1 only because a gap nobody can install is not yet a gap anybody has.

**Exit:** `youtube.com` fails in Chrome, Firefox and Edge with each browser's default DNS settings;
a locked session survives a real reboot on real hardware; and a tab open before a session starts is
closed in all three browsers.

---

### ~~13. NXDOMAIN the DoH bootstrap hostnames~~ — **already done on `main`**

Struck. `internal/enforce/doh.go` shipped in PR #13 and does this better than the draft above
proposed. The draft led with an endpoint-hostname list; the shipped version leads with
`use-application-dns.net`, Mozilla's documented canary — NXDOMAIN on that name is the supported
signal for "this network manages DNS" and Firefox turns DoH off by itself. The endpoint list is the
belt to that canary's braces, for browsers with DoH explicitly enabled, which ignore the canary by
design.

**Kept from the draft, because it is the reasoning that stops someone "fixing" this later:** browsers
fall back to system DNS when DoH bootstrap fails, so failing the lookup is a *downgrade to the layer
we control*, not an outage. That is why NXDOMAIN is right and a firewall block on 443 would be wrong.

Threat model row 9 stays "Partly": a private DoH endpoint still wins, and the file says so.

---

### 14. Verify Firefox and Edge `[measured gap]`

Chrome is verified twice in `claude.md`, with a table. Firefox and Edge are not verified at all.
Repeat the identical matrix for both, after item 13, and record it in `claude.md` the same way:

| Check | Chrome | Firefox | Edge |
|---|---|---|---|
| `youtube.com` cold | ✅ | ? | ? |
| `youtube.com` with browser DoH on | n/a | ? | ? |
| `m.youtube.com` (subdomain, sink's job) | ✅ | ? | ? |
| `cdc.gov`, `988lifeline.org` reachable | ✅ | ? | ? |
| Warm tab closed on commit | ✅ (extension) | ✗ (no extension) | ✗ (no extension) |

Edge is Chromium and should behave as Chrome does; **confirm rather than assume**, because Edge ships
its own DoH default and the point of this item is the defaults.

---

### 15. Verify reboot survival on real hardware `[measured gap]`

Phase 2's exit criterion is a 10-minute session surviving five things, one of which is a reboot. It
is thoroughly tested against `fakeClock`, and `clock_windows.go` — `QueryInterruptTime`,
`QueryUnbiasedInterruptTime`, and a `BootID` derived by subtracting monotonic from wall and
truncating to the minute — has never been exercised across an actual power cycle.

The failure mode if `BootID` is wrong is not subtle: `Anchor.Recover` credits the shutdown gap from
the wall clock only when it believes a reboot happened. Get it wrong in one direction and a reboot
credits nothing (the lock gets longer); wrong in the other and every NTP step looks like a reboot
(the lock gets shorter, which is the direction that matters).

**Test:** 30-minute session, note `remainingSeconds`, reboot, wait three minutes before logging in,
check `remainingSeconds` again. Expect roughly `before − (downtime + boot time)`, within seconds. Do
it twice: once with a clean shutdown, once with a hard power-off, because the 5-second checkpoint is
the only thing standing between those two cases.

---

### ~~16. Port the extension to Firefox and Edge~~ — **already done on `main`**

Struck. `extension/browser.js` shipped in PR #13: a four-line `browser`/`chrome` alias plus a
`sessionStore` fallback for Firefox builds without `storage.session`, and
`browser_specific_settings` in the manifest. No polyfill dependency, on the stated grounds that a
supply-chain surface inside a blocker people install to protect themselves is not worth four lines.

**The draft was wrong about the shape, and the shipped version is why:** it proposed a second
adapter file (`background.firefox.js`) and one shared coordinator. One aliased import turned out to
be enough, so there is a single `background.js` for all three browsers. The injection seam in
`coordinator.js` still earns its keep — it is what makes `coordinator.test.mjs` cover every browser
at once — but the port needed less than the seam allowed for.

What remains is item 14: loading it in Firefox and Edge and driving it.

---

# PHASE V3 — FINISH THE SPEC'D PRODUCT

**Why fourth:** these are features `claude.md` specifies, that the daemon mostly already supports,
and that a user cannot reach. They rank below V2 because a missing feature is visible and a silent
enforcement gap is not.

**Exit:** every mode and surface described in `claude.md` is reachable from the UI, or is explicitly
struck from `claude.md` with a reason.

---

### 17. Hard Pomodoro

**Spec** (`claude.md`, Modes table): `N × (focus + break)`. Locked during focus; breaks are
IDLE-with-baseline; the next interval auto-arms.

**What exists:** `ModePomodoro`, `Session.CycleIndex`, `Session.CycleOf`. Nothing writes them.

**What it needs:**

* `Commit` takes `cycles` and `breakMinutes` and initialises `CycleOf`.
* A sixth state, or a reuse of `Complete` with a cycle check. **Prefer reuse**: on
  `Focus → Complete`, if `CycleIndex+1 < CycleOf`, start a break `Deadline`, set state to `IDLE`
  with a live break deadline, and auto-`Commit` the next interval when it expires. A sixth state
  means every table in `claude.md` grows a row and every consumer grows a case.
* Breaks are IDLE-with-baseline — so `rulesLocked` needs no change at all, which is the tell that
  reusing IDLE is the right shape.
* **Credit exactly once per completed focus interval**, in the same signed write as the transition,
  same as today. `creditLocked` guards on `s.Credited`; a multi-cycle session needs that flag reset
  per interval or the second cycle earns nothing. Watch this one: it is the same double-pay hazard
  `claude.md` already calls out, in a new place.
* `/api/state` ships `cycle: {index, of, phase}` — already in the spec's example JSON, absent from
  `sessionView`.
* The dial's status line gets `"2 of 4 · locked in"`. The countdown stays the interval's, not the
  whole session's, because the interval is what you are enduring.

**The trap:** auto-arming the next interval must not re-open the 15-second abort window every cycle.
Four free bailout points in a two-hour session is not a commitment device. The grace window belongs
to the *session*, not the interval — cycle 2 onwards arms straight into `FOCUS`.

---

### 18. Spend the bank from the UI

`POST /api/bank/spend` works, is tested (`bank_test.go`, `api/bank_test.go`), and has no control.
`flow-client.ts:97` exposes `api.spend`. The Blocking screen renders `bankLabel(bank)` — a sentence —
and nothing else.

**Build:** under the bank line, when `balanceSeconds >= 60` and the session is IDLE, a duration
picker (5/10/15, capped at the balance) and a "Take a break" button. When `spending` is true, the
line becomes a countdown and the control disappears.

**Copy matters here more than usual.** This is the one path in the app that lifts enforcement
without waiting, and the dialog has to say what it costs before it binds, the same way
`CommitDialog` does:

> Spend 10 of your 10 banked minutes. Blocking lifts for 10 minutes and then locks again on its own.
> You cannot stop it early or get the unused time back.

The last sentence is the one that stops "spend 30, use 2" from ever being attempted, and it is true
today because `Bank.StartSpend` deducts up front and there is no cancel verb.

---

### 19. Create and edit schedules

`schedule.New`, `Set.Add`, `PutSchedule`, and `POST|PUT /api/schedules` all exist and are tested.
The UI can toggle the two seeded rows (`ScheduleRow.tsx`) and cannot create, delete, or retime
anything. `claude.md` lists schedules as the Blocking screen's third row type.

**Build:** an "Add a schedule" row that opens a small form — name, start `HH:MM`, end `HH:MM`, day
checkboxes, and which lists it covers. `parseHHMM` already rejects malformed times with a usable
error, so the daemon does the validating and the UI shows what came back, same as `CustomSites`.

**Two things the daemon needs that it does not have:**

* `DELETE /api/schedules/{id}`. It does not exist. And it must be **direction-aware**, like every
  other mutation: deleting an *active* schedule weakens enforcement, so it is `409 would_weaken`
  while its window is live. Deleting an inactive one is free.
* Editing an active schedule is the same problem. Refuse it while the window is live rather than
  inventing a delay — the window ends on its own, which is friction the clock already provides.

**Day-of-week is in the model and not in the API.** `Schedule.Days` exists and `schedulePut`
(`api/bank.go:74`) does not carry it, so a schedule created over HTTP is every-day whatever the UI
sends. Add the field.

---

### 20. The session composition sub-screen

`ui/src/app/page.tsx:22`:

```ts
const SESSION_LISTS = ["preset.video", "preset.doomscroll", "preset.gaming"];
```

Every session blocks exactly those three, forever. `claude.md` is unusually specific about why this
cannot live on the dial:

> Choosing a session's blocklist *on the dial* would be a bypass: a user mid-craving would deselect
> YouTube and commit to a session that blocks nothing. Session composition lives in a settings
> sub-screen, edited when calm, not at the moment of commitment.

So build the sub-screen, and honour the constraint that makes it necessary:

* Reachable from the Blocking screen, **not** the Focus screen.
* **Editing is refused while a session is active** — `409 would_weaken`, the same machinery as
  everything else. This is the whole point; a settings page you can open mid-craving is the dial with
  extra clicks.
* The Focus screen keeps its read-only "Covers video, doomscroll, gaming" line, which already exists
  and is already correct.
* Persist as a signed row (`sessionListsKey`) beside the others in `persistLocked`.

**Consider, and probably reject:** named presets ("Deep work", "Study"). It is the obvious next step
and it multiplies the number of things a user can fiddle with at 11pm. One list, edited when calm,
is the version that matches the thesis.

---

### 21. Default-deny for `bedtime` and `offline`

`presets.go:165` is candid:

```go
// preset.bedtime and preset.offline mean "everything except the allowlist",
// which the domain-list model cannot express. Phase 6 gives them a
// default-deny flag; until then they are the union of everything named.
```

Phase 6 shipped without it. A user enabling "Offline" gets roughly eighty domains blocked and
believes they got the internet turned off. **That is the worst kind of gap: the enforcement and the
user's model of the enforcement disagree, and the user finds out by successfully browsing.**

**Fix.** A `DefaultDeny bool` on `List`, surfacing as a flag on `Effective`. The sink inverts: NXDOMAIN
everything *except* the permanent allowlist and a user-editable work allowlist.

**This is the most dangerous change in the entire document.** A default-deny resolver that gets the
allowlist wrong is a machine with no internet and a fifteen-minute wait to fix it. Non-negotiables:

* The permanent allowlist (`blocklist/allowlist.go`) applies first and always — crisis lines,
  government, health authorities, OS update.
* A **user allowlist** is mandatory, editable when the rule is off, and pre-seeded with the things
  that break a working machine: the corporate SSO domain, the VPN, the code host, the package
  registries.
* Hosts and WFP **do not participate**. Hosts cannot express default-deny at all, and a WFP
  default-deny is a bricked network stack. Sink only.
* **Ship it behind an explicit confirmation** that names the risk, in the same voice as
  `CommitDialog`.

If that reads as too much risk for the value: the honest alternative is to **delete `preset.offline`
and rename `preset.bedtime` to what it actually blocks.** A preset that lies about its scope is worse
than one that does not exist. Make that call before building.

---

# PHASE V4 — DURABILITY

**Why fifth:** nothing here is wrong today. All of it is wrong in six months of daily use, which is
the timescale a commitment device is supposed to operate on.

**Exit:** a simulated year of use leaves `state.db` bounded, `/api/health` fast, and no route whose
cost grows with history.

---

### 22. Event log retention

`store.Append` has no cap and nothing prunes. Every transition, every reconcile repair, every drift
event, forever. With defect 1 unfixed that is 28,800 rows a day; fixed, it is still unbounded.

**Fix.** A retention pass on daemon start and daily: delete events older than 90 days, keeping a
floor of the most recent 10,000 rows whatever their age.

**The subtlety worth writing down:** the event log is a tamper-evidence record, so *deletion* by the
daemon has to be distinguishable from deletion by an attacker. Write a `log_pruned` event recording
the count and the oldest surviving id. A gap in the ids with no `log_pruned` row before it is then
evidence; a gap with one is housekeeping. Without that, pruning destroys the property the signing
exists to provide.

---

### 23. `/api/health` cost scales with history

`server.go:150` calls `s.st.Verify()`, which walks **every row in both tables** and recomputes an
HMAC for each. That is correct and it is what `flowctl verify` is for. It is also on a route named
"health", which is the route a monitoring loop or a future status indicator would poll.

**Fix.** Split them:

* `GET /api/health` reports layers, uptime and reconcile freshness, plus a *cached* signature verdict
  refreshed on a timer (every few minutes) rather than per request.
* `flowctl verify` keeps the full walk, because a full walk is exactly what it is for.

---

### 24. Rate-limit ARMING/abort churn

`claude.md` deferred this and it is still true: every commit and every abort is a signed write, and
nothing stops a user from tapping the dial forty times. Each tap writes a session row, a baseline
row, a bank row, a schedules row and a custom row (`persistLocked` writes all five, always) plus an
event.

**Two fixes, take both:**

* **Only write rows that changed.** `persistLocked` marshals and writes five rows on every single
  transition. Track dirty flags. This is the bigger win and it is not really a rate limit at all.
* A minimum interval between commits — five seconds — returning `429`. Cheap, and it caps the worst
  case.

---

# PHASE V5 — TRUST AND POLISH

**Exit:** the app is keyboard-navigable, the UI has tests, the stack has one end-to-end test, and no
route leaks more than it must.

---

### 25. `/api/rules` leaks the blocklist to any web page `[read]`

`server.go:73`:

```go
outer.HandleFunc("GET /api/rules", func(w http.ResponseWriter, r *http.Request) {
    w.Header().Set("Access-Control-Allow-Origin", "*")
    s.rules(w, r)
})
```

Unauthenticated, and `*`. The reasoning in `rules.go:22` is sound as far as it goes — an extension
cannot read the token file, the endpoint is loopback-only, read-only, and grants no authority.

**But `*` means any web page you visit can `fetch("http://127.0.0.1:8787/api/rules")` and read the
response.** Not just probe for it — read it. That tells an arbitrary site that you run Flow and
whether you block adult content, gambling, or your own named list of domains. It is not an authority
leak; it is a privacy leak, and the categories involved are about as sensitive as categories get.

**Fix.** Keep it unauthenticated; stop making it universally readable.

```go
// Echo the origin only for extension origins. A web page gets no CORS header,
// so the browser refuses to hand it the body — while the extension, which has
// host_permissions for 127.0.0.1, is unaffected.
if o := r.Header.Get("Origin"); strings.HasPrefix(o, "chrome-extension://") ||
    strings.HasPrefix(o, "moz-extension://") {
    w.Header().Set("Access-Control-Allow-Origin", o)
}
```

A page can still detect that *something* answers on 8787. It can no longer read what.

---

### 26. Modals: focus trap, Escape, `role="dialog"`

`CommitDialog` and `EscapeDialog` are `<div>`s over a backdrop. No `role="dialog"`, no
`aria-modal`, no focus trap, no Escape-to-close, and focus stays behind them in the page. A keyboard
or screen-reader user can open the commit dialog and not reach the Commit button.

`claude.md`'s non-negotiables include no blocking of accessibility software. **Being unusable with
accessibility software is a different failure and it is not covered by that clause, which is
precisely why it needs its own line here.**

`<dialog showModal>` gets the trap, the Escape handling, the backdrop and the inert background for
free, in fewer lines than the current markup. Take the native one.

While in there: add `:focus-visible` rings to the dial, the chips and the switches. There is no focus
indicator anywhere in the app right now.

---

### 27. Component tests for the UI

`ui/src/lib/state.test.ts` is 400 lines and excellent. It is also the only test file in the UI.
`Dial`, `BaselineRow`, `CommitDialog`, `EscapeDialog`, `CustomSites` and `PopOutTimer` have none.

**The bugs this would catch are the ones the project has already hit twice**, both recorded in
`claude.md`: the accent dot rendering at zero progress, and the pop-out timer not receiving the
current time. Both were pure render bugs in components with tested pure helpers underneath.

Add `@testing-library/react` and test the invariants, not the markup:

* A pending-disable switch renders `aria-checked="true"`. **It is fully enforced and must not read
  as off** — the single most important visual rule on the Blocking screen.
* The dial in `FOCUS` renders no hint line.
* The dial in `FOCUS` does not call `onTap`.
* `CustomSites` hides the remove button when `enabled` is true.
* `SessionOwnedRow` renders no switch.

---

### 28. One end-to-end daemon test

Every layer is unit tested with a fake. Nothing tests the whole stack. Add one `//go:build
integration` test that starts a real daemon against a temp `FLOW_DATA_DIR`, drives it over HTTP, and
asserts across a restart:

```text
commit → assert blocked in the effective set
kill the process → start a new one against the same store
assert still FOCUS, assert remaining is within a second, assert blocks re-applied
escape → wait → challenge → verify → assert IDLE, assert baseline survives
```

`manager_test.go` already does the store half of this with a fake clock. This is the version that
also exercises `api.Listen`, the token file, the port file and the proxy.

---

### 29. A signed remote time source

`Anchor.ServerStart` is `*time.Time` and is never set — `clock.go:50` says so directly. The whole
"server elapsed is a ceiling" branch in `Elapsed` is unreachable, and the trust hierarchy in
`claude.md`'s TIME AUTHORITY section has two live sources instead of three.

Monotonic already defeats the actual attack (threat model row 6, tested), so **this is depth, not a
hole.** Do it when there is a reason to.

If done: fetch on commit and on reconnect, store `(server_start, wall_start, monotonic_start,
boot_id)` as one signed row — the wiring is already shaped for it. **Fail open**: no network must
never mean no session, or the app stops working on a plane.

---

# PHASE V6 — PLATFORM

**Exit:** stated when there is a reason to start. Everything here is real work with no user asking
for it yet.

---

### 30. macOS and Linux enforcers

`layers_other.go` returns sink + hosts. `resolver_other.go` errors on `Pin`. `key_other.go` stores
the HMAC key in plaintext. `service_other.go` cannot install a service. The interfaces are all
correct and the platform code is all missing.

* **macOS:** `pf` for the DNS containment, a LaunchDaemon for the service, Keychain for the key.
* **Linux:** `nftables`, systemd, kernel keyring.

`enforce.Enforcer` was designed so this is additive rather than a rewrite, and reading the code that
claim holds up — `Layer` is four methods and nothing outside the platform files assumes Windows.

**Two-week estimate each, and probably not worth it** until someone who is not on Windows asks.

---

### 31. Wails v3: a real tray and real multi-window

`claude.md` records both v2 limitations honestly: no system tray (v1 had it, v3 returns it), and
single-window (which is why mini mode is a route that resizes the one window rather than a floating
widget).

v3 is alpha. **Do not migrate for the tray.** Mini mode covers the need, and swapping the shell
framework for a nicer affordance is exactly the kind of trade this project has been good at
refusing. Revisit when v3 is stable and something actually needs a second window.

---

## THINGS DELIBERATELY NOT ON THIS ROADMAP

Carried forward from `claude.md`, restated so nobody re-proposes them:

* **Safe Mode survival.** A boot-critical service that mishandles failure produces an unbootable
  machine. A bricked laptop is a worse outcome than ten more minutes of Reddit, permanently.
* **Fighting the administrator.** Elevated uninstall works in every state, mid-session included. This
  is a requirement, not a bug.
* **Anything that makes a lock easier to release.** Per-category delays *longer* than 15 minutes are
  fine; shorter is not, and the escape hatch's 15–30 minute band is not configurable by anything.
* **Mobile enforcement.** The phone in your pocket bypasses all of this and no desktop app closes
  that. The README says so and should keep saying so.
* **VPN detection.** WFP could block known VPN client processes. It would also break work VPNs.
* **Per-domain IP blocking.** Shared CDN addresses, rotating faster than a 3s reconcile. Considered
  and rejected once already; the reasoning has not changed.
* **Streaks, scores, charts.** The app competes with YouTube for attention and loses that fight, so
  it should not enter it.
* **Named session presets.** See item 20. Probably a mistake, deliberately.

---

## HOW TO WORK THROUGH THIS

Unchanged from `claude.md`: **no direct commits to `main`.** Branch, commit, `gh pr create`. The
pre-commit hook enforces it.

One branch per queue item, named for the item:

```powershell
git checkout -b v0-dns-sink-stop        # item 1
git checkout -b v0-bank-spend-baseline  # item 2
```

**Every V0 item ships with a test that fails without the fix.** All seven are behavioural defects, so
all seven are testable, and a fix without a test for a bug this class is a fix that comes back.

Update the V2 progress box in this file as each phase closes, and fold anything learned back into
`claude.md` — the verification tables in particular, which are the most useful thing in that file and
the thing most likely to go stale.

**Checks, unchanged:**

```powershell
go test . ./cmd/... ./internal/... ; go vet . ./cmd/... ./internal/...
cd extension ; npm test
cd ../ui ; npm test ; npm run typecheck ; npm run lint ; npm run build
```

Named packages rather than `./...` because `ui/node_modules` ships a stray Go package that `./...`
picks up as part of this module. On a fresh clone run `(cd ui && npm run build)` once before anything
Go — the root package embeds `ui/out` and `go:embed` fails on a directory that does not exist.
