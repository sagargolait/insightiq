"use client";

import Link from "next/link";
import { useState } from "react";
import { ArrowRightIcon, Building2Icon, PlusIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { DropdownMenu, DropdownMenuContent, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Logo } from "@/components/brand";
import { CreateOrgDialog } from "@/components/create-org-dialog";
import { EmptyState } from "@/components/empty-state";
import { RoleBadge } from "@/components/role-badge";
import { useSession } from "@/components/session-provider";
import { UserAvatar, UserMenuItems } from "@/components/user-menu";
import { initials } from "@/lib/format";

export default function OrgPickerPage() {
  const { user, orgs } = useSession();
  const [creating, setCreating] = useState(false);
  const firstName = user.name.split(" ")[0];

  return (
    <div className="min-h-svh">
      <header className="sticky top-0 z-10 flex h-16 items-center justify-between border-b bg-background/70 px-4 backdrop-blur-xl md:px-8">
        <Logo />
        <DropdownMenu>
          <DropdownMenuTrigger
            render={<Button variant="ghost" size="icon" className="rounded-full" aria-label="Account" />}
          >
            <UserAvatar user={user} className="size-8" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-60">
            <UserMenuItems user={user} />
          </DropdownMenuContent>
        </DropdownMenu>
      </header>

      <main className="mx-auto max-w-3xl px-4 py-12 md:py-16">
        <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">Hi {firstName}, pick a workspace</h1>
            <p className="mt-1 text-sm text-muted-foreground">
              {orgs.length
                ? `You belong to ${orgs.length} workspace${orgs.length === 1 ? "" : "s"}.`
                : "You're not in any workspace yet."}
            </p>
          </div>
          {orgs.length > 0 && (
            <Button onClick={() => setCreating(true)}>
              <PlusIcon /> New workspace
            </Button>
          )}
        </div>

        <div className="mt-8">
          {orgs.length === 0 ? (
            <EmptyState
              icon={Building2Icon}
              title="Create your first workspace"
              description="Or ask a teammate to send you an invitation link."
              action={
                <Button onClick={() => setCreating(true)}>
                  <PlusIcon /> Create workspace
                </Button>
              }
            />
          ) : (
            <div className="grid gap-3 sm:grid-cols-2">
              {orgs.map((org) => (
                <Link key={org.id} href={`/orgs/${org.id}`} className="group rounded-xl outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
                  <Card className="transition-[box-shadow,transform] group-hover:-translate-y-0.5 group-hover:ring-primary/40">
                    <CardHeader className="flex flex-row items-center gap-3">
                      <div className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary/15 text-sm font-semibold text-brand ring-1 ring-primary/25">
                        {initials(org.name)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <CardTitle className="truncate">{org.name}</CardTitle>
                        <CardDescription className="truncate">/{org.slug}</CardDescription>
                      </div>
                      <RoleBadge role={org.role} />
                      <ArrowRightIcon className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                    </CardHeader>
                  </Card>
                </Link>
              ))}
            </div>
          )}
        </div>
      </main>

      <CreateOrgDialog open={creating} onOpenChange={setCreating} />
    </div>
  );
}
