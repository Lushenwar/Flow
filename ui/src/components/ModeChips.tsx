"use client";

export type Mode = "commitment" | "pomodoro";

/** The classic interval. Configurable would be a settings screen for a decision
 *  nobody has asked to make twice. */
export const POMODORO_BREAK_MINUTES = 5;
export const POMODORO_CYCLES = [2, 4, 6] as const;

interface Props {
  mode: Mode;
  disabled: boolean;
  onPick: (mode: Mode) => void;
}

/**
 * Two words above the duration row. Same pill styling as DurationChips, because
 * they are the same kind of choice made at the same moment.
 *
 * Disabled once a session exists, for the same reason the durations are: this is
 * chosen when calm, not mid-session.
 */
export function ModeChips({ mode, disabled, onPick }: Props) {
  return (
    <div className="flex gap-2">
      {(["commitment", "pomodoro"] as const).map((m) => {
        const selected = m === mode;
        return (
          <button
            key={m}
            type="button"
            disabled={disabled}
            onClick={() => onPick(m)}
            className="rounded-full text-[12px] disabled:opacity-40"
            style={{
              padding: "5px 11px",
              border: selected ? "none" : "0.5px solid var(--hairline)",
              background: selected ? "var(--accent-tint)" : "transparent",
              color: selected ? "var(--accent)" : "var(--text-secondary)",
            }}
          >
            {m === "commitment" ? "one block" : "pomodoro"}
          </button>
        );
      })}
    </div>
  );
}

/** How many intervals. Only rendered in pomodoro mode. */
export function CycleChips({
  cycles,
  disabled,
  onPick,
}: {
  cycles: number;
  disabled: boolean;
  onPick: (cycles: number) => void;
}) {
  return (
    <div className="flex gap-2">
      {POMODORO_CYCLES.map((n) => {
        const selected = n === cycles;
        return (
          <button
            key={n}
            type="button"
            disabled={disabled}
            onClick={() => onPick(n)}
            className="rounded-full text-[12px] disabled:opacity-40"
            style={{
              padding: "5px 11px",
              border: selected ? "none" : "0.5px solid var(--hairline)",
              background: selected ? "var(--accent-tint)" : "transparent",
              color: selected ? "var(--accent)" : "var(--text-secondary)",
            }}
          >
            ×{n}
          </button>
        );
      })}
    </div>
  );
}
