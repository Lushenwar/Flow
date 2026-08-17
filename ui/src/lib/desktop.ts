"use client";

import { useSyncExternalStore } from "react";

/**
 * The bridge to the Wails shell.
 *
 * Hand-written rather than imported from the generated `wailsjs` bindings: it is
 * four method signatures, and a hand-written version keeps the browser build free
 * of a generated module that only exists after a Wails build has run. The same
 * pages have to work in `npm run dev` with no shell at all.
 */

/**
 * Window control only. The API does not come through here — the shell proxies
 * /api same-origin and adds the bearer token in Go, so the token never reaches
 * this bundle.
 */
interface WailsApp {
  SetMini(mini: boolean): Promise<void>;
  Minimise(): Promise<void>;
  Quit(): Promise<void>;
  /** The WINDOW's autostart, not the daemon's. The daemon is a service that
   *  starts itself, and nothing in this UI can change that. */
  AutostartEnabled(): Promise<boolean>;
  SetAutostart(on: boolean): Promise<void>;
}

declare global {
  interface Window {
    go?: { main?: { App?: WailsApp } };
  }
}

/** The bound Go object, or null in a plain browser. */
export function desktop(): WailsApp | null {
  if (typeof window === "undefined") return null;
  return window.go?.main?.App ?? null;
}

/**
 * Whether we are inside the desktop shell.
 *
 * Read at call time, never cached at module scope: the bindings are injected by
 * the runtime and may not exist yet when this module is first evaluated.
 */
export function isDesktop(): boolean {
  return desktop() !== null;
}

/**
 * The same answer, safe to render with.
 *
 * The pages are a static export, so they are prerendered with no window at all.
 * Reading `window` during render would make the server and client disagree on
 * the first paint; the false server snapshot means the window chrome is simply
 * absent from the HTML and appears on hydration. Bindings do not come and go
 * after load, so subscribing is a no-op.
 */
const NO_UPDATES = () => () => {};

export function useIsDesktop(): boolean {
  return useSyncExternalStore(NO_UPDATES, isDesktop, () => false);
}
