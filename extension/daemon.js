/**
 * Finding the daemon from inside the browser sandbox.
 *
 * Every other client reads %ProgramData%\Flow\port, which is the only
 * always-correct answer. An extension has no filesystem, so it probes instead.
 *
 * This exists because assuming 8787 was a silent, total failure: if anything
 * else held the port, the daemon quietly took another one, kept working, and
 * went on reporting itself healthy — while URL-path granularity and warm-tab
 * closing stopped completely and nothing anywhere said so. The daemon now walks
 * a bounded range rather than jumping to an ephemeral port, and this walks the
 * same range to meet it.
 *
 * Keep BASE_PORT and SEARCH in step with api.PortSearch in
 * internal/api/server.go.
 */

export const BASE_PORT = 8787;
export const SEARCH = 3;

/**
 * Returns a fetchJSON function for the coordinator.
 *
 * The last working port is remembered, so the common case costs one request and
 * only a failure pays for the walk.
 */
export function createRuleFetcher(fetchJSON, basePort = BASE_PORT, search = SEARCH) {
  let port = basePort;

  return async () => {
    try {
      return await fetchJSON(port);
    } catch (first) {
      for (let p = basePort; p < basePort + search; p++) {
        if (p === port) continue; // already tried, that is what got us here
        try {
          const rules = await fetchJSON(p);
          port = p;
          return rules;
        } catch {
          // Keep walking.
        }
      }
      // Nothing in range answered. Throw the original error: the coordinator
      // treats this as "daemon down" and keeps the last known rules rather than
      // unblocking, which is the correct failure direction.
      throw first;
    }
  };
}
