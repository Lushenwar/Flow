"use client";

import { useEffect, useState } from "react";
import { Modal } from "@/components/Modal";
import { ApiError, api } from "@/lib/flow-client";
import { formatCountdown, secondsUntil, type SessionView } from "@/lib/state";

interface Props {
  session: SessionView;
  now: number;
  onClose: () => void;
  onReleased: () => void;
}

/**
 * The escape hatch. It always exists, cannot be disabled by any setting, and
 * the delay cannot be shortened — this dialog only shows what is already true.
 *
 * Copy is deliberately flat. "Session ended early", not "You failed": an escape
 * hatch people are ashamed to use is a trap with extra steps.
 */
export function EscapeDialog({ session, now, onClose, onReleased }: Props) {
  const [challenge, setChallenge] = useState<{ id: string; text: string } | null>(null);
  const [typed, setTyped] = useState("");
  const [mismatch, setMismatch] = useState(false);
  const remaining = secondsUntil(session.escape.availableAt, now);
  const ready = session.escape.requested && remaining === 0;

  useEffect(() => {
    if (!ready || challenge) return;
    // Refused before the delay elapses, so this can only succeed once the wait
    // is genuinely over.
    api.challenge().then(setChallenge).catch(() => {});
  }, [ready, challenge]);

  async function submit() {
    if (!challenge) return;
    setMismatch(false);
    try {
      await api.verify(challenge.id, typed);
      onReleased();
    } catch (e) {
      if (e instanceof ApiError && e.code === "challenge_mismatch") {
        setMismatch(true);
        setTyped("");
      }
    }
  }

  return (
    <Modal onClose={onClose} label="End this session early">
      <>
        <p className="text-[14px] mb-3">End this session early</p>

        {!ready ? (
          <>
            <p className="text-[12px] mb-2" style={{ color: "var(--text-secondary)" }}>
              Everything stays blocked until the countdown ends. You can close this
              window; the wait continues either way.
            </p>
            <p className="tabular text-[30px] my-4 text-center">
              {formatCountdown(remaining)}
            </p>
          </>
        ) : (
          <>
            <p className="text-[12px] mb-3" style={{ color: "var(--text-secondary)" }}>
              Type this exactly, including capitals.
            </p>
            <p
              className="tabular text-[12px] leading-5 mb-3 break-all select-none"
              style={{ color: "var(--text)" }}
            >
              {challenge?.text ?? "…"}
            </p>
            <textarea
              value={typed}
              onChange={(e) => setTyped(e.target.value)}
              // Friction, not security: the daemon cannot know how the
              // characters arrived, so this only removes the obvious shortcut.
              onPaste={(e) => e.preventDefault()}
              rows={3}
              spellCheck={false}
              autoCapitalize="none"
              autoCorrect="off"
              className="tabular w-full text-[12px] p-2 rounded"
              style={{
                background: "transparent",
                border: "0.5px solid var(--hairline)",
                color: "var(--text)",
              }}
            />
            {mismatch && (
              <p className="text-[11px] mt-2" style={{ color: "var(--warning)" }}>
                That didn&apos;t match. The text is the same; nothing was lost.
              </p>
            )}
          </>
        )}

        <div className="flex justify-end gap-3 text-[12px] mt-4">
          <button type="button" onClick={onClose} style={{ color: "var(--text-muted)" }}>
            Close
          </button>
          {ready && (
            <button
              type="button"
              onClick={submit}
              disabled={typed.length === 0}
              className="rounded-full disabled:opacity-40"
              style={{
                padding: "5px 11px",
                background: "var(--accent-tint)",
                color: "var(--accent)",
              }}
            >
              End session
            </button>
          )}
        </div>
      </>
    </Modal>
  );
}
