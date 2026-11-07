import {
  BotIcon,
  DatabaseIcon,
  LayoutDashboardIcon,
  LayoutGridIcon,
  MailIcon,
  SettingsIcon,
  UsersIcon,
  type LucideIcon,
} from "lucide-react";
import type { Permission } from "@/lib/permissions";

export type NavItem = {
  title: string;
  /** Path segment under /orgs/[orgId]; "" is the overview. */
  segment: string;
  icon: LucideIcon;
  permission?: Permission;
  soon?: boolean;
};

export const workspaceNav: NavItem[] = [
  { title: "Overview", segment: "", icon: LayoutGridIcon },
  { title: "Dashboards", segment: "dashboards", icon: LayoutDashboardIcon, permission: "dashboards:view", soon: true },
  { title: "AI Copilot", segment: "copilot", icon: BotIcon, soon: true },
  { title: "Data sources", segment: "data-sources", icon: DatabaseIcon, soon: true },
];

export const manageNav: NavItem[] = [
  { title: "Members", segment: "members", icon: UsersIcon, permission: "members:view" },
  { title: "Invitations", segment: "invitations", icon: MailIcon, permission: "members:manage" },
  { title: "Settings", segment: "settings", icon: SettingsIcon, permission: "organization:view" },
];

export const allNav = [...workspaceNav, ...manageNav];
