import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = {
  title: "InsightIQ — Business intelligence",
  description: "AI-powered business intelligence for modern teams."
};

export default function RootLayout({ children }: Readonly<{ children: React.ReactNode }>) {
  return <html lang="en"><body>{children}</body></html>;
}
