"use client";

import { createContext, useContext } from "react";
import type { OrganizationWithRole } from "@/lib/api";
import { can, type Permission } from "@/lib/permissions";

type OrgCtx = { org: OrganizationWithRole; can: (p: Permission) => boolean };

const OrgContext = createContext<OrgCtx | null>(null);

export function useOrg(): OrgCtx {
  const ctx = useContext(OrgContext);
  if (!ctx) throw new Error("useOrg must be used inside <OrgProvider>");
  return ctx;
}

export function OrgProvider({ org, children }: { org: OrganizationWithRole; children: React.ReactNode }) {
  return (
    <OrgContext.Provider value={{ org, can: (p) => can(org.role, p) }}>{children}</OrgContext.Provider>
  );
}
