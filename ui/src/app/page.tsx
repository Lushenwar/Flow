"use client";

import { baselineOnCount, dialText } from "@/lib/state";
import { usePoll } from "@/lib/use-poll";

/**
 * Focus screen. Phase 4 replaces this with the dial; until then it renders the
 * same derived text the dial will, so the daemon wiring is exercised now and the
 * only thing Phase 4 adds is geometry.
 */
export default function FocusScreen() {
  const { state } = usePoll();

  if (!state) {
    return <p className="text-[13px]" style={{ color: "var(--text-muted)" }}>Loading…</p>;
  }

  const text = dialText(state.session, baselineOnCount(state.baseline), 50);

  return (
    <div className="flex flex-col items-center gap-1 py-8">
      <span className="tabular text-[30px]">{text.countdown}</span>
      <span className="text-[12px]" style={{ color: "var(--text-secondary)" }}>
        {text.status}
      </span>
      {text.hint && (
        <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
          {text.hint}
        </span>
      )}
      {text.coverage && (
        <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
          {text.coverage}
        </span>
      )}
      <p className="text-[11px] mt-6" style={{ color: "var(--text-muted)" }}>
        The dial arrives in Phase 4.
      </p>
    </div>
  );
}
