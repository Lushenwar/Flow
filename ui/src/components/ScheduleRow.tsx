"use client";

import { listName, scheduleWindow, type ScheduleRow } from "@/lib/state";

interface Props {
  row: ScheduleRow;
  last: boolean;
  busy: boolean;
  onToggle: (enabled: boolean) => void;
}

/**
 * The third row type on the Blocking screen. A schedule has no countdown of its
 * own — it is on or off, and the window says when it bites.
 *
 * Turning one ON is instant, like a baseline rule. Turning one OFF is instant
 * too, and deliberately so: a schedule that has not fired yet is not enforcing
 * anything, so a delay would be friction with nothing behind it. Once the window
 * is live, the rules it contributes are covered by the same union as everything
 * else and cannot be weakened mid-session.
 */
export function ScheduleRowView({ row, last, busy, onToggle }: Props) {
  return (
    <div
      className="flex items-center justify-between py-[10px]"
      style={{ borderBottom: last ? "none" : "0.5px solid var(--hairline)" }}
    >
      <span className="flex flex-col">
        <span className="text-[14px]">{row.name}</span>
        <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
          {scheduleWindow(row, Intl.DateTimeFormat().resolvedOptions().timeZone)}
          {" · "}
          {row.listIds.map(listName).join(", ").toLowerCase()}
        </span>
      </span>

      <span className="flex items-center gap-3">
        {row.active && (
          <span className="text-[11px]" style={{ color: "var(--accent)" }}>
            on now
          </span>
        )}
        <button
          type="button"
          role="switch"
          aria-checked={row.enabled}
          disabled={busy}
          onClick={() => onToggle(!row.enabled)}
          className="relative rounded-full transition-colors disabled:opacity-50"
          style={{
            width: 34,
            height: 20,
            background: row.enabled ? "var(--accent)" : "var(--hairline)",
          }}
        >
          <span
            className="absolute rounded-full bg-white transition-transform"
            style={{
              width: 16,
              height: 16,
              top: 2,
              left: 2,
              transform: row.enabled ? "translateX(14px)" : "translateX(0)",
            }}
          />
        </button>
      </span>
    </div>
  );
}
