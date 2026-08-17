// node --test extension/daemon.test.mjs
import { test } from "node:test";
import assert from "node:assert/strict";
import { BASE_PORT, SEARCH, createRuleFetcher } from "./daemon.js";

const RULES = { enforcing: true, version: 1, domains: ["reddit.com"], allowPaths: {} };

/** A fake daemon listening on exactly one port. */
function daemonOn(listening) {
  const tried = [];
  const fetchJSON = async (port) => {
    tried.push(port);
    if (port !== listening) throw new Error("ECONNREFUSED");
    return RULES;
  };
  return { fetchJSON, tried };
}

test("the common case costs one request", async () => {
  const { fetchJSON, tried } = daemonOn(BASE_PORT);
  const get = createRuleFetcher(fetchJSON);

  assert.deepEqual(await get(), RULES);
  assert.deepEqual(tried, [BASE_PORT]);
});

// The bug: something else holds 8787, the daemon walks forward, and the
// extension polls 8787 forever while the daemon reports itself healthy.
test("finds a daemon that had to move off the base port", async () => {
  const { fetchJSON } = daemonOn(BASE_PORT + 1);
  const get = createRuleFetcher(fetchJSON);

  assert.deepEqual(await get(), RULES, "did not find the daemon one port along");
});

test("remembers the port it found, so the walk is paid for once", async () => {
  const { fetchJSON, tried } = daemonOn(BASE_PORT + 2);
  const get = createRuleFetcher(fetchJSON);

  await get();
  const afterFirst = tried.length;
  await get();

  assert.equal(tried.length, afterFirst + 1, "walked the range again on a known-good port");
  assert.equal(tried.at(-1), BASE_PORT + 2);
});

test("a daemon that is down throws, so the coordinator keeps the last rules", async () => {
  const get = createRuleFetcher(async () => {
    throw new Error("ECONNREFUSED");
  });

  // Must reject rather than resolve to null: resolving would look like
  // "the daemon says nothing is blocked", which unblocks everything.
  await assert.rejects(get(), /ECONNREFUSED/);
});

test("the walk is bounded", async () => {
  const tried = [];
  const get = createRuleFetcher(async (p) => {
    tried.push(p);
    throw new Error("ECONNREFUSED");
  });

  await assert.rejects(get());
  assert.ok(tried.length <= SEARCH + 1, `probed ${tried.length} ports for a range of ${SEARCH}`);
});
