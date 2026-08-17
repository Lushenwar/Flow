"use client";

import { useEffect, useState } from "react";
import { desktop, useIsDesktop } from "@/lib/desktop";

/**
 * Open the window at login. Desktop shell only — a browser tab cannot do this,
 * and there is nothing to fake.
 *
 * This controls the WINDOW and nothing else. The daemon is a service registered
 * Automatic and starts itself whatever this says; if it were switchable from
 * here it would be an off switch with a friendly label.
 *
 * It ships off. A blocker that adds itself to startup without being asked, and
 * then hides the switch, is behaving like the software this app exists to be an
 * alternative to. The entry is a plain HKCU Run key, so it also shows up in Task
 * Manager's Startup tab and can be removed there without touching Flow.
 */
export function AutostartRow() {
  const isDesktop = useIsDesktop();
  const [on, setOn] = useState<boolean | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!isDesktop) return;
    desktop()
      ?.AutostartEnabled()
      .then(setOn)
      .catch(() => setOn(false));
  }, [isDesktop]);

  // Not desktop, or we have not heard back yet: render nothing rather than a
  // switch in a guessed position.
  if (!isDesktop || on === null) return null;

  async function toggle() {
    const next = !on;
    setBusy(true);
    try {
      await desktop()?.SetAutostart(next);
      // Read it back rather than trusting the write: the registry is the truth,
      // and a checkbox that shows what we asked for instead of what happened is
      // the kind of lie that takes an hour to notice.
      setOn((await desktop()?.AutostartEnabled()) ?? next);
    } catch {
      setOn(await desktop()?.AutostartEnabled().catch(() => on) ?? on);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <div className="flex items-center justify-between py-[10px]">
        <span className="flex flex-col">
          <span className="text-[14px]">Open Flow at login</span>
          <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
            Starts minimised. Blocking runs either way — the daemon is a service.
          </span>
        </span>
        <button
          type="button"
          role="switch"
          aria-checked={on}
          aria-label="Open Flow at login"
          disabled={busy}
          onClick={toggle}
          className="relative rounded-full transition-colors disabled:opacity-50"
          style={{
            width: 34,
            height: 20,
            flexShrink: 0,
            background: on ? "var(--accent)" : "var(--hairline)",
          }}
        >
          <span
            className="absolute rounded-full bg-white transition-transform"
            style={{
              width: 16,
              height: 16,
              top: 2,
              left: 2,
              transform: on ? "translateX(14px)" : "translateX(0)",
            }}
          />
        </button>
      </div>
    </div>
  );
}
