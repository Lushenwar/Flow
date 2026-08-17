import { describe, expect, it } from "vitest";
import {
  AppState,
  BaselineRow,
  SessionView,
  arcFraction,
  bankLabel,
  baselineOnCount,
  dashArray,
  dialAction,
  dialText,
  EventRow,
  eventLabel,
  formatCountdown,
  liveRemainingSeconds,
  pendingLabel,
  progressFraction,
  rowState,
  scheduleWindow,
  secondsUntil,
  sessionOwnedIds,
  splitDomains,
  visibleEvents,
} from "./state";

const session = (over: Partial<SessionView> = {}): SessionView => ({
  state: "IDLE",
  mode: "commitment",
  targetAt: null,
  remainingSeconds: 0,
  canRelease: false,
  blocklistIds: [],
  escape: { requested: false, availableAt: null },
  durationSeconds: 0,
  graceRemainingSeconds: 0,
  graceSeconds: 0,
  cycle: null,
  ...over,
});

describe("formatCountdown", () => {
  it("never changes width mid-tick", () => {
    // The reason the countdown is monospace in the first place.
    expect(formatCountdown(9)).toBe("00:09");
    expect(formatCountdown(59)).toBe("00:59");
    expect(formatCountdown(60)).toBe("01:00");
    expect(formatCountdown(872)).toBe("14:32");
  });

  it("grows a field only past an hour", () => {
    expect(formatCountdown(3599)).toBe("59:59");
    expect(formatCountdown(3600)).toBe("1:00:00");
    expect(formatCountdown(7325)).toBe("2:02:05");
  });

  it("floors at zero rather than showing a negative", () => {
    expect(formatCountdown(-5)).toBe("00:00");
    expect(formatCountdown(0)).toBe("00:00");
  });
});

describe("progressFraction", () => {
  it("clamps to [0,1]", () => {
    expect(progressFraction(5, 10)).toBe(0.5);
    expect(progressFraction(20, 10)).toBe(1);
    expect(progressFraction(-5, 10)).toBe(0);
  });

  it("does not divide by zero", () => {
    expect(progressFraction(5, 0)).toBe(0);
  });
});

describe("dashArray", () => {
  it("is empty at 0 and full at 1", () => {
    expect(dashArray(0).startsWith("0 ")).toBe(true);
    const [filled, total] = dashArray(1).split(" ").map(Number);
    expect(filled).toBeCloseTo(total);
  });
});

describe("secondsUntil", () => {
  const now = Date.parse("2026-08-04T12:00:00Z");

  it("counts down to an instant", () => {
    expect(secondsUntil("2026-08-04T12:14:32Z", now)).toBe(872);
  });

  it("floors at zero for a past instant", () => {
    expect(secondsUntil("2026-08-04T11:00:00Z", now)).toBe(0);
  });

  it("treats missing or unparseable input as zero", () => {
    expect(secondsUntil(null, now)).toBe(0);
    expect(secondsUntil("not a date", now)).toBe(0);
  });
});

describe("rowState", () => {
  const row = (over: Partial<BaselineRow>): BaselineRow => ({
    id: "preset.adult",
    enabled: true,
    pendingDisableAt: null,
    ...over,
  });

  it("distinguishes on, off, and pending", () => {
    expect(rowState(row({ enabled: true }))).toBe("on");
    expect(rowState(row({ enabled: false }))).toBe("off");
    expect(
      rowState(row({ enabled: true, pendingDisableAt: "2026-08-04T12:15:00Z" })),
    ).toBe("pending");
  });

  it("never reports a pending disable as off, because it is not", () => {
    const pending = row({ enabled: true, pendingDisableAt: "2026-08-04T12:15:00Z" });
    expect(rowState(pending)).not.toBe("off");
  });
});

describe("pendingLabel", () => {
  it("reads as a countdown", () => {
    const now = Date.parse("2026-08-04T12:00:00Z");
    expect(
      pendingLabel(
        { id: "x", enabled: true, pendingDisableAt: "2026-08-04T12:14:32Z" },
        now,
      ),
    ).toBe("unblocks in 14:32");
  });
});

