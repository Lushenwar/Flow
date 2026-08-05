/**
 * Pure derivations. No component computes time — it all happens here, where it
 * can be unit tested without rendering anything.
 */

export type SessionState =
  | "IDLE"
  | "ARMING"
  | "FOCUS"
  | "RELEASING"
  | "COMPLETE";

export interface SessionView {
  state: SessionState;
  mode: string;
  targetAt: string | null;
  remainingSeconds: number;
  canRelease: boolean;
  graceRemainingSeconds: number;
  blocklistIds: string[];
  escape: { requested: boolean; availableAt: string | null };
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

export function dialText(
  session: SessionView,
  baselineOnCount: number,
  chosenMinutes: number,
): DialText {
  const coverage =
    baselineOnCount === 1
      ? "1 block always on"
      : `${baselineOnCount} blocks always on`;

  switch (session.state) {
    case "ARMING":
      return {
        countdown: formatCountdown(session.remainingSeconds),
        status: `starting in ${session.graceRemainingSeconds}s`,
        hint: "tap to cancel",
      };
    case "FOCUS":
      return {
        countdown: formatCountdown(session.remainingSeconds),
        status: "locked in",
        hint: "no way to stop this",
      };
    case "RELEASING":
      return {
        countdown: formatCountdown(session.remainingSeconds),
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
