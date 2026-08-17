"use client";

import { Maximize2 } from "lucide-react";
import { useRouter } from "next/navigation";
import { Dial } from "@/components/Dial";
import { desktop } from "@/lib/desktop";
import { arcFraction, baselineOnCount, dialText } from "@/lib/state";
import { useNow, usePoll } from "@/lib/use-poll";

/**
 * The sticky-note timer: the dial, pinned above everything, and nothing else.
 *
 * Inert on purpose. This is a thing you glance at while working in another
 * window, not a second place to commit or abort from — the hint line is dropped
 * for the same reason FOCUS drops it, because a prompt to tap over a dial that
 * does nothing contradicts itself.
 */
export default function MiniTimer() {
  const { state } = usePoll();
  const now = useNow();
  const router = useRouter();

  async function restore() {
    router.push("/");
    await desktop()?.SetMini(false);
  }

  return (
    <div className="flex h-screen flex-col">
      {/* The whole strip drags the widget; only the button does not. */}
      <div
        className="flex items-center justify-between px-2 pt-2 select-none"
        style={{ "--wails-draggable": "drag" } as React.CSSProperties}
      >
        <span className="text-[10px]" style={{ color: "var(--text-muted)" }}>
          Flow
        </span>
        <button
          type="button"
          aria-label="Back to the full window"
          title="Back to the full window"
          onClick={restore}
          className="rounded p-1 transition-colors hover:bg-[var(--hairline)]"
          style={
            {
              color: "var(--text-muted)",
              "--wails-draggable": "no-drag",
            } as React.CSSProperties
          }
        >
          <Maximize2 size={12} />
        </button>
      </div>

      <div className="flex flex-1 items-center justify-center">
        {state ? (
          <Dial
            text={{
              ...dialText(
                state.session,
                baselineOnCount(state.baseline),
                0,
                now,
              ),
              hint: undefined,
            }}
            fraction={arcFraction(state.session)}
            action="none"
            neutral={state.session.state === "COMPLETE"}
            onTap={() => {}}
          />
        ) : (
          <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
            …
          </span>
        )}
      </div>
    </div>
  );
}
