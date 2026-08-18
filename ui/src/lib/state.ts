/**
 * Pure derivations. No component computes time — it all happens here, where it
 * can be unit tested without rendering anything.
 */

export type SessionState =
  | "IDLE"
  | "ARMING"
  | "FOCUS"
  | "BREAK"
  | "RELEASING"
  | "COMPLETE";

/** Present only for Hard Pomodoro. Null for a commitment session. */
export interface CycleView {
  index: number; // 1-based
  of: number;
  phase: "focus" | "break";
  breakSeconds: number;
  breakRemainingSeconds: number;
}

export interface SessionView {
  state: SessionState;
  mode: string;
  targetAt: string | null;
  remainingSeconds: number;
  canRelease: boolean;
  blocklistIds: string[];
  escape: { requested: boolean; availableAt: string | null };
  durationSeconds: number;
  graceRemainingSeconds: number;
  graceSeconds: number;
  cycle: CycleView | null;
}

export interface BaselineRow {
  id: string;
  enabled: boolean;
  pendingDisableAt: string | null;
}

export interface EffectiveView {
  blockedIds: string[];
  attribution: Record<string, "baseline" | "session" | "schedule">;
}

export interface ScheduleRow {
  id: string;
  name: string;
  listIds: string[];
  start: string;
  end: string;
  tz: string;
  enabled: boolean;
  active: boolean;
  /** 0 = Sunday. Empty means every day. */
  days?: number[];
}

export const DAY_LABELS = ["S", "M", "T", "W", "T", "F", "S"] as const;

/** "every day", or the days it may start on. A window that crosses midnight
 *  belongs to the day it STARTED on, which is why this never says "and Sunday
 *  morning". */
export function dayLabel(days?: number[]): string {
  if (!days || days.length === 0 || days.length === 7) return "every day";
  const names = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
  return days
    .slice()
    .sort((a, b) => a - b)
    .map((d) => names[d] ?? "?")
    .join(", ");
}

export interface BankView {
  balanceSeconds: number;
  spending: boolean;
  remainingSeconds: number;
}

/** The escape list for default-deny modes. `locked` mirrors whether one is in
 *  force — the daemon decides it, the UI reports it. */
export interface AllowList {
  id: string;
  name: string;
  domains: string[];
  locked: boolean;
  max: number;
}

export interface CustomList {
  id: string;
  name: string;
  domains: string[];
  /** Mirrors the baseline rule carrying the list. Removal is refused while this
   *  is true, and the daemon is what decides it — the UI only reports it. */
  enabled: boolean;
  max: number;
}

/**
 * Split whatever was typed into candidate domains.
 *
 * People paste lists — one per line out of a note, or comma-separated out of
 * somewhere else. Splitting here rather than making them add one at a time is
 * the difference between blocking twelve sites and giving up after three.
 * Validation stays on the daemon; this only decides where the boundaries are.
 */
