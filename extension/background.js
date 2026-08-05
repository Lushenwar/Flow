import { evaluate } from "./rules.js";

/**
 * The extension is additive. It never relaxes anything the daemon enforces at the
 * network layer; it adds the two things that layer structurally cannot do:
 *
 *   1. URL-path granularity. HTTPS means the network sees `youtube.com`, never
 *      `/watch` vs `/@channel`.
 *   2. Closing a tab that was ALREADY OPEN when enforcement started. DNS-layer
 *      blocking only bites when something needs to resolve a name, so a warm tab
 *      keeps working for a minute or two — measured in Chrome, and the single
 *      biggest gap between the product thesis and the build.
 *
 * If the daemon is unreachable this does nothing at all. A blocker that fails
 * closed in the browser would strand someone whose daemon crashed.
 */

const DAEMON = "http://127.0.0.1:8787/api/rules";
const POLL_MS = 2000;

let rules = null;
let lastVersion = null;

async function fetchRules() {
  try {
    const res = await fetch(DAEMON, { cache: "no-store" });
    if (!res.ok) return;
    const next = await res.json();
    const changed = next.version !== lastVersion || next.enforcing !== rules?.enforcing;
    rules = next;
    lastVersion = next.version;
    if (changed) await sweepAllTabs();
  } catch {
    // Daemon down or restarting. Leave the last known rules in place rather than
    // unblocking: enforcement is the daemon's job, and it will be back.
  }
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
  const verdict = evaluate(tab.url, rules);
  if (!verdict.blocked) return;
  try {
    await chrome.tabs.update(tab.id, { url: blockedUrl(verdict.reason, tab.url) });
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

// MV3 service workers get suspended. The alarm wakes us so an idle tab is caught
// within a minute even with no interaction; the interval covers the common case
// where the worker is already alive, and every tab event checks immediately.
chrome.alarms.create("poll", { periodInMinutes: 1 });
chrome.alarms.onAlarm.addListener(fetchRules);
setInterval(fetchRules, POLL_MS);

fetchRules();
