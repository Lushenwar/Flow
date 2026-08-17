"use client";

import { useState } from "react";
import { ApiError, api } from "@/lib/flow-client";
import { bankLabel, formatCountdown, type BankView } from "@/lib/state";

/**
 * Earned recreation time, and the one control in the app that lifts enforcement
 * without waiting out a delay.
 *
 * Everything about it is deliberately hemmed in, and all of it is the daemon's
 * doing rather than this component's: IDLE only, deducted up front, no cancel.
 * What this file owes the user is saying so BEFORE the button is pressed, the
 * same way CommitDialog does — "spend 30, use 2, refund 28" is an off switch
 * with extra steps, and the reason it is impossible should not be a surprise
 * discovered afterward.
 *
 * Baseline is NOT lifted by a spend. Focus minutes buy back the things you
 * avoid for productivity, never the things you asked to be permanently
 * protected from.
 */
export function BankRow({
  bank,
  idle,
  onSpent,
}: {
  bank: BankView | null;
  /** The daemon refuses a spend outside IDLE. The UI must not decide this. */
  idle: boolean;
  onSpent: () => void;
}) {
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [confirming, setConfirming] = useState<number | null>(null);

  if (!bank) return null;

  const wholeMinutes = Math.floor(bank.balanceSeconds / 60);
  // Only offer amounts that are actually affordable. A disabled row of chips
  // teaches nothing that the balance line above it has not already said.
  const options = [5, 10, 15, 30].filter((m) => m <= wholeMinutes);

  async function spend(minutes: number) {
    setBusy(true);
    setProblem(null);
    try {
      await api.spend(minutes);
      setConfirming(null);
      onSpent();
    } catch (e) {
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <p className="text-[12px] mb-1" style={{ color: "var(--text-secondary)" }}>
        Recreation time
      </p>
      <p className="text-[11px] mb-2" style={{ color: "var(--text-muted)" }}>
        {bankLabel(bank)}
      </p>

      {bank.spending && (
        <p className="tabular text-[12px]" style={{ color: "var(--accent)" }}>
          {formatCountdown(bank.remainingSeconds)} left · locks again on its own
        </p>
      )}

      {!bank.spending && options.length > 0 && idle && confirming === null && (
        <div className="flex gap-2 flex-wrap">
          {options.map((m) => (
            <button
              key={m}
              type="button"
              disabled={busy}
              onClick={() => setConfirming(m)}
              className="rounded-full text-[12px] disabled:opacity-40"
              style={{
                padding: "5px 11px",
                border: "0.5px solid var(--hairline)",
                color: "var(--text-secondary)",
              }}
            >
              take {m}m
            </button>
          ))}
        </div>
      )}

      {/* The terms, before it binds. There is no cancel after this. */}
      {confirming !== null && (
        <div className="text-[11px]" style={{ color: "var(--text-secondary)" }}>
          <p className="mb-2">
            Spend {confirming} of your {wholeMinutes} banked minutes. Blocking
            lifts for {confirming} minutes and then locks again on its own. You
            cannot stop it early, and unused minutes are not returned.
          </p>
          <div className="flex gap-3">
            <button
              type="button"
              onClick={() => setConfirming(null)}
              style={{ color: "var(--text-muted)" }}
            >
              Cancel
            </button>
            <button
              type="button"
              disabled={busy}
              onClick={() => spend(confirming)}
              className="rounded-full disabled:opacity-40"
              style={{
                padding: "5px 11px",
                background: "var(--accent-tint)",
                color: "var(--accent)",
              }}
            >
              Take {confirming} minutes
            </button>
          </div>
        </div>
      )}

      {!bank.spending && options.length > 0 && !idle && (
        <p className="text-[11px]" style={{ color: "var(--text-muted)" }}>
          Spending needs a finished session — not during one.
        </p>
      )}

      {problem && (
        <p className="text-[11px] mt-2" style={{ color: "var(--warning)" }}>
          {problem}
        </p>
      )}
    </div>
  );
}

/** The daemon's refusals are answers, not failures. */
function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e);
  switch (e.code) {
    case "not_idle":
      return "You can only spend recreation time between sessions.";
    case "spend_active":
      return "There is already a window open.";
    case "insufficient_balance":
      return "That is more than you have banked.";
    default:
      return e.code;
  }
}
