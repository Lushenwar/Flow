import { api as browserAPI, sessionStore } from "./browser.js";
import { createCoordinator } from "./coordinator.js";

/**
 * Thin adapter: chrome APIs in, coordinator out. All the logic worth testing
 * lives in coordinator.js.
 *
 * The extension is additive. It never relaxes anything the daemon enforces at
 * the network layer; it adds the two things that layer structurally cannot do:
 *
 *   1. URL-path granularity. HTTPS means the network sees `youtube.com`, never
 *      `/watch` vs `/@channel`.
 *   2. Closing a tab that was ALREADY OPEN when enforcement started. DNS-layer
 *      blocking only bites when something needs to resolve a name, so a warm tab
 *      keeps working for a minute or two.
 *
 * If the daemon is unreachable this does nothing. A blocker that fails closed in
 * the browser would strand someone whose daemon crashed.
 */

const DAEMON = "http://127.0.0.1:8787/api/rules";
const POLL_MS = 2000;

const flow = createCoordinator({
  fetchJSON: async () => {
    const res = await fetch(DAEMON, { cache: "no-store" });
    if (!res.ok) throw new Error(String(res.status));
    return res.json();
  },
  getTabs: () => browserAPI.tabs.query({}),
  getTab: (id) => browserAPI.tabs.get(id),
  updateTab: (id, url) => browserAPI.tabs.update(id, { url }),
  // MV3 suspends the service worker after ~30s idle and re-runs this file on the
  // next event. session storage survives that, so a wake starts with the last
  // known rules instead of evaluating against null and letting the navigation
  // that woke us sail through.
  loadCache: async () => (await sessionStore.get("rules")).rules,
  saveCache: (r) => sessionStore.set({ rules: r }),
  blockedUrlFor: (reason, original) =>
    browserAPI.runtime.getURL("blocked.html") +
    "?reason=" + encodeURIComponent(reason) +
    "&from=" + encodeURIComponent(original),
});

browserAPI.tabs.onUpdated.addListener((_id, info, tab) => {
  if (info.status === "loading" || info.url) flow.checkTab(tab);
});

browserAPI.tabs.onActivated.addListener(async ({ tabId }) => {
  try {
    await flow.checkTab(await browserAPI.tabs.get(tabId));
  } catch {
    // Tab vanished.
  }
});

// The alarm wakes the worker so an idle tab is caught within a minute even with
// no interaction; the interval covers the case where the worker is already
// alive, and every tab event checks immediately.
browserAPI.alarms.create("poll", { periodInMinutes: 1 });
browserAPI.alarms.onAlarm.addListener(() => flow.refresh());
setInterval(() => flow.refresh(), POLL_MS);

// A browser restart is the warm-tab case again: Chrome restores the tabs from
// last session, and the rule version has not changed since, so a change-driven
// sweep never fires. Sweep once on startup instead of waiting for a change that
// will not come. Safe after prime() resolves — sweeping during it deadlocks.
flow.prime().then(() => flow.sweepAllTabs());
