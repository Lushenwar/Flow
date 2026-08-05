"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

/**
 * Two words is the entire navigation. No sidebar, no icons, no logo.
 */
const tabs = [
  { href: "/", label: "Focus" },
  { href: "/blocking", label: "Blocking" },
] as const;

export function Tabs() {
  const path = usePathname();

  return (
    <nav className="flex gap-4 mb-4">
      {tabs.map((tab) => {
        const active = path === tab.href;
        return (
          <Link
            key={tab.href}
            href={tab.href}
            className="pb-1 text-[13px] transition-colors"
            style={{
              fontWeight: active ? 500 : 400,
              color: active ? "var(--text)" : "var(--text-muted)",
              borderBottom: active ? "2px solid var(--text)" : "2px solid transparent",
            }}
          >
            {tab.label}
          </Link>
        );
      })}
    </nav>
  );
}
