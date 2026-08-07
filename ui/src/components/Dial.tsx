"use client";

import {
  DIAL_CIRCUMFERENCE,
  DIAL_RADIUS,
  dashArray,
  type DialAction,
  type DialText,
} from "@/lib/state";

interface Props {
  text: DialText;
  fraction: number;
  action: DialAction;
  /** COMPLETE draws the ring in a neutral tone: nothing is being enforced by it. */
  neutral: boolean;
  onTap: () => void;
}

const SIZE = 180;
const CENTER = SIZE / 2;
const STROKE = 8;

/**
 * One control, one tap target, three visual states.
 *
 * The arc starts at twelve o'clock, which is what the −90° rotation is for.
 * Circumference is 2πr ≈ 477.5, so the dasharray is [filled] [circumference].
 */
export function Dial({ text, fraction, action, neutral, onTap }: Props) {
  const inert = action === "none";
  const arcColor = neutral ? "var(--text-muted)" : "var(--accent)";

  return (
    <button
      type="button"
      onClick={inert ? undefined : onTap}
      aria-label={`${text.status}. ${text.countdown} remaining.`}
      aria-disabled={inert}
      className="relative block"
      style={{ width: SIZE, height: SIZE, cursor: inert ? "default" : "pointer" }}
    >
      <svg width={SIZE} height={SIZE} viewBox={`0 0 ${SIZE} ${SIZE}`}>
        <circle
          cx={CENTER}
          cy={CENTER}
          r={DIAL_RADIUS}
          fill="none"
          stroke="var(--hairline)"
          strokeWidth={STROKE}
        />
        <circle
          cx={CENTER}
          cy={CENTER}
          r={DIAL_RADIUS}
          fill="none"
          stroke={arcColor}
          strokeWidth={STROKE}
          // A round cap on a zero-length dash still paints: an accent dot at
          // twelve o'clock in IDLE, where the spec says the arc is empty.
          strokeLinecap={fraction > 0 ? "round" : "butt"}
          strokeDasharray={dashArray(fraction)}
          transform={`rotate(-90 ${CENTER} ${CENTER})`}
          style={{ transition: "stroke-dasharray 240ms linear" }}
        />
        {/* Keeps the dasharray honest if the constant ever drifts from the radius. */}
        <desc>{`circumference ${DIAL_CIRCUMFERENCE.toFixed(1)}`}</desc>
      </svg>

      <span className="absolute inset-0 flex flex-col items-center justify-center gap-1">
        <span className="tabular text-[30px] leading-none">{text.countdown}</span>
        <span className="text-[12px]" style={{ color: "var(--text-secondary)" }}>
          {text.status}
        </span>
        {/* Absent in FOCUS on purpose: a hint left over from IDLE contradicts
            the state directly above it. */}
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
      </span>
    </button>
  );
}