export function splitDomains(input: string): string[] {
  return input
    .split(/[\s,;]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}

/** "23:00 – 07:00 · every day", with the pinned zone shown when it differs. */
export function scheduleWindow(s: ScheduleRow, systemTz?: string): string {
  const base = `${s.start} – ${s.end}`;
  if (systemTz && s.tz && s.tz !== systemTz) {
    // The zone is pinned at creation. Surfacing it explains why the window did
    // not move when the machine's timezone did.
    return `${base} ${s.tz}`;
  }
  return base;
}

/** Recreation time reads in whole minutes; seconds are noise at this scale. */
export function bankLabel(bank: BankView | null): string {
  if (!bank) return "";
  if (bank.spending) {
    return `${formatCountdown(bank.remainingSeconds)} of recreation left`;
  }
  const minutes = Math.floor(bank.balanceSeconds / 60);
  if (minutes <= 0) return "No recreation time banked";
  return `${minutes} recreation ${minutes === 1 ? "minute" : "minutes"} banked`;
}

export interface EventRow {
  id: number;
  ts: string;
  kind: string;
  data: string;
}

/**
 * Copy for the event log. Non-judgmental throughout: "Session ended early", not
 * "You failed". The log exists to be informative, not to shame anyone into
 * avoiding it — an escape hatch people are ashamed to use is a trap.
 */
export function eventLabel(kind: string): string | null {
  const labels: Record<string, string> = {
    session_commit: "Session started",
    session_FOCUS: "Session locked",
    session_COMPLETE: "Session finished",
    session_abort: "Backed out during the grace window",
    session_escape_requested: "Early end requested",
    escape_challenge_failed: "Challenge not matched",
    session_ended_early: "Session ended early",
    clock_drift: "Clock drift detected",
    reconcile_repaired: "Blocking rules repaired",
    reconcile_failed: "Could not repair blocking rules",
    baseline_enabled: "Block turned on",
    baseline_disable_requested: "Block scheduled to turn off",
    baseline_disable_cancelled: "Turn-off cancelled",
    baseline_disabled: "Block turned off",
    boot_recovered: "Session restored after restart",
    session_signature_invalid: "Saved session failed its signature check",
    baseline_signature_invalid: "Saved blocks failed their signature check",
    custom_added: "Sites added to your list",
    custom_removed: "Site removed from your list",
    custom_signature_invalid: "Saved custom sites failed their signature check",
    allow_added: "Sites added to the always-reachable list",
    allow_removed: "Site removed from the always-reachable list",
    allow_signature_invalid: "Saved always-reachable sites failed their signature check",
    session_lists_changed: "Changed what a session blocks",
    schedule_saved: "Schedule saved",
    schedule_deleted: "Schedule deleted",
    schedule_timezone_changed: "Timezone changed under an active schedule",
  };
  // Unknown kinds are dropped rather than shown raw: service_start on every
  // launch is noise, not history.
  return labels[kind] ?? null;
}

/**
 * Unlabelled kinds removed. Order is the daemon's — /api/events returns newest
 * first, because the limit has to be applied in SQL and a LIMIT only keeps the
 * right end if the ORDER BY already points that way.
 */
export function visibleEvents(events: EventRow[]): EventRow[] {
  return events.filter((e) => eventLabel(e.kind) !== null);
}

export interface AppState {
  session: SessionView;
  baseline: BaselineRow[];
  effective: EffectiveView;
}

/**
 * mm:ss, or h:mm:ss past an hour. Always two digits after the first colon so the
 * string never changes width mid-tick.
 */
export function formatCountdown(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const hours = Math.floor(s / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  const pad = (n: number) => String(n).padStart(2, "0");
  return hours > 0
    ? `${hours}:${pad(minutes)}:${pad(seconds)}`
    : `${pad(minutes)}:${pad(seconds)}`;
}

/** Fraction of the ring to fill, clamped to [0, 1]. */
export function progressFraction(elapsed: number, total: number): number {
  if (total <= 0) return 0;
  return Math.min(1, Math.max(0, elapsed / total));
}

export const DIAL_RADIUS = 76;
export const DIAL_CIRCUMFERENCE = 2 * Math.PI * DIAL_RADIUS;

/** SVG stroke-dasharray for a given fill fraction. */
export function dashArray(fraction: number): string {
  const filled = DIAL_CIRCUMFERENCE * progressFraction(fraction, 1);
  return `${filled} ${DIAL_CIRCUMFERENCE}`;
}

/**
 * How full the ring should be.
 *
 * ARMING sweeps the grace window, because that is the countdown the user is
 * watching and can still back out of. FOCUS and RELEASING sweep the session.
 * IDLE is empty, COMPLETE is full.
 */
export function arcFraction(session: SessionView): number {
  switch (session.state) {
    case "ARMING":
      return progressFraction(
        session.graceSeconds - session.graceRemainingSeconds,
        session.graceSeconds,
      );
    case "BREAK":
      // The break's own countdown. Reusing the interval denominator would leave
      // the ring almost motionless through the one phase that is short.
      return progressFraction(
        (session.cycle?.breakSeconds ?? 0) -
          (session.cycle?.breakRemainingSeconds ?? 0),
        session.cycle?.breakSeconds ?? 0,
      );
    case "FOCUS":
    case "RELEASING":
      return progressFraction(
        session.durationSeconds - session.remainingSeconds,
        session.durationSeconds,
      );
    case "COMPLETE":
      return 1;
    default:
      return 0;
  }
}

/**
 * What a tap on the dial does. There is deliberately no action in FOCUS or
 * RELEASING — the tap is inert and there is no control to render.
 */
export type DialAction = "commit" | "abort" | "none";

export function dialAction(session: SessionView): DialAction {
  switch (session.state) {
    case "IDLE":
    case "COMPLETE":
      return "commit";
    case "ARMING":
    // A break enforces nothing beyond baseline, so the tap declines the rest of
    // the pomodoro rather than breaking a lock. Same verb, same endpoint.
    case "BREAK":
      return "abort";
    default:
      return "none";
  }
}

/** Seconds until an ISO instant, floored at 0. Never negative. */
export function secondsUntil(iso: string | null, now: number): number {
  if (!iso) return 0;
  const then = Date.parse(iso);
  if (Number.isNaN(then)) return 0;
  return Math.max(0, Math.round((then - now) / 1000));
}

export type RowState = "on" | "off" | "pending";

/**
 * A pending disable is NOT off. The rule is fully enforced for the whole
 * countdown, so the switch must not read as off.
 */
export function rowState(row: BaselineRow): RowState {
  if (row.pendingDisableAt) return "pending";
  return row.enabled ? "on" : "off";
}

/** "unblocks in 14:32" — the countdown the Blocking screen shows. */
export function pendingLabel(row: BaselineRow, now: number): string {
  return `unblocks in ${formatCountdown(secondsUntil(row.pendingDisableAt, now))}`;
}

/** Human label for a preset id. Falls back to the id rather than hiding it. */
export function listName(id: string): string {
  const names: Record<string, string> = {
    "preset.adult": "Adult content",
    "preset.gambling": "Gambling",
    "preset.shopping": "Shopping",
    "preset.doomscroll": "Doomscroll",
    "preset.video": "Video",
    "preset.gaming": "Gaming",
    "preset.delivery": "Food delivery",
    "preset.bedtime": "Bedtime",
    "preset.offline": "Offline",
    "preset.work": "Work",
    "preset.study": "Study",
    "custom.blocked": "Custom sites",
  };
  return names[id] ?? id;
}

export interface DialText {
  countdown: string;
  status: string;
  /** Absent in FOCUS: a "tap to commit" hint under "locked in" contradicts the
   *  state directly above it, at the one moment the tap is inert. */
  hint?: string;
  /** IDLE only: without it the screen implies nothing is protected while
   *  gambling and adult content are still enforced. */
  coverage?: string;
}

/**
 * The countdown the dial shows, derived from the daemon's absolute instant on
 * every tick rather than from a local accumulator. A setInterval that decrements
 * a number drifts, and resets when the window does.
 */
export function liveRemainingSeconds(session: SessionView, now: number): number {
  if (session.targetAt) return secondsUntil(session.targetAt, now);
  return session.remainingSeconds;
}

export function dialText(
  session: SessionView,
  baselineOnCount: number,
  chosenMinutes: number,
  now?: number,
): DialText {
  const remaining =
    now === undefined ? session.remainingSeconds : liveRemainingSeconds(session, now);
  const coverage =
    baselineOnCount === 1
      ? "1 block always on"
      : `${baselineOnCount} blocks always on`;

  // "2 of 4 · " prefixes the status in Hard Pomodoro. The interval you are in is
  // the thing you are enduring, so it belongs next to the state, not in a corner.
  const cycle = session.cycle ? `${session.cycle.index} of ${session.cycle.of} · ` : "";

  switch (session.state) {
    case "ARMING":
      return {
        countdown: formatCountdown(remaining),
        status: `starting in ${session.graceRemainingSeconds}s`,
        hint: "tap to cancel",
      };
    case "FOCUS":
      return {
        countdown: formatCountdown(remaining),
        status: `${cycle}locked in`,
        hint: "no way to stop this",
      };
    case "BREAK":
      return {
        countdown: formatCountdown(remaining),
        status: `${cycle}break`,
        // The one honest thing to say: nothing is stopping you, and the next
        // interval starts on its own whether you are ready or not.
        hint: "tap to end here",
      };
    case "RELEASING":
      return {
        countdown: formatCountdown(remaining),
        status: "ending early",
        hint: "still blocked until the countdown ends",
      };
    case "COMPLETE":
      return { countdown: formatCountdown(0), status: "done" };
    default:
      return {
        countdown: formatCountdown(chosenMinutes * 60),
        status: "not focusing",
        hint: "tap to commit",
        coverage,
      };
  }
}

/** How many baseline rules are actually being enforced right now. */
export function baselineOnCount(rows: BaselineRow[]): number {
  return rows.filter((r) => r.enabled).length;
}

/**
 * Session-owned rules appear on the Blocking screen so you can see they are
 * enforced, but without a switch — you do not control them right now.
 */
export function sessionOwnedIds(state: AppState): string[] {
  const baselineIds = new Set(state.baseline.map((r) => r.id));
  return state.effective.blockedIds.filter(
    (id) =>
      !baselineIds.has(id) && state.effective.attribution[id] === "session",
  );
}
