import type {
  AppState,
  BankView,
  BaselineRow,
  EventRow,
  ScheduleRow,
} from "./state";

/**
 * The daemon owns all authority; this client owns zero. Every call is a request
 * the daemon is free to refuse, and a 409 is a normal answer rather than an
 * error to work around.
 */

/**
 * Empty base means same-origin, which is the desktop shell: it serves these
 * pages and proxies /api to the daemon, attaching the bearer token in Go. The
 * token never enters this bundle.
 *
 * `npm run dev` sets both vars because the browser has no shell to proxy through
 * and cannot read %ProgramData%\Flow\token — which is exactly why the browser
 * build is a dev tool and not something you can hand to anyone.
 */
const BASE = process.env.NEXT_PUBLIC_FLOW_URL ?? "";
const TOKEN = process.env.NEXT_PUBLIC_FLOW_TOKEN ?? "";

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
  challenge: () =>
    call<{ id: string; text: string }>("/api/session/escape/challenge"),
  verify: (challengeId: string, typed: string) =>
    call("/api/session/escape/verify", {
      method: "POST",
      body: JSON.stringify({ challengeId, typed }),
    }),
  ack: () => call("/api/session/ack", { method: "POST" }),

  events: (since = 0) => call<EventRow[]>(`/api/events?since=${since}`),

  bank: () => call<BankView>("/api/bank"),
  spend: (minutes: number) =>
    call<BankView>("/api/bank/spend", {
      method: "POST",
      body: JSON.stringify({ minutes }),
    }),

  schedules: () => call<ScheduleRow[]>("/api/schedules"),
  putSchedule: (s: Partial<ScheduleRow>) =>
    call<ScheduleRow[]>("/api/schedules", {
      method: "POST",
      body: JSON.stringify(s),
    }),

  baseline: () => call<BaselineRow[]>("/api/baseline"),
  enable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    call(`/api/baseline/${id}/disable`, { method: "POST" }),
  cancelDisable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/disable`, { method: "DELETE" }),
};
