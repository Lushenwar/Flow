"use client";

import { useEffect, useState } from "react";
import { AutostartRow } from "@/components/AutostartRow";
import { BankRow } from "@/components/BankRow";
import { BaselineRowView, SessionOwnedRow } from "@/components/BaselineRow";
import { CustomSites } from "@/components/CustomSites";
import { History } from "@/components/History";
import { ScheduleEditor } from "@/components/ScheduleEditor";
import { ScheduleRowView } from "@/components/ScheduleRow";
import { ApiError, api } from "@/lib/flow-client";
import {
  sessionOwnedIds,
  type BankView,
  type ScheduleRow,
} from "@/lib/state";
import { useNow, usePoll } from "@/lib/use-poll";

export default function BlockingScreen() {
  const { state, error, refresh } = usePoll();
  const now = useNow();
  const [busy, setBusy] = useState<string | null>(null);
  const [refused, setRefused] = useState<string | null>(null);
  const [schedules, setSchedules] = useState<ScheduleRow[]>([]);
  const [bank, setBank] = useState<BankView | null>(null);
  // "new" opens a blank form; an id opens that row for editing.
  const [editing, setEditing] = useState<string | null>(null);

  useEffect(() => {
    const load = () => {
      api.schedules().then(setSchedules).catch(() => {});
      api.bank().then(setBank).catch(() => {});
    };
    const first = setTimeout(load, 0);
    const t = setInterval(load, 5000);
    return () => {
      clearTimeout(first);
      clearInterval(t);
    };
  }, []);

  async function run(id: string, fn: () => Promise<unknown>) {
    setBusy(id);
    setRefused(null);
    try {
      await fn();
    } catch (e) {
      // The UI never greys out a control because it decided to. It shows what
      // the daemon refused and why.
      setRefused(
        e instanceof ApiError && e.code === "would_weaken"
          ? "Can't turn this off during a session."
          : String(e),
      );
    } finally {
      setBusy(null);
      refresh();
    }
  }

  // The caption renders before the rows do, and before any data arrives. The
  // rule is stated before the user touches a switch, not discovered afterward.
  const caption = (
    <p className="text-[12px] mb-3" style={{ color: "var(--text-secondary)" }}>
      Always on, no timer. Off takes 15 minutes.
    </p>
  );

  if (error && !state) {
    return (
      <>
        {caption}
        <p className="text-[13px]" style={{ color: "var(--text-muted)" }}>
          Can&apos;t reach the daemon. Enforcement is unaffected — this window is
          only a view.
        </p>
      </>
    );
  }
  if (!state) {
    return (
      <>
        {caption}
        <p className="text-[13px]" style={{ color: "var(--text-muted)" }}>
          Loading…
        </p>
      </>
    );
  }

  const owned = sessionOwnedIds(state);

  return (
    <>
      {caption}

      <div>
        {state.baseline.map((row, i) => (
          <BaselineRowView
            key={row.id}
            row={row}
            now={now}
            last={i === state.baseline.length - 1 && owned.length === 0}
            busy={busy === row.id}
            onEnable={() => run(row.id, () => api.enable(row.id))}
            onDisable={() => run(row.id, () => api.disable(row.id))}
            onCancel={() => run(row.id, () => api.cancelDisable(row.id))}
          />
        ))}

        {owned.map((id, i) => (
          <SessionOwnedRow
            key={id}
            id={id}
            remainingSeconds={state.session.remainingSeconds}
            last={i === owned.length - 1}
          />
        ))}
      </div>

      {refused && (
        <p className="text-[12px] mt-3" style={{ color: "var(--warning)" }}>
          {refused}
        </p>
      )}

      {/* Adding a site changes the baseline rows too — the list arrives as a
          rule with its own switch — so a change here has to refresh the poll. */}
      <CustomSites onChanged={refresh} />

      <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
        <p className="text-[12px] mb-1" style={{ color: "var(--text-secondary)" }}>
          Schedules
        </p>
        {schedules.map((s, i) => (
          <ScheduleRowView
            key={s.id}
            row={s}
            last={i === schedules.length - 1 && !editing}
            busy={busy === s.id}
            onToggle={(enabled) =>
              run(s.id, async () => {
                await api.putSchedule({ ...s, enabled });
                setSchedules(await api.schedules());
              })
            }
            onEdit={() => setEditing(s.id)}
            onDelete={() =>
              run(s.id, async () => {
                await api.deleteSchedule(s.id);
                setSchedules(await api.schedules());
              })
            }
          />
        ))}

        {editing ? (
          <ScheduleEditor
            existing={schedules.find((s) => s.id === editing)}
            onCancel={() => setEditing(null)}
            onDone={async () => {
              setEditing(null);
              setSchedules(await api.schedules());
              refresh();
            }}
          />
        ) : (
          <button
            type="button"
            onClick={() => setEditing("new")}
            className="text-[11px] mt-2 underline underline-offset-2"
            style={{ color: "var(--text-muted)" }}
          >
            add a schedule
          </button>
        )}
      </div>

      {/* idle comes from the daemon's state, never from a local guess: a spend
          is refused outside IDLE and the UI only reports that, it does not
          decide it. */}
      <BankRow
        bank={bank}
        idle={state.session.state === "IDLE" || state.session.state === "COMPLETE"}
        onSpent={() => {
          api.bank().then(setBank).catch(() => {});
          refresh();
        }}
      />

      {/* Renders nothing in a browser: there is no window to open at login. */}
      <AutostartRow />

      <History />
    </>
  );
}
