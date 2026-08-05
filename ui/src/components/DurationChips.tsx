"use client";

export const DURATIONS = [25, 50, 120] as const;

interface Props {
  minutes: number;
  disabled: boolean;
  onPick: (minutes: number) => void;
}

/**
 * Pill row. The selected chip drops its border and takes an accent tint; the
 * others sit on a 0.5px border over nothing.
 *
 * Disabled once a session exists: duration is chosen when calm, not mid-session.
 */
export function DurationChips({ minutes, disabled, onPick }: Props) {
  return (
    <div className="flex gap-2">
      {DURATIONS.map((m) => {
        const selected = m === minutes;
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
            {m < 60 ? `${m}m` : `${m / 60}h`}
          </button>
        );
      })}
    </div>
  );
}
