"use client";

import { Plus, X } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError, api } from "@/lib/flow-client";
import { splitDomains, type CustomList } from "@/lib/state";

/**
 * The sites the presets do not cover.
 *
 * Adding is instant, because it strengthens. Removing is refused while the list
 * is enforced, and the way out is the ordinary one: turn the Custom sites row
 * off, wait fifteen minutes, then edit. That is stated on the screen rather than
 * discovered by clicking, the same way the Blocking screen's caption states the
 * asymmetry before you touch a switch.
 */
export function CustomSites({ onChanged }: { onChanged: () => void }) {
  const [list, setList] = useState<CustomList | null>(null);
  const [input, setInput] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const load = () => api.blocklists().then(setList).catch(() => {});
    const first = setTimeout(load, 0);
    const t = setInterval(load, 5000);
    return () => {
      clearTimeout(first);
      clearInterval(t);
    };
  }, []);

  async function submit() {
    const domains = splitDomains(input);
    if (domains.length === 0) return;

    setBusy(true);
    setProblem(null);
    try {
      const res = await api.addBlocked(domains);
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
      setList(await api.removeBlocked(domain));
      onChanged();
    } catch (e) {
      setProblem(describe(e));
    } finally {
      setBusy(false);
    }
  }

  if (!list) return null;

  const locked = list.enabled;

  return (
    <div className="mt-5 pt-3" style={{ borderTop: "0.5px solid var(--hairline)" }}>
      <p className="text-[12px] mb-1" style={{ color: "var(--text-secondary)" }}>
        Custom sites
      </p>
      <p className="text-[11px] mb-2" style={{ color: "var(--text-muted)" }}>
        {locked
          ? "Adding is instant. To remove one, turn Custom sites off first — that takes 15 minutes."
          : "Paste a domain or a whole URL. Subdomains are covered automatically."}
      </p>

      <div className="flex gap-2 mb-2">
        <input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") submit();
          }}
          placeholder="reddit.com"
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
          aria-label="Add these sites"
          className="rounded-full disabled:opacity-40 flex items-center gap-1"
          style={{
            padding: "5px 11px",
            background: "var(--accent-tint)",
            color: "var(--accent)",
            fontSize: 12,
          }}
        >
          <Plus size={12} />
          Add
        </button>
      </div>

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
          {/* Absent, not disabled, while enforced: a control that cannot do
              anything is worse than no control, and the caption above already
              says why it is gone. */}
          {!locked && (
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
          )}
        </div>
      ))}
    </div>
  );
}

/** The daemon's refusals are answers, not failures, so each one gets a sentence. */
function describe(e: unknown): string {
  if (!(e instanceof ApiError)) return String(e);
  switch (e.code) {
    case "would_weaken":
      return "Turn Custom sites off before removing anything. That takes 15 minutes.";
    case "allowlisted":
      return "That one can never be blocked — emergency, health, and OS update sites are permanently allowed.";
    case "not_a_domain":
      return e.detail ? `Not a domain: ${e.detail}` : "That isn't a domain.";
    case "too_many_domains":
      return "That's as many custom sites as the list holds.";
    default:
      return e.code;
  }
}
