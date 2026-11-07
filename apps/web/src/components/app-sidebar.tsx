"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useState } from "react";
import { CheckIcon, ChevronsUpDownIcon, PlusIcon } from "lucide-react";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuBadge,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { CreateOrgDialog } from "@/components/create-org-dialog";
import { manageNav, workspaceNav, type NavItem } from "@/components/nav";
import { useOrg } from "@/components/org-provider";
import { RoleBadge } from "@/components/role-badge";
import { useSession } from "@/components/session-provider";
import { UserAvatar, UserMenuItems } from "@/components/user-menu";
import { initials } from "@/lib/format";

function OrgSwitcher() {
  const router = useRouter();
  const { orgs } = useSession();
  const { org } = useOrg();
  const [creating, setCreating] = useState(false);

  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <SidebarMenuButton
                  size="lg"
                  className="data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground"
                />
              }
            >
              <div className="grid size-8 shrink-0 place-items-center rounded-lg bg-linear-to-br from-[#1a73d1] to-[#0a4a8a] text-xs font-semibold text-white shadow-[inset_0_1px_0_rgb(255_255_255/0.2)]">
                {initials(org.name)}
              </div>
              <div className="grid min-w-0 flex-1 text-left text-sm leading-tight">
                <span className="truncate font-semibold">{org.name}</span>
                <span className="truncate font-mono text-xs text-muted-foreground">/{org.slug}</span>
              </div>
              <ChevronsUpDownIcon className="ml-auto" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="start" className="w-64">
              <DropdownMenuGroup>
                <DropdownMenuLabel>Workspaces</DropdownMenuLabel>
                {orgs.map((o) => (
                  <DropdownMenuItem key={o.id} onClick={() => router.push(`/orgs/${o.id}`)} className="gap-2">
                    <div className="grid size-6 place-items-center rounded-md border text-[10px] font-semibold">
                      {initials(o.name)}
                    </div>
                    <span className="flex-1 truncate">{o.name}</span>
                    {o.id === org.id && <CheckIcon className="text-brand" />}
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setCreating(true)} className="gap-2">
                <div className="grid size-6 place-items-center rounded-md border border-dashed">
                  <PlusIcon className="size-3.5" />
                </div>
                New workspace
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>
      <CreateOrgDialog open={creating} onOpenChange={setCreating} />
    </>
  );
}

function NavGroup({ label, items }: { label: string; items: NavItem[] }) {
  const pathname = usePathname();
  const { org, can } = useOrg();
  const base = `/orgs/${org.id}`;
  const visible = items.filter((i) => !i.permission || can(i.permission));
  if (!visible.length) return null;

  return (
    <SidebarGroup>
      <SidebarGroupLabel className="text-[11px] font-medium tracking-[0.08em] uppercase">{label}</SidebarGroupLabel>
      <SidebarMenu>
        {visible.map((item) => {
          const href = item.segment ? `${base}/${item.segment}` : base;
          const active = item.segment ? pathname.startsWith(href) : pathname === base;
          return (
            <SidebarMenuItem key={item.title}>
              <SidebarMenuButton isActive={active} tooltip={item.title} render={<Link href={href} />}>
                <item.icon />
                <span>{item.title}</span>
              </SidebarMenuButton>
              {item.soon && (
                <SidebarMenuBadge className="text-[10px] font-normal text-muted-foreground">Soon</SidebarMenuBadge>
              )}
            </SidebarMenuItem>
          );
        })}
      </SidebarMenu>
    </SidebarGroup>
  );
}

function NavUser() {
  const { user } = useSession();
  const { org } = useOrg();
  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger
            render={
              <SidebarMenuButton
                size="lg"
                className="data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground"
              />
            }
          >
            <UserAvatar user={user} className="size-8" />
            <div className="grid min-w-0 flex-1 text-left text-sm leading-tight">
              <span className="truncate font-medium">{user.name}</span>
              <span className="truncate text-xs text-muted-foreground">{user.email}</span>
            </div>
            <RoleBadge role={org.role} className="group-data-[collapsible=icon]:hidden" />
          </DropdownMenuTrigger>
          <DropdownMenuContent side="top" align="start" className="w-60">
            <UserMenuItems user={user} />
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
    </SidebarMenu>
  );
}

export function AppSidebar() {
  return (
    <Sidebar collapsible="icon" variant="inset">
      <SidebarHeader>
        <OrgSwitcher />
      </SidebarHeader>
      <SidebarContent>
        <NavGroup label="Workspace" items={workspaceNav} />
        <NavGroup label="Manage" items={manageNav} />
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