describe("dialText", () => {
  it("tells IDLE what is still enforced", () => {
    const t = dialText(session(), 3, 50);
    expect(t.countdown).toBe("50:00");
    expect(t.status).toBe("not focusing");
    expect(t.coverage).toBe("3 blocks always on");
  });

  it("pluralises coverage correctly", () => {
    expect(dialText(session(), 1, 50).coverage).toBe("1 block always on");
  });

  it("makes the second tap discoverable in ARMING", () => {
    const t = dialText(
      session({ state: "ARMING", remainingSeconds: 1500, graceRemainingSeconds: 12 }),
      2,
      25,
    );
    expect(t.status).toBe("starting in 12s");
    expect(t.hint).toBe("tap to cancel");
  });

  it("never shows the IDLE hint in FOCUS", () => {
    // The mockup's bug: "tap to commit" under "locked in" contradicts the state
    // directly above it, at the one moment the tap is inert.
    const t = dialText(session({ state: "FOCUS", remainingSeconds: 2892 }), 2, 25);
    expect(t.countdown).toBe("48:12");
    expect(t.status).toBe("locked in");
    expect(t.hint).not.toBe("tap to commit");
    expect(t.coverage).toBeUndefined();
  });

  it("says blocks are still live while releasing", () => {
    const t = dialText(session({ state: "RELEASING", remainingSeconds: 600 }), 2, 25);
    expect(t.hint).toContain("still blocked");
  });

  it("shows a neutral done state", () => {
    const t = dialText(session({ state: "COMPLETE" }), 2, 25);
    expect(t.status).toBe("done");
    expect(t.countdown).toBe("00:00");
  });
});

describe("arcFraction", () => {
  it("is empty in IDLE and full in COMPLETE", () => {
    expect(arcFraction(session())).toBe(0);
    expect(arcFraction(session({ state: "COMPLETE" }))).toBe(1);
  });

  it("sweeps the grace window in ARMING, not the session", () => {
    // 15s grace, 3s gone. If it swept the 25-minute session instead, the ring
    // would look motionless during the one window the user can still back out of.
    const s = session({
      state: "ARMING",
      graceSeconds: 15,
      graceRemainingSeconds: 12,
      durationSeconds: 1500,
      remainingSeconds: 1497,
    });
    expect(arcFraction(s)).toBeCloseTo(0.2);
  });

  it("sweeps the session in FOCUS", () => {
    const s = session({ state: "FOCUS", durationSeconds: 1500, remainingSeconds: 375 });
    expect(arcFraction(s)).toBeCloseTo(0.75);
  });

  it("keeps sweeping the session while releasing", () => {
    const s = session({ state: "RELEASING", durationSeconds: 1000, remainingSeconds: 500 });
    expect(arcFraction(s)).toBeCloseTo(0.5);
  });

  it("survives a zero denominator", () => {
    expect(arcFraction(session({ state: "FOCUS" }))).toBe(0);
  });
});

describe("pomodoro", () => {
  const cycle = (over: Partial<NonNullable<SessionView["cycle"]>> = {}) => ({
    index: 2,
    of: 4,
    phase: "focus" as const,
    breakSeconds: 300,
    breakRemainingSeconds: 0,
    ...over,
  });

  it("puts the interval next to the state, not in a corner", () => {
    const t = dialText(
      session({ state: "FOCUS", remainingSeconds: 600, cycle: cycle() }),
      2,
      25,
    );
    expect(t.status).toBe("2 of 4 · locked in");
  });

  it("says break, and says the tap works", () => {
    const t = dialText(
      session({
        state: "BREAK",
        remainingSeconds: 180,
        cycle: cycle({ phase: "break", breakRemainingSeconds: 180 }),
      }),
      2,
      25,
    );
    expect(t.status).toBe("2 of 4 · break");
    expect(t.hint).toBe("tap to end here");
  });

  it("leaves a commitment session's status alone", () => {
    const t = dialText(session({ state: "FOCUS", remainingSeconds: 600 }), 2, 25);
    expect(t.status).toBe("locked in");
  });

  it("sweeps the break's own countdown, not the interval's", () => {
    // 5-minute break, 2 minutes gone. Against the 25-minute interval the ring
    // would look motionless through the one phase that is short.
    const s = session({
      state: "BREAK",
      durationSeconds: 1500,
      remainingSeconds: 1500,
      cycle: cycle({ phase: "break", breakSeconds: 300, breakRemainingSeconds: 180 }),
    });
    expect(arcFraction(s)).toBeCloseTo(0.4);
  });

  it("makes the tap end the pomodoro during a break", () => {
    // A break enforces nothing beyond baseline, so there is no lock to break.
    expect(dialAction(session({ state: "BREAK", cycle: cycle() }))).toBe("abort");
  });

  it("keeps the tap inert inside an interval", () => {
    expect(dialAction(session({ state: "FOCUS", cycle: cycle() }))).toBe("none");
  });
});

