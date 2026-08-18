"use client";

import { useEffect, useState } from "react";
import { ApiError, api } from "@/lib/flow-client";
import { listName } from "@/lib/state";

/**
 * What a session covers.
 *
 * This lives on the BLOCKING screen, deliberately, and never on the dial:
 *
 *   Choosing a session's blocklist on the dial would be a bypass: a user
 *   mid-craving would deselect YouTube and commit to a session that blocks
 *   nothing. Session composition lives in a settings sub-screen, edited when
 *   calm, not at the moment of commitment.
 *
 * "Edited when calm" is enforced by the daemon rather than by where the control
 * is drawn — PUT /api/session/lists is a 409 while a session is running. A
 * settings screen you can open mid-craving is the dial with extra clicks.
 */
const CHOICES = [
  "preset.video",
  "preset.doomscroll",
  "preset.gaming",
  "preset.shopping",
  "preset.delivery",
  "preset.study",
  "preset.work",
];

export function SessionLists({ locked }: { locked: boolean }) {
  const [ids, setIds] = useState<string[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api
      .sessionLists()
      .then((r) => setIds(r.listIds))
      .catch(() => {});
  }, []);

  if (!ids) return null;

  async function toggle(id: string) {
    const next = ids!.includes(id) ? ids!.filter((x) => x !== id) : [...ids!, id];
    if (next.length === 0) {
      setProblem("A session has to block something.");
      return;
    }
    setBusy(true);
    setProblem(null);
    // Optimistic, then corrected by what the daemon actually stored — it sorts,
    // and it can refuse outright.
    setIds(next);
    try {
      const saved = await api.putSessionLists(next);
      setIds(saved.listIds);
    } catch (e) {
      setIds(ids);
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <p className="text-[12px] mb-1" style={{ color: "var(--text-secondary)" }}>
        What a focus session blocks
      </p>
      <p className="text-[11px] mb-2" style={{ color: "var(--text-muted)" }}>
        {locked
          ? "Locked while a session is running. This is chosen when you are calm, not mid-session."
          : "Applies to the next session you start."}
      </p>

      <div className="flex gap-1 flex-wrap">
        {CHOICES.map((id) => {
          const on = ids.includes(id);
          return (
            <button
              key={id}
              type="button"
              aria-pressed={on}
              disabled={busy || locked}
              onClick={() => toggle(id)}
              className="rounded-full text-[11px] disabled:opacity-40"
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
        <p className="text-[11px] mt-2" style={{ color: "var(--warning)" }}>
          {problem}
        </p>
      )}
    </div>
  );
}

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e);
  switch (e.code) {
    case "would_weaken":
      return "Not while a session is running. That is the point of choosing it beforehand.";
    case "no_lists":
      return "A session has to block something.";
    default:
      return e.code;
  }
}
