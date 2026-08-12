"use client";

import { Minus, PictureInPicture2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { desktop, useIsDesktop } from "@/lib/desktop";

/**
 * The window chrome, drawn by the page because the Wails window is frameless.
 *
 * Frameless is a build-time option in Wails v2 — it cannot be toggled at
 * runtime — so the full-size window has to wear a custom bar in order for mini
 * mode to be able to shed the OS one. Everything here is absent in a browser.
 */
export function TitleBar() {
  const isDesktop = useIsDesktop();
  const router = useRouter();

  if (!isDesktop) return null;

  return (
    <div
      className="flex items-center justify-between mb-3 -mt-1 select-none"
      // Wails reads this property to decide what drags the window. Without it a
      // frameless window cannot be moved at all.
      style={{ "--wails-draggable": "drag" } as React.CSSProperties}
    >
      <span className="text-[11px]" style={{ color: "var(--text-muted)" }}>
        Flow
      </span>

      <span
        className="flex items-center gap-1"
        style={{ "--wails-draggable": "no-drag" } as React.CSSProperties}
      >
        <WindowButton
          label="Pin the timer on top"
          onClick={async () => {
            // Route first: the window is about to be widget-sized, and arriving
            // there still showing the full screen is a visible flash of the
            // wrong layout.
            router.push("/mini");
            await desktop()?.SetMini(true);
          }}
        >
          <PictureInPicture2 size={13} />
        </WindowButton>
        <WindowButton label="Minimise" onClick={() => desktop()?.Minimise()}>
          <Minus size={13} />
        </WindowButton>
        {/* Closing the window ends nothing. The daemon is a service and keeps
            enforcing — that is the entire ownership model, and the only reason
            a close button is safe to offer mid-session. */}
        <WindowButton label="Close" onClick={() => desktop()?.Quit()}>
          <X size={13} />
        </WindowButton>
      </span>
    </div>
  );
}

function WindowButton({
  label,
  onClick,
  children,
}: {
  label: string;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={onClick}
      className="rounded p-1 transition-colors hover:bg-[var(--hairline)]"
      style={{ color: "var(--text-muted)" }}
    >
      {children}
    </button>
  );
}