describe("dialAction", () => {
  it("commits from IDLE and COMPLETE", () => {
    expect(dialAction(session())).toBe("commit");
    expect(dialAction(session({ state: "COMPLETE" }))).toBe("commit");
  });

  it("aborts in ARMING", () => {
    expect(dialAction(session({ state: "ARMING" }))).toBe("abort");
  });

  it("is inert once locked", () => {
    // There is no stop verb, so there is no control to render.
    expect(dialAction(session({ state: "FOCUS" }))).toBe("none");
    expect(dialAction(session({ state: "RELEASING" }))).toBe("none");
  });
});

describe("liveRemainingSeconds", () => {
  const now = Date.parse("2026-08-04T12:00:00Z");

  it("derives from targetAt, not from the polled number", () => {
    // The daemon polls once a second; the countdown ticks smoothly between polls
    // because it is recomputed from an absolute instant, never accumulated.
    const s = session({
      state: "FOCUS",
      targetAt: "2026-08-04T12:10:00Z",
      remainingSeconds: 999,
    });
    expect(liveRemainingSeconds(s, now)).toBe(600);
  });

  it("falls back to the polled number when there is no target", () => {
    expect(liveRemainingSeconds(session({ remainingSeconds: 42 }), now)).toBe(42);
  });

  it("floors at zero past the target", () => {
    const s = session({ state: "FOCUS", targetAt: "2026-08-04T11:59:00Z" });
    expect(liveRemainingSeconds(s, now)).toBe(0);
  });
});

describe("scheduleWindow", () => {
  const s = {
    id: "b", name: "Bedtime", listIds: ["preset.bedtime"],
    start: "23:00", end: "07:00", tz: "America/New_York",
    enabled: true, active: false,
  };

  it("reads as a window", () => {
    expect(scheduleWindow(s, "America/New_York")).toBe("23:00 – 07:00");
  });

  it("surfaces the pinned zone when the machine has moved", () => {
    // Explains why the window did not shift when the system timezone did.
    expect(scheduleWindow(s, "Asia/Tokyo")).toContain("America/New_York");
  });
});

describe("bankLabel", () => {
  it("counts down while spending", () => {
    expect(bankLabel({ balanceSeconds: 0, spending: true, remainingSeconds: 872 }))
      .toBe("14:32 of recreation left");
  });

  it("reports whole minutes banked", () => {
    expect(bankLabel({ balanceSeconds: 600, spending: false, remainingSeconds: 0 }))
      .toBe("10 recreation minutes banked");
    expect(bankLabel({ balanceSeconds: 60, spending: false, remainingSeconds: 0 }))
      .toBe("1 recreation minute banked");
  });

  it("says nothing is banked rather than showing a bare zero", () => {
    expect(bankLabel({ balanceSeconds: 0, spending: false, remainingSeconds: 0 }))
      .toBe("No recreation time banked");
    expect(bankLabel({ balanceSeconds: 30, spending: false, remainingSeconds: 0 }))
      .toBe("No recreation time banked");
  });

  it("renders nothing before the bank has loaded", () => {
    expect(bankLabel(null)).toBe("");
  });
});

