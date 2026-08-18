"use client";

import { useState } from "react";
import { ApiError, api } from "@/lib/flow-client";
import { DAY_LABELS, listName, type ScheduleRow } from "@/lib/state";

/**
 * Create or edit a schedule.
 *
 * Everything about validity is the daemon's call — parseHHMM already rejects a
 * malformed time with a usable message, and a live window is refused with
 * would_weaken. This form's job is to collect and to show what came back, the
 * same division of labour CustomSites uses.
 */
const LISTS = [
  "preset.doomscroll",
  "preset.video",
  "preset.gaming",
  "preset.shopping",
  "preset.delivery",
  "preset.adult",
  "preset.gambling",
];

export function ScheduleEditor({
  existing,
  onDone,
  onCancel,
}: {
  existing?: ScheduleRow;
  onDone: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState(existing?.name ?? "");
  const [start, setStart] = useState(existing?.start ?? "23:00");
  const [end, setEnd] = useState(existing?.end ?? "07:00");
  const [lists, setLists] = useState<string[]>(existing?.listIds ?? ["preset.doomscroll"]);
  const [days, setDays] = useState<number[]>(existing?.days ?? []);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  async function save() {
    setBusy(true);
    setProblem(null);
    try {
      await api.putSchedule({
        // A stable id for an edit; a fresh one for a new row. Date.now is enough
        // — these are per-machine and never merged with anyone else's.
        id: existing?.id ?? `sched.${Date.now()}`,
        name: name.trim() || "Schedule",
        listIds: lists,
        start,
        end,
        enabled: existing?.enabled ?? true,
        days,
      });
      onDone();
    } catch (e) {
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="py-3">
      <div className="flex gap-2 mb-2">
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="Bedtime"
          className="flex-1 text-[12px] px-2 py-1 rounded"
          style={field}
        />
      </div>

      <div className="flex items-center gap-2 mb-2 text-[12px]">
        {/* Native time inputs: the platform already has a picker, a mask, and
            locale-correct formatting for this. */}
        <input
          type="time"
          value={start}
          onChange={(e) => setStart(e.target.value)}
          className="px-2 py-1 rounded tabular"
          style={field}
        />
        <span style={{ color: "var(--text-muted)" }}>to</span>
        <input
          type="time"
          value={end}
          onChange={(e) => setEnd(e.target.value)}
          className="px-2 py-1 rounded tabular"
          style={field}
        />
      </div>

      {/* Which days the window may START on. An overnight window belongs to the
          day it started, so this is not "which days am I blocked". */}
      <div className="flex gap-1 mb-2">
        {DAY_LABELS.map((label, i) => {
          const on = days.includes(i);
          return (
            <button
              key={i}
              type="button"
              aria-label={`day ${i}`}
              aria-pressed={on}
              onClick={() =>
                setDays(on ? days.filter((d) => d !== i) : [...days, i])
              }
              className="rounded-full text-[11px]"
              style={{
                width: 24,
                height: 24,
                border: on ? "none" : "0.5px solid var(--hairline)",
                background: on ? "var(--accent-tint)" : "transparent",
                color: on ? "var(--accent)" : "var(--text-muted)",
              }}
            >
              {label}
            </button>
          );
        })}
        <span className="text-[11px] self-center ml-1" style={{ color: "var(--text-muted)" }}>
          {days.length === 0 ? "every day" : "starts on these days"}
        </span>
      </div>

      <div className="flex gap-1 flex-wrap mb-2">
        {LISTS.map((id) => {
          const on = lists.includes(id);
          return (
            <button
              key={id}
              type="button"
              aria-pressed={on}
              onClick={() =>
                setLists(on ? lists.filter((l) => l !== id) : [...lists, id])
              }
              className="rounded-full text-[11px]"
              style={{
                padding: "3px 9px",
                border: on ? "none" : "0.5px solid var(--hairline)",
                background: on ? "var(--accent-tint)" : "transparent",
                color: on ? "var(--accent)" : "var(--text-muted)",
              }}
            >
              {listName(id)}
            </button>
          );
        })}
      </div>

      {problem && (
        <p className="text-[11px] mb-2" style={{ color: "var(--warning)" }}>
          {problem}
        </p>
      )}

      <div className="flex gap-3 text-[12px]">
        <button type="button" onClick={onCancel} style={{ color: "var(--text-muted)" }}>
          Cancel
        </button>
        <button
          type="button"
          onClick={save}
          disabled={busy || lists.length === 0}
          className="rounded-full disabled:opacity-40"
          style={{
            padding: "5px 11px",
            background: "var(--accent-tint)",
            color: "var(--accent)",
          }}
        >
          Save
        </button>
      </div>
    </div>
  );
}

const field = {
  background: "transparent",
  border: "0.5px solid var(--hairline)",
  color: "var(--text)",
} as const;

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e);
  switch (e.code) {
    case "would_weaken":
      return "This schedule is in force right now. Wait for the window to close, then edit it.";
    case "bad_day":
      return "That is not a day of the week.";
    default:
      // parseHHMM's message is more useful than anything restated here.
      return e.code;
  }
}
