import type { Metadata } from "next";
import { Tabs } from "@/components/Tabs";
import "./globals.css";

export const metadata: Metadata = {
  title: "Flow",
  description: "Commitment blocker and focus timer",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">
        {/* Every screen sits in one card. No window chrome beyond the tabs. */}
        <main className="mx-auto max-w-[420px] p-6">
          <Tabs />
          <div className="card">{children}</div>
        </main>
      </body>
    </html>
  );
}
