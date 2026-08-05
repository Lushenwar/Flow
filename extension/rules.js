/**
 * Pure URL matching. No chrome APIs in here so it can be tested with `node --test`.
 *
 * This layer exists because HTTPS means the network layer sees `youtube.com` and
 * never `/watch` vs `/@channel`. Domain-level blocking is all you get without an
 * extension or a MITM cert, and installing a MITM root CA to enforce a focus
 * timer is not a trade worth making.
 *
 * The extension is ADDITIVE. Uninstall it and domain-level blocking still holds.
 */

/** Hosts the extension must never interfere with, whatever the daemon says. */
const NEVER = [
  "localhost",
  "127.0.0.1",
  "gov",
  "mil",
  "gov.uk",
  "gc.ca",
  "canada.ca",
  "988lifeline.org",
  "suicidepreventionlifeline.org",
  "crisistextline.org",
  "samaritans.org",
  "thetrevorproject.org",
  "findahelpline.com",
  "who.int",
  "nhs.uk",
  "windowsupdate.com",
  "update.microsoft.com",
];

/** Label-boundary suffix match: "gov" matches cdc.gov, never notgov.com. */
export function underDomain(hostname, domain) {
  const h = String(hostname || "").toLowerCase().replace(/\.$/, "");
  const d = String(domain || "").toLowerCase();
  return h === d || h.endsWith("." + d);
}

export function isNeverBlocked(hostname) {
  return NEVER.some((d) => underDomain(hostname, d));
}

/**
 * Decide what to do with a URL.
 *
 * Returns { blocked: boolean, reason: string }. Anything that is not http(s) —
 * chrome://, the extension's own pages, about:blank — is left alone, or the
 * blocked page could not render and settings would be unreachable.
 */
export function evaluate(url, rules) {
  const empty = { blocked: false, reason: "not-enforcing" };
  if (!rules || !rules.enforcing || !Array.isArray(rules.domains)) return empty;

  let parsed;
  try {
    parsed = new URL(url);
  } catch {
    return { blocked: false, reason: "unparseable" };
  }
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    return { blocked: false, reason: "not-web" };
  }
  if (isNeverBlocked(parsed.hostname)) {
    return { blocked: false, reason: "allowlist" };
  }

  const domain = rules.domains.find((d) => underDomain(parsed.hostname, d));
  if (!domain) return { blocked: false, reason: "no-rule" };

  // A path exception makes only that path reachable; everything else on the
  // domain stays blocked.
  const patterns = (rules.allowPaths || {})[domain];
  if (patterns && patterns.length > 0) {
    const path = parsed.pathname + parsed.search;
    for (const p of patterns) {
      let re;
      try {
        re = new RegExp(p);
      } catch {
        continue; // a malformed pattern must not unblock the domain
      }
      if (re.test(path)) {
        return { blocked: false, reason: "path-allowed:" + domain };
      }
    }
  }
  return { blocked: true, reason: domain };
}
