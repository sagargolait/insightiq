import { SessionProvider } from "@/components/session-provider";

export default function OrgsLayout({ children }: { children: React.ReactNode }) {
  return <SessionProvider>{children}</SessionProvider>;
}
