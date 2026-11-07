"use client";

import { createContext, useContext } from "react";
import { Loader2Icon } from "lucide-react";
import type { OrganizationWithRole, User } from "@/lib/api";
import { useMe, useOrgs } from "@/lib/queries";

type Session = {
  user: User;
  orgs: OrganizationWithRole[];
};

const SessionContext = createContext<Session | null>(null);

export function useSession(): Session {
  const ctx = useContext(SessionContext);
  if (!ctx) throw new Error("useSession must be used inside <SessionProvider>");
  return ctx;
}

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const me = useMe();
  const orgs = useOrgs();

  if (!me.data || !orgs.data) {
    const error = me.error ?? orgs.error;
    return (
      <div className="grid min-h-svh place-items-center text-sm text-muted-foreground">
        {error ? `Couldn't reach the API: ${error.message}` : <Loader2Icon className="size-5 animate-spin" />}
      </div>
    );
  }

  return <SessionContext.Provider value={{ user: me.data, orgs: orgs.data }}>{children}</SessionContext.Provider>;
}
