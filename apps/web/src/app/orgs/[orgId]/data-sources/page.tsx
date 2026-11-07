"use client";

import { DatabaseIcon } from "lucide-react";
import { ComingSoon } from "@/components/coming-soon";
import { useOrg } from "@/components/org-provider";

export default function DataSourcesPage() {
  const { can } = useOrg();
  return (
    <ComingSoon
      title="Data sources"
      description="Connect databases, warehouses and SaaS tools."
      icon={DatabaseIcon}
      features={[
        { title: "Postgres & warehouses", body: "Read-only connections with encrypted credentials." },
        { title: "CSV upload", body: "Drop in a spreadsheet to start exploring instantly." },
        { title: "Tenant isolated", body: "Every source is scoped to this workspace by row-level security." },
      ]}
      note={
        can("integrations:manage")
          ? "As an owner or admin you'll be able to manage connections."
          : "Owners and admins manage connections."
      }
    />
  );
}
