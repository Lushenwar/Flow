"use client";

import { Lock } from "lucide-react";
import { useState } from "react";
import { CommitDialog } from "@/components/CommitDialog";
import { Dial } from "@/components/Dial";
import { DURATIONS, DurationChips } from "@/components/DurationChips";
import { EscapeDialog } from "@/components/EscapeDialog";
import {
  CycleChips,
  ModeChips,
  POMODORO_BREAK_MINUTES,
  type Mode,
} from "@/components/ModeChips";
import { PopOutTimer } from "@/components/PopOutTimer";
import { useIsDesktop } from "@/lib/desktop";
import { api } from "@/lib/flow-client";
import {
  arcFraction,
  baselineOnCount,
  dialAction,
  dialText,
  listName,
} from "@/lib/state";
import { useNow, usePoll } from "@/lib/use-poll";

/** What a session covers by default. Editing lives in settings, not here. */
const SESSION_LISTS = ["preset.video", "preset.doomscroll", "preset.gaming"];
const ESCAPE_MINUTES = 15;

export default function FocusScreen() {
  const { state, refresh } = usePoll();
  const now = useNow();
  // Before the loading return: hook order cannot depend on whether state arrived.
  const isDesktop = useIsDesktop();
  const [minutes, setMinutes] = useState<number>(DURATIONS[1]);
  const [mode, setMode] = useState<Mode>("commitment");
  const [cycles, setCycles] = useState(4);
  const [dialogOpen, setDialogOpen] = useState(false);
  const [escapeOpen, setEscapeOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  if (!state) {
    return <p className="text-[13px]" style={{ color: "var(--text-muted)" }}>Loading…</p>;
  }

  const session = state.session;
  const action = dialAction(session);
  const text = dialText(session, baselineOnCount(state.baseline), minutes, now);

  async function tap() {
    if (action === "commit") {
      setDialogOpen(true);
      return;
    }
    if (action === "abort") {
      // Free and instant, and only possible in ARMING. The daemon is the one
      // that decides that; a 409 here just means the window closed.
      setBusy(true);
      try {
        await api.abort();
      } finally {
        setBusy(false);
        refresh();
      }
    }
    // FOCUS and RELEASING: no action, no control to render.
  }

  async function commit(acceptTamperPenalty: boolean) {
    setDialogOpen(false);
    setBusy(true);
    try {
      await api.commit({
        mode,
        // In pomodoro this is one interval, not the total.
        durationMinutes: minutes,
        blocklistIds: SESSION_LISTS,
        acceptTamperPenalty,
        ...(mode === "pomodoro"
          ? { cycles, breakMinutes: POMODORO_BREAK_MINUTES }
          : {}),
      });
    } finally {
      setBusy(false);
      refresh();
    }
  }

  const covers = session.blocklistIds.length > 0 ? session.blocklistIds : SESSION_LISTS;

  return (
    <div className="flex flex-col items-center">
      <Dial
        text={text}
        fraction={arcFraction(session)}
        action={busy ? "none" : action}
        neutral={session.state === "COMPLETE"}
        onTap={tap}
      />

      <div className="mt-5 flex flex-col items-center gap-2">
        <ModeChips
          mode={mode}
          disabled={action !== "commit" || busy}
          onPick={setMode}
        />
        <div className="flex gap-2">
          <DurationChips
            minutes={minutes}
            disabled={action !== "commit" || busy}
            onPick={setMinutes}
          />
          {mode === "pomodoro" && (
            <CycleChips
              cycles={cycles}
              disabled={action !== "commit" || busy}
              onPick={setCycles}
            />
          )}
        </div>
        {mode === "pomodoro" && action === "commit" && (
          <p className="text-[11px]" style={{ color: "var(--text-muted)" }}>
            {cycles} × {minutes}m focus, {POMODORO_BREAK_MINUTES}m breaks
          </p>
        )}
      </div>

      {/* Read-only. Choosing a session's blocklist here would be a bypass: a
          user mid-craving would deselect YouTube and commit to nothing. */}
      <p
        className="text-[11px] mt-4 text-center"
        style={{ color: "var(--text-muted)" }}
      >
        Covers {covers.map(listName).join(", ").toLowerCase()}
      </p>

      {/* The consequence sits directly under the control that causes it. */}
      <div
        className="w-full mt-5 pt-3 flex items-center justify-center gap-[6px] text-[12px]"
        style={{ borderTop: "0.5px solid var(--hairline)", color: "var(--text-muted)" }}
      >
        <Lock size={12} />
        no off switch until 0:00
      </div>

      {/* The escape hatch is always reachable while locked. Understated, not
          hidden: hiding it would make it a trap, foregrounding it would make it
          the obvious next click. */}
      {(session.state === "FOCUS" || session.state === "RELEASING") && (
        <button
          type="button"
          onClick={async () => {
            if (!session.escape.requested) {
              setBusy(true);
              try {
                await api.escape();
              } finally {
                setBusy(false);
                refresh();
              }
            }
            setEscapeOpen(true);
          }}
          className="text-[11px] mt-3 underline underline-offset-2"
          style={{ color: "var(--text-muted)" }}
        >
          {session.escape.requested ? "ending early…" : "end early"}
        </button>
      )}

      {/* The same dial in a window the OS keeps on top. Inert on purpose: it is
          there to show you the countdown, not to be a second place to commit or
          abort from.

          Browser only. The desktop shell has mini mode, which is the same idea
          without needing a tab to stay open behind it, so offering both would be
          two buttons for one job. */}
      {!isDesktop && (
      <PopOutTimer>
        {(pipNow) => (
          <Dial
            // Its own clock, from the pop-out's window rather than this page's —
            // see PopOutTimer. The hint goes with the tap: "tap to commit" over
            // a dial that does nothing is the same contradiction FOCUS avoids by
            // dropping it.
            text={{
              ...dialText(session, baselineOnCount(state.baseline), minutes, pipNow),
              hint: undefined,
            }}
            fraction={arcFraction(session)}
            action="none"
            neutral={session.state === "COMPLETE"}
            onTap={() => {}}
          />
        )}
      </PopOutTimer>
      )}

      {dialogOpen && (
        <CommitDialog
          minutes={minutes}
          blocklistIds={SESSION_LISTS}
          escapeMinutes={ESCAPE_MINUTES}
          cycles={mode === "pomodoro" ? cycles : undefined}
          breakMinutes={mode === "pomodoro" ? POMODORO_BREAK_MINUTES : undefined}
          onCancel={() => setDialogOpen(false)}
          onConfirm={commit}
        />
      )}

      {escapeOpen && (
        <EscapeDialog
          session={session}
          now={now}
          onClose={() => setEscapeOpen(false)}
          onReleased={() => {
            setEscapeOpen(false);
            refresh();
          }}
        />
      )}
    </div>
  );
}
