import { evaluate } from "./rules.js";

/**
 * Rule fetching, priming and tab sweeping, with every chrome API injected.
 *
 * This is a separate module because the bug that made it necessary was a
 * deadlock in exactly this control flow — prime() awaited a chain that ended in
 * await prime() — and it was invisible to the matcher tests while the poll loop
 * kept running and looked healthy. Orchestration needs its own check.
 */
export function createCoordinator({
  fetchJSON,
  getTabs,
  getTab,
  updateTab,
  loadCache = async () => null,
  saveCache = async () => {},
  blockedUrlFor,
}) {
  let rules = null;
  let lastVersion = null;
  let priming = null;

  async function loadCached() {
    try {
      const cached = await loadCache();
      if (cached && rules === null) {
        rules = cached;
        lastVersion = cached.version;
      }
    } catch {
      // No cache yet.
    }
  }

  /**
   * `sweep` MUST be false while priming: checkTab awaits prime(), and a sweep
   * calls checkTab, so a sweeping prime waits on itself forever.
   */
  async function refresh({ sweep = true } = {}) {
    let next;
    try {
      next = await fetchJSON();
    } catch {
      // Daemon down or restarting. Keep the last known rules rather than
      // unblocking: enforcement is the daemon's job and it will be back.
      return;
    }
    if (!next) return;

    const changed = next.version !== lastVersion || next.enforcing !== rules?.enforcing;
    rules = next;
    lastVersion = next.version;
    try {
      await saveCache(next);
    } catch {
      // Cache is an optimisation, not a requirement.
    }
    if (changed && sweep) await sweepAllTabs();
  }

  function prime() {
    if (!priming) {
      priming = (async () => {
        await loadCached();
        await refresh({ sweep: false });
      })();
    }
    return priming;
  }

  async function checkTab(tab) {
    if (!tab || !tab.url || tab.id === undefined || tab.id === null) return false;
    await prime();

    const verdict = evaluate(tab.url, rules);
    if (!verdict.blocked) return false;

    try {
      // Re-read: priming may have taken a moment and the user may have navigated
      // on. Redirecting to a stale target puts the wrong page in the address bar.
      const current = (await getTab(tab.id)) || tab;
      if (!evaluate(current.url, rules).blocked) return false;
      await updateTab(tab.id, blockedUrlFor(verdict.reason, current.url));
      return true;
    } catch {
      // Tab closed under us, or is a protected page.
      return false;
    }
  }

  /** The warm-tab fix: re-evaluate every open tab when the rule set changes. */
  async function sweepAllTabs() {
    try {
      const tabs = await getTabs();
      const results = await Promise.all(tabs.map(checkTab));
      return results.filter(Boolean).length;
    } catch {
      return 0;
    }
  }

  return {
    refresh,
    prime,
    checkTab,
    sweepAllTabs,
    get rules() {
      return rules;
    },
  };
}
