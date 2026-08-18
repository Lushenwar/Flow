"use client";

import { Plus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError, api } from "@/lib/flow-client";
import { splitDomains, type AllowList as AllowListView } from "@/lib/state";

/**
 * The escape list for "block everything" modes.
 *
 * Bedtime and Offline refuse every name they were never told about, so this is
 * not a convenience — it is the difference between a commitment device and a
 * laptop with no internet and a fifteen-minute wait to fix it. It says so.
 *
 * The direction is REVERSED from Custom sites, and deliberately: under
 * default-deny, ADDING here is what weakens enforcement. So adding is refused
 * while a window is live, and removing is always free.
 */
export function AllowList({ onChanged }: { onChanged: () => void }) {
  const [list, setList] = useState<AllowListView | null>(null);
  const [input, setInput] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const load = () => api.allowlist().then(setList).catch(() => {});
    const first = setTimeout(load, 0);
    const t = setInterval(load, 5000);
    return () => {
      clearTimeout(first);
      clearInterval(t);
    };
  }, []);

  if (!list) return null;

  async function submit() {
    const domains = splitDomains(input);
    if (domains.length === 0) return;
    setBusy(true);
    setProblem(null);
    try {
      const res = await api.addAllowed(domains);
      setList(res.list);
      setInput("");
      onChanged();
    } catch (e) {
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  async function remove(domain: string) {
    setBusy(true);
    setProblem(null);
    try {
      setList(await api.removeAllowed(domain));
      onChanged();
    } catch (e) {
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <p className="text-[12px] mb-1" style={{ color: "var(--text-secondary)" }}>
        Always reachable
      </p>
      <p className="text-[11px] mb-2" style={{ color: "var(--text-muted)" }}>
        {list.locked
          ? "A block-everything rule is running, so this list is frozen. Removing is still allowed."
          : "Bedtime and Offline block everything except these. Add your work VPN, your code host, anything you cannot lose. Emergency, health and OS update sites are always reachable and need no entry here."}
      </p>

      {!list.locked && (
        <div className="flex gap-2 mb-2">
          <input
            value={input}
            onChange={(e) => setInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") submit();
            }}
            placeholder="github.com"
            spellCheck={false}
            autoCapitalize="none"
            autoCorrect="off"
            className="flex-1 text-[12px] px-2 py-1 rounded"
            style={{
              background: "transparent",
              border: "0.5px solid var(--hairline)",
              color: "var(--text)",
            }}
          />
          <button
            type="button"
            onClick={submit}
            disabled={busy || input.trim() === ""}
            aria-label="Allow these sites"
            className="rounded-full disabled:opacity-40 flex items-center gap-1"
            style={{
              padding: "5px 11px",
              background: "var(--accent-tint)",
              color: "var(--accent)",
              fontSize: 12,
            }}
          >
            <Plus size={12} />
            Allow
          </button>
        </div>
      )}

      {problem && (
        <p className="text-[11px] mb-2" style={{ color: "var(--warning)" }}>
          {problem}
        </p>
      )}

      {list.domains.map((domain, i) => (
        <div
          key={domain}
          className="flex items-center justify-between py-[10px]"
          style={{
            borderBottom:
              i === list.domains.length - 1 ? "none" : "0.5px solid var(--hairline)",
          }}
        >
          <span className="text-[14px]">{domain}</span>
          {/* Always available: narrowing the escape list strengthens the block. */}
          <button
            type="button"
            onClick={() => remove(domain)}
            disabled={busy}
            aria-label={`Remove ${domain}`}
            className="rounded p-1 disabled:opacity-40"
            style={{ color: "var(--text-muted)" }}
          >
            <X size={13} />
          </button>
        </div>
      ))}
    </div>
  );
}

function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e);
  switch (e.code) {
    case "would_weaken":
      return "A block-everything rule is running. You cannot widen the escape list while it is in force.";
    case "not_a_domain":
      return e.detail ? `Not a domain: ${e.detail}` : "That isn't a domain.";
    case "too_many_domains":
      return "That's as many entries as the list holds.";
    default:
      return e.code;
  }
}