describe("splitDomains", () => {
  it("takes a pasted list however it was separated", () => {
    // One per line out of a note, or comma-separated out of somewhere else.
    expect(splitDomains("reddit.com\nx.com, news.ycombinator.com;  tiktok.com"))
      .toEqual(["reddit.com", "x.com", "news.ycombinator.com", "tiktok.com"]);
  });

  it("takes a single entry unchanged, whitespace and all", () => {
    expect(splitDomains("  reddit.com  ")).toEqual(["reddit.com"]);
  });

  it("yields nothing for empty input, so the button has nothing to send", () => {
    expect(splitDomains("")).toEqual([]);
    expect(splitDomains("   \n  ")).toEqual([]);
  });

  it("does not try to validate — that is the daemon's job", () => {
    // Splitting decides boundaries only. A bad entry has to reach the daemon to
    // come back with a reason the user can act on.
    expect(splitDomains("not a domain")).toEqual(["not", "a", "domain"]);
  });
});

describe("event log copy", () => {
  it("never blames the user", () => {
    // "Session ended early", not "You failed". An escape hatch people are
    // ashamed to use is a trap with extra steps.
    const blaming = /fail(ed|ure)|gave up|you (lost|broke|failed)|cheat|weak/i;
    for (const kind of [
      "session_ended_early",
      "session_abort",
      "escape_challenge_failed",
      "session_escape_requested",
    ]) {
      const label = eventLabel(kind);
      expect(label).not.toBeNull();
      expect(label!).not.toMatch(blaming);
    }
  });

  it("labels the events the tamper log exists for", () => {
    expect(eventLabel("clock_drift")).toBe("Clock drift detected");
    expect(eventLabel("reconcile_repaired")).toBe("Blocking rules repaired");
    expect(eventLabel("session_ended_early")).toBe("Session ended early");
  });

  it("drops unlabelled noise rather than printing raw kinds", () => {
    expect(eventLabel("service_start")).toBeNull();
  });
});

describe("visibleEvents", () => {
  // As the daemon returns them: newest first, because the limit is applied in
  // SQL and a LIMIT only keeps the right end if the ORDER BY points that way.
  const rows: EventRow[] = [
    { id: 3, ts: "2026-08-04T12:02:00Z", kind: "clock_drift", data: "{}" },
    { id: 2, ts: "2026-08-04T12:01:00Z", kind: "session_commit", data: "{}" },
    { id: 1, ts: "2026-08-04T12:00:00Z", kind: "service_start", data: "{}" },
  ];

  it("hides noise and keeps the daemon's order", () => {
    const got = visibleEvents(rows);
    expect(got.map((e) => e.id)).toEqual([3, 2]);
  });

  it("survives an empty log", () => {
    expect(visibleEvents([])).toEqual([]);
  });
});

describe("baseline helpers", () => {
  const state: AppState = {
    session: session({ state: "FOCUS", blocklistIds: ["preset.video"] }),
    baseline: [
      { id: "preset.adult", enabled: true, pendingDisableAt: null },
      { id: "preset.gambling", enabled: true, pendingDisableAt: null },
      { id: "preset.shopping", enabled: false, pendingDisableAt: null },
    ],
    effective: {
      blockedIds: ["preset.adult", "preset.gambling", "preset.video"],
      attribution: {
        "preset.adult": "baseline",
        "preset.gambling": "baseline",
        "preset.video": "session",
      },
    },
  };

  it("counts only what is actually enforced", () => {
    expect(baselineOnCount(state.baseline)).toBe(2);
  });

  it("surfaces session-owned rules that have no switch", () => {
    expect(sessionOwnedIds(state)).toEqual(["preset.video"]);
  });

  it("does not list a baseline rule as session-owned when both claim it", () => {
    // Attribution comes from the daemon; the UI must never derive it. If the
    // daemon says baseline, the row keeps its switch.
    const both: AppState = {
      ...state,
      baseline: [...state.baseline, { id: "preset.video", enabled: true, pendingDisableAt: null }],
      effective: {
        ...state.effective,
        attribution: { ...state.effective.attribution, "preset.video": "baseline" },
      },
    };
    expect(sessionOwnedIds(both)).toEqual([]);
  });
});
