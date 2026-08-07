"use client";

import {
  type ReactNode,
  useEffect,
  useState,
  useSyncExternalStore,
} from "react";
import { createPortal } from "react-dom";

/**
 * The countdown in its own always-on-top window.
 *
 * ponytail: this is the browser's Document Picture-in-Picture API, not a second
 * app window and not a tray widget. The OS keeps it above everything else for
 * free, so the whole feature is a portal into a document the browser hands us.
 * Ceiling: Chromium 116+ only (Chrome, Edge). Firefox has no equivalent, so the
 * button hides itself there; a real floating window would need the Wails shell.
 */

interface DocumentPiP {
  requestWindow(options?: { width?: number; height?: number }): Promise<Window>;
}

function pipApi(): DocumentPiP | undefined {
  return (window as { documentPictureInPicture?: DocumentPiP })
    .documentPictureInPicture;
}

/** Support never changes after load, so the "subscribe" here is a no-op. It
 *  exists to read `window` on the client only — the server snapshot is false, so
 *  the button is absent in the HTML and appears on hydration without a mismatch. */
const NO_UPDATES = () => () => {};

export function PopOutTimer({ children }: { children: ReactNode }) {
  const supported = useSyncExternalStore(
    NO_UPDATES,
    () => !!pipApi(),
    () => false,
  );
  const [pip, setPip] = useState<Window | null>(null);

  // Closing the tab must not leave an orphaned window floating over everything.
  useEffect(() => () => pip?.close(), [pip]);

  async function open() {
    const w = await pipApi()!.requestWindow({ width: 220, height: 240 });
    // The PiP document starts empty — no stylesheets, no CSS variables. Cloning
    // the page's own <style>/<link> nodes is what makes the timer look like the
    // timer instead of unstyled text.
    for (const node of document.querySelectorAll(
      'style, link[rel="stylesheet"]',
    )) {
      w.document.head.appendChild(node.cloneNode(true));
    }
    w.document.body.style.display = "grid";
    w.document.body.style.placeItems = "center";
    w.document.body.style.height = "100vh";
    w.document.body.style.margin = "0";
    w.addEventListener("pagehide", () => setPip(null));
    setPip(w);
  }

  if (!supported) return null;

  return (
    <>
      <button
        type="button"
        onClick={pip ? () => pip.close() : open}
        className="text-[11px] mt-3 underline underline-offset-2"
        style={{ color: "var(--text-muted)" }}
      >
        {pip ? "close pop-out timer" : "pop out timer"}
      </button>
      {pip && createPortal(children, pip.document.body)}
    </>
  );
}
