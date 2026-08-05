"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "./mast-client";
import type { AppState } from "./state";

/**
 * One poll feeds both screens. The UI never derives canRelease or attribution —
 * it renders what the daemon says, which is what stops the Blocking screen from
 * appearing to lift a permanent rule when a session ends.
 */
export function usePoll(intervalMs = 1000) {
  const [state, setState] = useState<AppState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const alive = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const next = await api.state();
      if (alive.current) {
        setState(next);
        setError(null);
      }
    } catch (e) {
      if (alive.current) setError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  useEffect(() => {
    alive.current = true;
    // The first fetch is scheduled rather than called inline: setState in an
    // effect body triggers a cascading render, and this hook is a subscription
    // to an external system, not a render-time computation.
    const first = setTimeout(refresh, 0);
    const t = setInterval(refresh, intervalMs);
    return () => {
      alive.current = false;
      clearTimeout(first);
      clearInterval(t);
    };
  }, [refresh, intervalMs]);

  return { state, error, refresh };
}

/**
 * A ticking clock for countdown rendering. Countdowns are derived from the
 * daemon's absolute instants on every tick, never accumulated locally — a
 * setInterval accumulator drifts and resets when the process does.
 */
export function useNow(intervalMs = 1000) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), intervalMs);
    return () => clearInterval(t);
  }, [intervalMs]);
  return now;
}
