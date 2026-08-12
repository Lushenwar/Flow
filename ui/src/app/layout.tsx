import type { Metadata } from "next";
import { Shell } from "@/components/Shell";
import "./globals.css";

export const metadata: Metadata = {
  title: "Flow",
  description: "Commitment blocker and focus timer",
};

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className="h-full antialiased">
      <body className="min-h-full">
        {/* Shell picks the layout: the card and its tabs, or the bare widget. */}
        <Shell>{children}</Shell>
      </body>
    </html>
  );
}
