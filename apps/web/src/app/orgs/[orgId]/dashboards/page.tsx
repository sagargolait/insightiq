"use client";

import { LayoutDashboardIcon } from "lucide-react";
import { ComingSoon } from "@/components/coming-soon";
import { useOrg } from "@/components/org-provider";

export default function DashboardsPage() {
  const { can } = useOrg();
  return (
    <ComingSoon
      title="Dashboards"
      description="Build and share live views of your business metrics."
      icon={LayoutDashboardIcon}
      features={[
        { title: "Widgets", body: "Charts, KPIs and tables backed by your data sources." },
        { title: "Projects", body: "Group dashboards by team, product or initiative." },
        { title: "Shared by role", body: "Viewers read; analysts and above build and edit." },
      ]}
      note={
        can("dashboards:create")
          ? "Your role can create and edit dashboards once they ship."
          : "Your role will be able to view dashboards once they ship."
      }
    />
  );
}
