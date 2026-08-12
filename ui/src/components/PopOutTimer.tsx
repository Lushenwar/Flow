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

export function PopOutTimer({
  children,
}: {
  children: (now: number) => ReactNode;
}) {
  const supported = useSyncExternalStore(
    NO_UPDATES,
    () => !!pipApi(),
    () => false,
  );
  const [pip, setPip] = useState<Window | null>(null);
  const [now, setNow] = useState(() => Date.now());

  // Closing the tab must not leave an orphaned window floating over everything.
  useEffect(() => () => pip?.close(), [pip]);

  // The clock has to come from the pop-out's own window, not the page's.
  // Whenever this widget is the thing you are looking at, the tab that owns it
  // is by definition in the background, and Chrome throttles a hidden tab's
  // timers hard — measured here as a hidden tab issuing zero requests at all.
  // The pop-out window is visible, so its timers keep full rate.
  //
  // Only the countdown is driven from this. The arc still moves with the poll:
  // it advances by a third of a percent a minute, so nobody sees it wait.
  useEffect(() => {
    if (!pip) return;
    const id = pip.setInterval(() => setNow(Date.now()), 1000);
    return () => pip.clearInterval(id);
  }, [pip]);

  async function open() {
    const w = await pipApi()!.requestWindow({ width: 220, height: 240 });

    // The PiP document starts empty — no stylesheets, no CSS variables. Cloning
    // the page's own <style>/<link> nodes is what makes the timer look like the
    // timer instead of unstyled text. Every token comes across with them, so the
    // widget tracks the app's design rather than restating it here.
    for (const node of document.querySelectorAll<HTMLElement>(
      'style, link[rel="stylesheet"]',
    )) {
      const clone = node.cloneNode(true) as HTMLElement;
      if (clone instanceof HTMLLinkElement) {
        // The attribute is relative and this document's base URL is not the
        // page's, so a plain clone would 404. The property is already absolute.
        clone.href = (node as HTMLLinkElement).href;
      }
      w.document.head.appendChild(clone);
    }

    // The app puts every screen on a card, so the pop-out is that card with the
    // window as its border: same surface, same text colour, centred content. A
    // radius and a hairline would only sit under the OS window edge.
    const body = w.document.body;
    body.className = document.body.className;
    body.style.cssText =
      "margin:0;height:100vh;display:grid;place-items:center;background:var(--surface)";
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
      {pip && createPortal(children(now), pip.document.body)}
    </>
  );
}
