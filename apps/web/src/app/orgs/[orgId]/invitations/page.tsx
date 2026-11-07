"use client";

import { useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { LockIcon, MailIcon, UserPlusIcon, XIcon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Card } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ConfirmDialog } from "@/components/confirm-dialog";
import { EmptyState } from "@/components/empty-state";
import { InviteDialog } from "@/components/invite-dialog";
import { useOrg } from "@/components/org-provider";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import { ApiError, type Invitation } from "@/lib/api";
import { useInvitations, useRevokeInvitation } from "@/lib/queries";
import { formatDate, relativeTime } from "@/lib/format";

function InvitationsView() {
  const params = useSearchParams();
  const { org, can } = useOrg();
  const canManage = can("members:manage");
  const invites = useInvitations(org.id, { enabled: canManage });
  const revokeMutation = useRevokeInvitation(org.id);
  const [inviting, setInviting] = useState(params.get("new") === "1" && canManage);
  const [revoking, setRevoking] = useState<Invitation | null>(null);

  if (!canManage) {
    return (
      <EmptyState
        icon={LockIcon}
        title="Admins only"
        description="Ask a workspace owner or admin to invite people."
      />
    );
  }

  async function revoke(inv: Invitation) {
    try {
      await revokeMutation.mutateAsync(inv.id);
      toast.success(`Invitation to ${inv.email} revoked`);
    } catch (err) {
      toast.error(err instanceof ApiError ? err.message : "Couldn't revoke invitation");
      throw err;
    }
  }

  const list = invites.data ?? [];

  return (
    <div className="flex flex-col gap-6">
      <PageHeader
        title="Invitations"
        description="Pending invites to this workspace. Links expire after 7 days."
        actions={
          <Button onClick={() => setInviting(true)}>
            <UserPlusIcon /> Invite people
          </Button>
        }
      />

      {invites.isPending ? (
        <Skeleton className="h-40 rounded-xl" />
      ) : list.length === 0 ? (
        <EmptyState
          icon={MailIcon}
          title="No pending invitations"
          description="Invite teammates to collaborate on dashboards and insights."
          action={
            <Button onClick={() => setInviting(true)}>
              <UserPlusIcon /> Invite people
            </Button>
          }
        />
      ) : (
        <Card className="py-0">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="pl-4">Email</TableHead>
                <TableHead>Role</TableHead>
                <TableHead className="hidden md:table-cell">Sent</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="w-12 pr-4" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {list.map((inv) => (
                <TableRow key={inv.id}>
                  <TableCell className="pl-4 font-medium">{inv.email}</TableCell>
                  <TableCell>
                    <RoleBadge role={inv.role} />
                  </TableCell>
                  <TableCell className="hidden text-muted-foreground md:table-cell">
                    {formatDate(inv.created_at)}
                  </TableCell>
                  <TableCell className="text-muted-foreground">{relativeTime(inv.expires_at)}</TableCell>
                  <TableCell className="pr-4 text-right">
                    <Button
                      variant="ghost"
                      size="icon-sm"
                      aria-label={`Revoke invitation to ${inv.email}`}
                      onClick={() => setRevoking(inv)}
                    >
                      <XIcon />
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Card>
      )}

      <InviteDialog
        orgId={org.id}
        actorRole={org.role}
        open={inviting}
        onOpenChange={setInviting}
      />
      <ConfirmDialog
        open={!!revoking}
        onOpenChange={(o) => !o && setRevoking(null)}
        title="Revoke invitation?"
        description={`The link sent to ${revoking?.email} will stop working immediately.`}
        confirmLabel="Revoke"
        onConfirm={() => (revoking ? revoke(revoking) : Promise.resolve())}
      />
    </div>
  );
}

export default function InvitationsPage() {
  return (
    <Suspense>
      <InvitationsView />
    </Suspense>
  );
}
