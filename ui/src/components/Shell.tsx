"use client";

import { usePathname } from "next/navigation";
import { TitleBar } from "@/components/TitleBar";
import { Tabs } from "@/components/Tabs";

/**
 * Chooses the layout.
 *
 * Mini mode is a route rather than a piece of global state, which is what keeps
 * it from needing a context or a provider: /mini is its own page with its own
 * poll, and the only thing the shell has to do is stay out of its way. Tabs and
 * the card are the full-size app; the widget is the dial and nothing else.
 */
export function Shell({ children }: { children: React.ReactNode }) {
  const mini = usePathname() === "/mini";

  if (mini) return <>{children}</>;

  return (
    <main className="mx-auto max-w-[420px] p-6">
      <TitleBar />
      <Tabs />
      <div className="card">{children}</div>
    </main>
  );
}
