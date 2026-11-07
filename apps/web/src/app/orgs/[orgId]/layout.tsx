"use client";

import Link from "next/link";
import { use } from "react";
import { usePathname } from "next/navigation";
import { SearchXIcon } from "lucide-react";
import { buttonVariants } from "@/components/ui/button";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Separator } from "@/components/ui/separator";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";
import { AppSidebar } from "@/components/app-sidebar";
import { EmptyState } from "@/components/empty-state";
import { allNav } from "@/components/nav";
import { OrgProvider } from "@/components/org-provider";
import { useSession } from "@/components/session-provider";
import { ThemeToggle } from "@/components/theme-toggle";

export default function OrgLayout({
  children,
  params,
}: {
  children: React.ReactNode;
  params: Promise<{ orgId: string }>;
}) {
  const { orgId } = use(params);
  const { orgs } = useSession();
  const pathname = usePathname();
  const org = orgs.find((o) => o.id === orgId);

  // Non-members get the same "not found" the API returns (404 hides existence).
  if (!org) {
    return (
      <div className="grid min-h-svh place-items-center p-6">
        <EmptyState
          icon={SearchXIcon}
          title="Workspace not found"
          description="It doesn't exist, or you're not a member."
          action={
            <Link href="/orgs" className={buttonVariants({ variant: "outline" })}>
              Back to workspaces
            </Link>
          }
          className="w-full max-w-md"
        />
      </div>
    );
  }

  const segment = pathname.split("/")[3] ?? "";
  const current = allNav.find((n) => n.segment === segment);

  return (
    <OrgProvider org={org}>
      <SidebarProvider>
        <AppSidebar />
        <SidebarInset>
          <header className="sticky top-0 z-10 flex h-14 shrink-0 items-center gap-2 border-b bg-background/70 px-4 backdrop-blur-xl">
            <SidebarTrigger className="-ml-1" />
            <Separator orientation="vertical" className="mr-2 data-vertical:h-4 data-vertical:self-auto" />
            <Breadcrumb>
              <BreadcrumbList>
                <BreadcrumbItem className="hidden md:block">
                  <BreadcrumbLink render={<Link href={`/orgs/${org.id}`} />}>{org.name}</BreadcrumbLink>
                </BreadcrumbItem>
                <BreadcrumbSeparator className="hidden md:block" />
                <BreadcrumbItem>
                  <BreadcrumbPage>{current?.title ?? "Overview"}</BreadcrumbPage>
                </BreadcrumbItem>
              </BreadcrumbList>
            </Breadcrumb>
            <div className="ml-auto">
              <ThemeToggle />
            </div>
          </header>
          <div
            aria-hidden
            className="pointer-events-none absolute inset-x-0 top-0 h-80 bg-[radial-gradient(60%_100%_at_30%_0%,var(--glow),transparent)]"
          />
          <div className="relative mx-auto w-full max-w-6xl flex-1 p-4 md:p-8">{children}</div>
        </SidebarInset>
      </SidebarProvider>
    </OrgProvider>
  );
}
