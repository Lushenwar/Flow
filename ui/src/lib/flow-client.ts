import type {
  AppState,
  BankView,
  BaselineRow,
  CustomList,
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
    /** The offending input, when the daemon could name it. "Invalid" is not a
     *  fix you can act on after pasting twenty lines. */
    readonly detail?: string,
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
    let detail: string | undefined;
    try {
      const body = (await res.json()) as { error?: string; detail?: string };
      code = body.error ?? code;
      detail = body.detail;
    } catch {
      // A non-JSON error body is still an error; the status carries the meaning.
    }
    throw new ApiError(res.status, code, detail);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  state: () => call<AppState>("/api/state"),

  commit: (body: {
    mode: string;
    /** In pomodoro this is ONE focus interval, not the total. */
    durationMinutes: number;
    blocklistIds: string[];
    graceSeconds?: number;
    acceptTamperPenalty?: boolean;
    /** Pomodoro only; the daemon refuses the mode without them. */
    cycles?: number;
    breakMinutes?: number;
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

  /** Newest first. The limit is capped daemon-side; asking for everything is
   *  not an option the API offers. */
  events: (limit = 20, since = 0) =>
    call<EventRow[]>(`/api/events?since=${since}&limit=${limit}`),

  bank: () => call<BankView>("/api/bank"),
  spend: (minutes: number) =>
    call<BankView>("/api/bank/spend", {
      method: "POST",
      body: JSON.stringify({ minutes }),
    }),

  /** What a session covers. Editing is a 409 while one is running — chosen
   *  when calm, which is the whole reason it is not on the dial. */
  sessionLists: () => call<{ listIds: string[] }>("/api/session/lists"),
  putSessionLists: (listIds: string[]) =>
    call<{ listIds: string[] }>("/api/session/lists", {
      method: "PUT",
      body: JSON.stringify({ listIds }),
    }),

  schedules: () => call<ScheduleRow[]>("/api/schedules"),
  /** Refused with 409 would_weaken while the schedule's own window is live. */
  putSchedule: (s: Partial<ScheduleRow> & { days?: number[] }) =>
    call<ScheduleRow[]>("/api/schedules", {
      method: "POST",
      body: JSON.stringify(s),
    }),
  deleteSchedule: (id: string) =>
    call<ScheduleRow[]>(`/api/schedules/${encodeURIComponent(id)}`, {
      method: "DELETE",
    }),

  /** The user's own list. Adding strengthens and always works; removing is
   *  refused while the list is enforced, which is a 409 the UI explains. */
  blocklists: () => call<CustomList>("/api/blocklists"),
  addBlocked: (domains: string[]) =>
    call<{ added: string[]; list: CustomList }>("/api/blocklists", {
      method: "POST",
      body: JSON.stringify({ domains }),
    }),
  removeBlocked: (domain: string) =>
    call<CustomList>(`/api/blocklists/${encodeURIComponent(domain)}`, {
      method: "DELETE",
    }),

  baseline: () => call<BaselineRow[]>("/api/baseline"),
  enable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/enable`, { method: "POST" }),
  disable: (id: string) =>
    call(`/api/baseline/${id}/disable`, { method: "POST" }),
  cancelDisable: (id: string) =>
    call<BaselineRow[]>(`/api/baseline/${id}/disable`, { method: "DELETE" }),
};
