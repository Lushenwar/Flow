"use client";

import { useState } from "react";
import { Modal } from "@/components/Modal";
import { listName } from "@/lib/state";

interface Props {
  minutes: number;
  blocklistIds: string[];
  escapeMinutes: number;
  /** Pomodoro only. Omitted for a single commitment block. */
  cycles?: number;
  breakMinutes?: number;
  onCancel: () => void;
  onConfirm: (acceptTamperPenalty: boolean) => void;
}

/**
 * Everything the user is agreeing to, stated plainly and before it binds. This
 * is the only place the terms are disclosed, so nothing here is softened: the
 * tamper penalty in particular must be opt-in and pre-disclosed, or a dead CMOS
 * battery becomes an unescapable lock on someone's own computer.
 */
export function CommitDialog({
  minutes,
  blocklistIds,
  escapeMinutes,
  cycles,
  breakMinutes,
  onCancel,
  onConfirm,
}: Props) {
  const [penalty, setPenalty] = useState(false);
  const pomodoro = !!cycles && cycles > 1;

  return (
    <Modal onClose={onCancel} label="Confirm this session">
      <>
        <p className="text-[14px] mb-3">
          {pomodoro
            ? `Commit to ${cycles} × ${minutes} minutes?`
            : `Commit to ${minutes} minutes?`}
        </p>

        <dl className="text-[12px] space-y-2 mb-4" style={{ color: "var(--text-secondary)" }}>
          {pomodoro && (
            <div>
              <dt className="inline">Shape: </dt>
              <dd className="inline">
                {cycles} locked intervals of {minutes} minutes, with{" "}
                {breakMinutes}-minute breaks between them. Each interval locks on
                its own; the breaks do not.
              </dd>
            </div>
          )}
          <div>
            <dt className="inline">Blocks: </dt>
            <dd className="inline">
              {blocklistIds.length === 0
                ? "baseline only"
                : blocklistIds.map(listName).join(", ")}
            </dd>
          </div>
          <div>
            <dt className="inline">Stopping: </dt>
            <dd className="inline">
              There is no off switch{pomodoro ? " inside an interval" : ""}. Ending
              early takes {escapeMinutes} minutes, and everything stays blocked
              while you wait.
              {pomodoro && " Between intervals you can stop for free."}
            </dd>
          </div>
          <div>
            <dt className="inline">Backing out: </dt>
            <dd className="inline">
              You have 15 seconds after committing. Blocks are live immediately.
              {pomodoro && " The later intervals start without a grace window."}
            </dd>
          </div>
        </dl>

        <label className="flex items-start gap-2 text-[12px] mb-4">
          <input
            type="checkbox"
            checked={penalty}
            onChange={(e) => setPenalty(e.target.checked)}
            className="mt-[2px]"
          />
          <span style={{ color: "var(--text-secondary)" }}>
            Add 15 minutes if the clock is tampered with (once per session, at most).
          </span>
        </label>

        <div className="flex justify-end gap-3 text-[12px]">
          <button type="button" onClick={onCancel} style={{ color: "var(--text-muted)" }}>
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onConfirm(penalty)}
            className="rounded-full"
            style={{
              padding: "5px 11px",
              background: "var(--accent-tint)",
              color: "var(--accent)",
            }}
          >
            Commit
          </button>
        </div>
      </>
    </Modal>
  );
}
