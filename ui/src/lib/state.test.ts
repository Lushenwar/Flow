import { describe, expect, it } from "vitest";
import {
  AppState,
  BaselineRow,
  SessionView,
  baselineOnCount,
  dashArray,
  dialText,
  formatCountdown,
  pendingLabel,
  progressFraction,
  rowState,
  secondsUntil,
  sessionOwnedIds,
} from "./state";

const session = (over: Partial<SessionView> = {}): SessionView => ({
  state: "IDLE",
  mode: "commitment",
  targetAt: null,
  remainingSeconds: 0,
  canRelease: false,
  graceRemainingSeconds: 0,
  blocklistIds: [],
  escape: { requested: false, availableAt: null },
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
