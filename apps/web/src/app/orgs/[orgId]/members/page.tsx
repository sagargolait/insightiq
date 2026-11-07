"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";
import { LogOutIcon, MoreHorizontalIcon, SearchIcon, UserMinusIcon, UserPlusIcon } from "lucide-react";
import { toast } from "sonner";
import { Button, buttonVariants } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { useOrg } from "@/components/org-provider";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import { RoleSelect } from "@/components/role-select";
import { useSession } from "@/components/session-provider";
import { UserAvatar } from "@/components/user-menu";
import { ApiError, type Member } from "@/lib/api";
import { useChangeRole, useMembers, useRemoveMember } from "@/lib/queries";
import { formatDate } from "@/lib/format";
import { assignableRoles, ROLE_LABEL, ROLES, type Role } from "@/lib/permissions";

export default function MembersPage() {
  const router = useRouter();
  const { user } = useSession();
  const { org, can } = useOrg();
  const members = useMembers(org.id);
  const changeRoleMutation = useChangeRole(org.id);
  const removeMutation = useRemoveMember(org.id);
  const [query, setQuery] = useState("");
  const [removing, setRemoving] = useState<Member | null>(null);

  const canManage = can("members:manage");
  const canManageOwners = can("members:manage_owners");
  const roles = assignableRoles(org.role);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = members.data ?? [];
    return q ? list.filter((m) => m.name.toLowerCase().includes(q) || m.email.toLowerCase().includes(q)) : list;
  }, [members.data, query]);

  /** Mirrors service rules: managers act on others; owners need manage_owners. */
  const canEdit = (m: Member) =>
    m.user_id !== user.id && canManage && (m.role !== "owner" || canManageOwners);

  async function changeRole(m: Member, role: Role) {
    try {
      await changeRoleMutation.mutateAsync({ userId: m.user_id, role });
      toast.success(`${m.name} is now ${role}`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Couldn't change role");
    }
  }

  async function remove(m: Member) {
    const self = m.user_id === user.id;
    try {
      await removeMutation.mutateAsync({ userId: m.user_id, self });
      if (self) {
        toast.success(`You left ${org.name}`);
        router.replace("/orgs");
        return;
      }
      toast.success(`${m.name} removed`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Couldn't remove member");
      throw err;
    }
  }

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Members"
        description="People with access to this workspace and what they can do."
        actions={
          canManage && (
            <Link href={`/orgs/${org.id}/invitations?new=1`} className={buttonVariants()}>
              <UserPlusIcon /> Invite people
            </Link>
          )
        }
      />

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {ROLES.map((r) => (
          <Card key={r} size="sm" className="gap-1 px-4">
            <span className="text-xs font-medium text-muted-foreground">{ROLE_LABEL[r]}s</span>
            <span className="text-2xl leading-none font-semibold tabular-nums">
              {members.data ? members.data.filter((x) => x.role === r).length : "–"}
            </span>
          </Card>
        ))}
      </div>

      <div className="flex items-center justify-between gap-3">
        <div className="relative w-full max-w-xs">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder="Search by name or email"
            className="pl-9"
          />
        </div>
        {members.data && (
          <span className="hidden text-sm text-muted-foreground sm:block">
            {filtered.length} of {members.data.length}
          </span>
        )}
      </div>

      <Card className="py-0">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead className="pl-4">Member</TableHead>
              <TableHead>Role</TableHead>
              <TableHead className="hidden md:table-cell">Joined</TableHead>
              <TableHead className="w-12 pr-4" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {members.isPending
              ? Array.from({ length: 4 }).map((_, i) => (
                  <TableRow key={i}>
                    <TableCell colSpan={4} className="px-4">
                      <Skeleton className="h-9" />
                    </TableCell>
                  </TableRow>
                ))
              : filtered.map((m) => {
                  const self = m.user_id === user.id;
                  return (
                    <TableRow key={m.user_id}>
                      <TableCell className="pl-4">
                        <div className="flex items-center gap-3">
                          <UserAvatar user={m} className="size-9" />
                          <div className="grid min-w-0 leading-tight">
                            <span className="truncate font-medium">
                              {m.name}
                              {self && <span className="font-normal text-muted-foreground"> (you)</span>}
                            </span>
                            <span className="truncate text-xs text-muted-foreground">{m.email}</span>
                          </div>
                        </div>
                      </TableCell>
                      <TableCell>
                        {canEdit(m) ? (
                          <RoleSelect value={m.role} roles={roles} onChange={(r) => r !== m.role && changeRole(m, r)} />
                        ) : (
                          <RoleBadge role={m.role} />
                        )}
                      </TableCell>
                      <TableCell className="hidden text-muted-foreground md:table-cell">
                        {formatDate(m.joined_at)}
                      </TableCell>
                      <TableCell className="pr-4 text-right">
                        {(self || canEdit(m)) && (
                          <DropdownMenu>
                            <DropdownMenuTrigger
                              render={<Button variant="ghost" size="icon-sm" aria-label={`Actions for ${m.name}`} />}
                            >
                              <MoreHorizontalIcon />
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end">
                              <DropdownMenuItem variant="destructive" onClick={() => setRemoving(m)}>
                                {self ? <LogOutIcon /> : <UserMinusIcon />}
                                {self ? "Leave workspace" : "Remove from workspace"}
                              </DropdownMenuItem>
                            </DropdownMenuContent>
                          </DropdownMenu>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
            {members.data && filtered.length === 0 && (
              <TableRow>
                <TableCell colSpan={4} className="py-10 text-center text-muted-foreground">
                  No members match “{query}”.
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </Card>

      <ConfirmDialog
        open={!!removing}
        onOpenChange={(o) => !o && setRemoving(null)}
        title={removing?.user_id === user.id ? `Leave ${org.name}?` : `Remove ${removing?.name}?`}
        description={
          removing?.user_id === user.id
            ? "You'll lose access immediately. You'll need a new invitation to rejoin."
            : "They'll lose access to all dashboards and data in this workspace immediately."
        }
        confirmLabel={removing?.user_id === user.id ? "Leave" : "Remove"}
        onConfirm={() => (removing ? remove(removing) : Promise.resolve())}
      />
    </div>
  );
}
