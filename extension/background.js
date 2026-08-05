import { evaluate } from "./rules.js";

/**
 * The extension is additive. It never relaxes anything the daemon enforces at the
 * network layer; it adds the two things that layer structurally cannot do:
 *
 *   1. URL-path granularity. HTTPS means the network sees `youtube.com`, never
 *      `/watch` vs `/@channel`.
 *   2. Closing a tab that was ALREADY OPEN when enforcement started. DNS-layer
 *      blocking only bites when something needs to resolve a name, so a warm tab
 *      keeps working for a minute or two.
 *
 * If the daemon is unreachable this does nothing at all. A blocker that fails
 * closed in the browser would strand someone whose daemon crashed.
 */

const DAEMON = "http://127.0.0.1:8787/api/rules";
const POLL_MS = 2000;

let rules = null;
let lastVersion = null;
let priming = null;

/**
 * MV3 suspends the service worker after ~30s idle and re-runs this file on the
 * next event. Without a cache, the navigation that woke us would be evaluated
 * against `rules === null` and sail straight through — measured: the first
 * YouTube /watch after a wake was not blocked. session storage survives the
 * suspend, so a wake starts with the last known rules already in hand.
 */
async function loadCached() {
  try {
    const { rules: cached } = await chrome.storage.session.get("rules");
    if (cached && rules === null) {
      rules = cached;
      lastVersion = cached.version;
    }
  } catch {
    // No cache yet.
  }
}

async function fetchRules() {
  try {
    const res = await fetch(DAEMON, { cache: "no-store" });
    if (!res.ok) return;
    const next = await res.json();
    const changed = next.version !== lastVersion || next.enforcing !== rules?.enforcing;
    rules = next;
    lastVersion = next.version;
    try {
      await chrome.storage.session.set({ rules: next });
    } catch {
      // Cache is an optimisation, not a requirement.
    }
    if (changed) await sweepAllTabs();
  } catch {
    // Daemon down or restarting. Leave the last known rules in place rather than
    // unblocking: enforcement is the daemon's job, and it will be back.
  }
}

/** Resolves once we have rules from cache or network. Never evaluate before it. */
function prime() {
  if (!priming) {
    priming = (async () => {
      await loadCached();
      await fetchRules();
    })();
  }
  return priming;
}

function blockedUrl(reason, original) {
  return (
    chrome.runtime.getURL("blocked.html") +
    "?reason=" + encodeURIComponent(reason) +
    "&from=" + encodeURIComponent(original)
  );
}

async function checkTab(tab) {
  if (!tab || !tab.url || !tab.id) return;
  await prime();

  const verdict = evaluate(tab.url, rules);
  if (!verdict.blocked) return;

  try {
    // Re-read the tab: priming may have taken a moment and the user may have
    // navigated on. Redirecting to a stale target is how the wrong page ends up
    // in the address bar.
    const current = await chrome.tabs.get(tab.id);
    if (!evaluate(current.url, rules).blocked) return;
    await chrome.tabs.update(tab.id, { url: blockedUrl(verdict.reason, current.url) });
  } catch {
    // Tab closed under us, or is a protected page. Nothing to do.
  }
}

/** The warm-tab fix: every open tab is re-evaluated when the rule set changes. */
async function sweepAllTabs() {
  try {
    const tabs = await chrome.tabs.query({});
    await Promise.all(tabs.map(checkTab));
  } catch {
    // no-op
  }
}

chrome.tabs.onUpdated.addListener((_id, info, tab) => {
  if (info.status === "loading" || info.url) checkTab(tab);
});

chrome.tabs.onActivated.addListener(async ({ tabId }) => {
  try {
    await checkTab(await chrome.tabs.get(tabId));
  } catch {
    // no-op
  }
});

// The alarm wakes the worker so an idle tab is caught within a minute even with
// no interaction; the interval covers the common case where the worker is
// already alive, and every tab event checks immediately.
chrome.alarms.create("poll", { periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(fetchRules);
setInterval(fetchRules, POLL_MS);

prime();
