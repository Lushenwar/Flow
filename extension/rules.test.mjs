// node --test extension/
import { test } from "node:test";
import assert from "node:assert/strict";
import { evaluate, isNeverBlocked, underDomain } from "./rules.js";

const video = {
  enforcing: true,
  domains: ["youtube.com", "twitch.tv", "reddit.com"],
  allowPaths: {},
};

const study = {
  enforcing: true,
  domains: ["youtube.com", "reddit.com"],
  allowPaths: {
    "youtube.com": ["^/@", "^/channel/", "^/playlist", "^/c/", "^/results"],
  },
};

test("suffix match respects label boundaries", () => {
  assert.ok(underDomain("www.youtube.com", "youtube.com"));
  assert.ok(underDomain("youtube.com", "youtube.com"));
  assert.ok(underDomain("m.youtube.com.", "youtube.com"));
  assert.ok(!underDomain("notyoutube.com", "youtube.com"));
  assert.ok(!underDomain("youtube.com.evil.net", "youtube.com"));
});

test("blocks a domain and its subdomains", () => {
  for (const u of [
    "https://youtube.com/",
    "https://www.youtube.com/feed/subscriptions",
    "https://m.youtube.com/",
    "https://www.twitch.tv/somebody",
  ]) {
    assert.equal(evaluate(u, video).blocked, true, u);
  }
});

test("leaves everything else alone", () => {
  for (const u of ["https://wikipedia.org/", "https://go.dev/doc"]) {
    assert.equal(evaluate(u, video).blocked, false, u);
  }
});

// The Phase 7 exit criterion.
test("blocks /watch while an allowlisted channel URL stays reachable", () => {
  assert.equal(evaluate("https://www.youtube.com/watch?v=abc123", study).blocked, true);
  assert.equal(evaluate("https://www.youtube.com/@SomeChannel", study).blocked, false);
  assert.equal(evaluate("https://www.youtube.com/channel/UC123", study).blocked, false);
  assert.equal(evaluate("https://www.youtube.com/playlist?list=PL1", study).blocked, false);
});

test("a path exception does not unblock the rest of the domain", () => {
  assert.equal(evaluate("https://www.youtube.com/", study).blocked, true);
  assert.equal(evaluate("https://www.youtube.com/shorts/xyz", study).blocked, true);
  assert.equal(evaluate("https://www.youtube.com/feed/trending", study).blocked, true);
});

test("a path exception on one domain does not leak to another", () => {
  assert.equal(evaluate("https://www.reddit.com/@someone", study).blocked, true);
});

test("a malformed pattern must not unblock the domain", () => {
  const broken = {
    enforcing: true,
    domains: ["youtube.com"],
    allowPaths: { "youtube.com": ["^/([unclosed"] },
  };
  assert.equal(evaluate("https://www.youtube.com/watch?v=x", broken).blocked, true);
});

test("the permanent allowlist is unreachable even if the daemon names it", () => {
  // A daemon that has been tampered with, or a bug, must not get the extension
  // to block a crisis line.
  const hostile = {
    enforcing: true,
    domains: ["988lifeline.org", "cdc.gov", "nhs.uk", "youtube.com"],
    allowPaths: {},
  };
  assert.equal(evaluate("https://988lifeline.org/", hostile).blocked, false);
  assert.equal(evaluate("https://www.cdc.gov/flu", hostile).blocked, false);
  assert.equal(evaluate("https://www.nhs.uk/", hostile).blocked, false);
  assert.ok(isNeverBlocked("988lifeline.org"));
  // ...while a genuinely blocked domain still is.
  assert.equal(evaluate("https://youtube.com/", hostile).blocked, true);
});

test("non-web schemes are never touched", () => {
  // Blocking chrome:// would make the extension impossible to turn off from the
  // browser, and about:blank is what a closing tab shows.
  for (const u of [
    "chrome://extensions",
    "chrome-extension://abc/blocked.html",
    "about:blank",
    "file:///C:/notes.txt",
  ]) {
    assert.equal(evaluate(u, video).blocked, false, u);
  }
});

test("nothing is blocked when the daemon is idle or unreachable", () => {
  assert.equal(evaluate("https://youtube.com/", { enforcing: false, domains: ["youtube.com"] }).blocked, false);
  assert.equal(evaluate("https://youtube.com/", null).blocked, false);
  assert.equal(evaluate("https://youtube.com/", {}).blocked, false);
});

test("garbage input does not throw", () => {
  assert.equal(evaluate("", video).blocked, false);
  assert.equal(evaluate("not a url", video).blocked, false);
  assert.equal(evaluate(undefined, video).blocked, false);
});
