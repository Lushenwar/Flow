import type { AppState, BaselineRow } from "./state";

/**
 * The daemon owns all authority; this client owns zero. Every call is a request
 * the daemon is free to refuse, and a 409 is a normal answer rather than an
 * error to work around.
 */

const BASE =
  process.env.NEXT_PUBLIC_MAST_URL ?? "http://127.0.0.1:8787";
const TOKEN = process.env.NEXT_PUBLIC_MAST_TOKEN ?? "";

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
  ) {
    super(`${status} ${code}`);
  }
}

async function call<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      Authorization: `Bearer ${TOKEN}`,
      ...init.headers,
    },
    cache: "no-store",
  });

  if (!res.ok) {
    let code = res.statusText;
    try {
      code = ((await res.json()) as { error?: string }).error ?? code;
    } catch {
      // A non-JSON error body is still an error; the status carries the meaning.
    }
    throw new ApiError(res.status, code);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  state: () => call<AppState>("/api/state"),

  commit: (body: {
    mode: string;
    durationMinutes: number;
    blocklistIds: string[];
    graceSeconds?: number;
    acceptTamperPenalty?: boolean;
  }) => call("/api/session", { method: "POST", body: JSON.stringify(body) }),

  /** Valid only in ARMING. Anywhere else this is a 409, by design. */
  abort: () => call("/api/session", { method: "DELETE" }),

  escape: () => call("/api/session/escape", { method: "POST" }),
  release: () => call("/api/session/release", { method: "POST" }),
  ack: () => call("/api/session/ack", { method: "POST" }),

  baseline: () => call<BaselineRow[]>("/api/baseline"),
  enable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    call(`/api/baseline/${id}/disable`, { method: "POST" }),
  cancelDisable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/disable`, { method: "DELETE" }),
};
