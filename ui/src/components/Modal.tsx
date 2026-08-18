"use client";

import { useEffect, useRef } from "react";

/**
 * A modal that a keyboard can actually use.
 *
 * The dialogs were <div>s over a backdrop: no role, no focus trap, no Escape,
 * and focus stayed behind them in the page — so a keyboard or screen-reader
 * user could open the commit dialog and never reach the Commit button.
 *
 * Native <dialog showModal> gets the trap, the Escape handling, the inert
 * background and the accessible role for free, in fewer lines than the markup
 * it replaces. claude.md's non-negotiables cover not BLOCKING accessibility
 * software; being unusable with it is a different failure and needs its own fix.
 */
export function Modal({
  onClose,
  children,
  label,
}: {
  onClose: () => void;
  children: React.ReactNode;
  label: string;
}) {
  const ref = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (el && !el.open) el.showModal();
  }, []);

  return (
    <dialog
      ref={ref}
      aria-label={label}
      // Escape fires `cancel` before `close`; routing both through onClose keeps
      // React state in step with what the element already did.
      onCancel={(e) => {
        e.preventDefault();
        onClose();
      }}
      onClose={onClose}
      // Clicking the backdrop is a click on the dialog element itself, since the
      // ::backdrop pseudo-element is not a separate event target.
      onClick={(e) => {
        if (e.target === ref.current) onClose();
      }}
      className="card w-full max-w-[380px] p-0"
      style={{
        // The UA stylesheet centres a <dialog> and paints its own background;
        // both are overridden here so it matches every other card in the app.
        background: "var(--surface)",
        color: "var(--text)",
        border: "0.5px solid var(--hairline)",
        borderRadius: 12,
        padding: 20,
      }}
    >
      {children}
    </dialog>
  );
}
