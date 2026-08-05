"use client";

import { Clock } from "lucide-react";
import { listName, pendingLabel, rowState, type BaselineRow as Row } from "@/lib/state";

interface Props {
  row: Row;
  now: number;
  last: boolean;
  busy: boolean;
  onEnable: () => void;
  onDisable: () => void;
  onCancel: () => void;
}

/** 34×20 pill with a 16px knob inset 2px. */
function Switch({
  on,
  pending,
  disabled,
  onClick,
}: {
  on: boolean;
  pending: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      onClick={onClick}
      className="relative rounded-full transition-colors disabled:opacity-50"
      style={{
        width: 34,
        height: 20,
        background: on ? "var(--accent)" : "var(--hairline)",
        // A pending disable is visually distinct but must NOT read as off: the
        // rule is fully enforced for the whole countdown.
        opacity: pending ? 0.55 : 1,
        outline: pending ? "1.5px dashed var(--warning)" : "none",
        outlineOffset: 2,
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
  );
}

export function BaselineRowView({
  row,
  now,
  last,
  busy,
  onEnable,
  onDisable,
  onCancel,
}: Props) {
  const state = rowState(row);

  return (
    <div
      className="flex items-center justify-between py-[10px]"
      style={{ borderBottom: last ? "none" : "0.5px solid var(--hairline)" }}
    >
      <span className="text-[14px]">{listName(row.id)}</span>

      <div className="flex items-center gap-3">
        {state === "pending" && (
          <span className="text-[12px]" style={{ color: "var(--warning)" }}>
            {pendingLabel(row, now)}
            {" · "}
            <button
              type="button"
              onClick={onCancel}
              className="underline underline-offset-2"
            >
              cancel
            </button>
          </span>
        )}
        <Switch
          on={row.enabled}
          pending={state === "pending"}
          disabled={busy}
          onClick={state === "pending" ? onCancel : row.enabled ? onDisable : onEnable}
        />
      </div>
    </div>
  );
}

/**
 * A session-owned rule appears in the list so you can see it is enforced, but it
 * has no switch — you do not control it right now. Attribution made visible.
 */
export function SessionOwnedRow({
  id,
  remainingSeconds,
  last,
}: {
  id: string;
  remainingSeconds: number;
  last: boolean;
}) {
  const minutes = Math.ceil(remainingSeconds / 60);
  return (
    <div
      className="flex items-center justify-between py-[10px]"
      style={{ borderBottom: last ? "none" : "0.5px solid var(--hairline)" }}
    >
      <span className="text-[14px]" style={{ color: "var(--text-muted)" }}>
        {listName(id)} — in this session
      </span>
      <span
        className="flex items-center gap-1 text-[12px] tabular"
        style={{ color: "var(--text-muted)" }}
      >
        <Clock size={12} />
        {minutes}m
      </span>
    </div>
  );
}
