"use client";

import { useState } from "react";
import { BaselineRowView, SessionOwnedRow } from "@/components/BaselineRow";
import { ApiError, api } from "@/lib/mast-client";
import { sessionOwnedIds } from "@/lib/state";
import { useNow, usePoll } from "@/lib/use-poll";

export default function BlockingScreen() {
  const { state, error, refresh } = usePoll();
  const now = useNow();
  const [busy, setBusy] = useState<string | null>(null);
  const [refused, setRefused] = useState<string | null>(null);

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
    </>
  );
}
