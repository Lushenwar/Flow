"use client";

import { useEffect, useState } from "react";
import { api } from "@/lib/flow-client";
import { eventLabel, visibleEvents, type EventRow } from "@/lib/state";

/**
 * Drift events, reconciliation repairs, and escapes. It lives on the Blocking
 * screen rather than a third tab: two screens is a hard constraint, and this is
 * a list, not a control.
 */
export function History({ limit = 8 }: { limit?: number }) {
  const [events, setEvents] = useState<EventRow[]>([]);

  useEffect(() => {
    const load = () => api.events().then(setEvents).catch(() => {});
    const first = setTimeout(load, 0);
    const t = setInterval(load, 5000);
    return () => {
      clearTimeout(first);
      clearInterval(t);
    };
  }, []);

  const rows = visibleEvents(events).slice(0, limit);
  if (rows.length === 0) return null;

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <p className="text-[12px] mb-2" style={{ color: "var(--text-secondary)" }}>
        Recent activity
      </p>
      <ul className="space-y-1">
        {rows.map((e) => (
          <li key={e.id} className="flex justify-between gap-3 text-[11px]">
            <span style={{ color: "var(--text-muted)" }}>{eventLabel(e.kind)}</span>
            <span className="tabular shrink-0" style={{ color: "var(--text-muted)" }}>
              {new Date(e.ts).toLocaleTimeString([], {
                hour: "2-digit",
                minute: "2-digit",
              })}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}
