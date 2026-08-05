// node --test extension/coordinator.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { createCoordinator } from "./coordinator.js";

const VIDEO = {
  enforcing: true,
  version: 1,
  domains: ["youtube.com", "reddit.com"],
  allowPaths: {},
};

function harness({ rules = VIDEO, tabs = [] } = {}) {
  const state = { tabs: [...tabs], updates: [], fetches: 0 };
  const co = createCoordinator({
    fetchJSON: async () => {
      state.fetches++;
      return rules;
    },
    getTabs: async () => state.tabs,
    getTab: async (id) => state.tabs.find((t) => t.id === id),
    updateTab: async (id, url) => {
      state.updates.push({ id, url });
      const t = state.tabs.find((x) => x.id === id);
      if (t) t.url = url;
    },
    blockedUrlFor: (reason, from) => `ext://blocked?reason=${reason}&from=${from}`,
  });
  return { co, state };
}

/** Rejects rather than hanging forever, so a deadlock fails the run. */
function withTimeout(promise, ms = 1000) {
  return Promise.race([
    promise,
    new Promise((_, rej) => setTimeout(() => rej(new Error("deadlock: never settled")), ms)),
  ]);
}

// The regression test. prime() awaited a chain ending in await prime(), which
// never resolved — silently disabling every block while the poll loop kept
// running and looked healthy.
test("prime resolves even with tabs open that need sweeping", async () => {
  const { co } = harness({ tabs: [{ id: 1, url: "https://www.youtube.com/watch?v=x" }] });
  await withTimeout(co.prime());
  assert.ok(co.rules, "rules should be loaded after priming");
});

test("a blocked tab is redirected once primed", async () => {
  const { co, state } = harness({ tabs: [{ id: 1, url: "https://www.reddit.com/" }] });
  const blocked = await withTimeout(co.checkTab({ id: 1, url: "https://www.reddit.com/" }));

  assert.equal(blocked, true);
  assert.equal(state.updates.length, 1);
  assert.match(state.updates[0].url, /reason=reddit\.com/);
});

test("checkTab does not hang when called before priming finishes", async () => {
  const { co, state } = harness({ tabs: [{ id: 1, url: "https://www.reddit.com/" }] });
  // Deliberately not awaiting prime() first: this is the service-worker wake path.
  await withTimeout(Promise.all([
    co.checkTab({ id: 1, url: "https://www.reddit.com/" }),
    co.checkTab({ id: 1, url: "https://www.reddit.com/" }),
  ]));
  assert.ok(state.updates.length >= 1);
});

// The warm-tab fix, which is the reason the extension exists.
test("a rule-set change sweeps tabs that were already open", async () => {
  const { co, state } = harness({
    tabs: [
      { id: 1, url: "https://www.youtube.com/watch?v=x" },
      { id: 2, url: "https://wikipedia.org/" },
    ],
  });
  const n = await withTimeout(co.refresh());

  assert.equal(state.updates.length, 1, "only the blocked tab moves");
  assert.equal(state.updates[0].id, 1);
  assert.equal(state.tabs[1].url, "https://wikipedia.org/", "untouched");
  assert.ok(n === undefined || n >= 0);
});

test("an unchanged version does not re-sweep", async () => {
  const { co, state } = harness({ tabs: [{ id: 1, url: "https://www.reddit.com/" }] });
  await withTimeout(co.refresh());
  const first = state.updates.length;
  state.tabs[0].url = "https://www.reddit.com/"; // user navigates back
  await withTimeout(co.refresh());

  assert.equal(state.updates.length, first, "same version must not trigger a sweep");
});

test("a daemon that is down blocks nothing and does not throw", async () => {
  const co = createCoordinator({
    fetchJSON: async () => {
      throw new Error("ECONNREFUSED");
    },
    getTabs: async () => [{ id: 1, url: "https://www.reddit.com/" }],
    getTab: async () => ({ id: 1, url: "https://www.reddit.com/" }),
    updateTab: async () => assert.fail("must not block while the daemon is unreachable"),
    blockedUrlFor: () => "ext://blocked",
  });
  await withTimeout(co.refresh());
  assert.equal(await withTimeout(co.checkTab({ id: 1, url: "https://www.reddit.com/" })), false);
});

test("cached rules let a woken worker block immediately", async () => {
  const state = { updates: [] };
  const co = createCoordinator({
    // Network is slow/unavailable on wake; the cache is what we have.
    fetchJSON: async () => {
      throw new Error("still starting");
    },
    getTabs: async () => [],
    getTab: async (id) => ({ id, url: "https://www.reddit.com/" }),
    updateTab: async (id, url) => state.updates.push({ id, url }),
    loadCache: async () => VIDEO,
    blockedUrlFor: (reason, from) => `ext://blocked?reason=${reason}&from=${from}`,
  });

  assert.equal(await withTimeout(co.checkTab({ id: 7, url: "https://www.reddit.com/" })), true);
  assert.equal(state.updates.length, 1);
});

test("a tab whose url changed during priming is not sent to a stale target", async () => {
  const state = { updates: [] };
  const co = createCoordinator({
    fetchJSON: async () => VIDEO,
    getTabs: async () => [],
    // By the time we re-read it, the user has navigated somewhere allowed.
    getTab: async (id) => ({ id, url: "https://wikipedia.org/" }),
    updateTab: async (id, url) => state.updates.push({ id, url }),
    blockedUrlFor: (reason, from) => `ext://blocked?reason=${reason}&from=${from}`,
  });

  assert.equal(await withTimeout(co.checkTab({ id: 1, url: "https://www.reddit.com/" })), false);
  assert.equal(state.updates.length, 0);
});
