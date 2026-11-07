"use client";

import { useState } from "react";
import { CheckIcon, CopyIcon, Loader2Icon, TriangleAlertIcon } from "lucide-react";
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/components/form-error";
import { RoleSelect } from "@/components/role-select";
import { ApiError, type CreatedInvitation } from "@/lib/api";
import { useInvite } from "@/lib/queries";
import { assignableRoles, type Role } from "@/lib/permissions";

export function InviteDialog({
  orgId,
  actorRole,
  open,
  onOpenChange,
  onCreated,
}: {
  orgId: string;
  actorRole: Role;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Cache is updated automatically; this is for extra side effects. */
  onCreated?: (inv: CreatedInvitation) => void;
}) {
  const [email, setEmail] = useState("");
  const [role, setRole] = useState<Role>("viewer");
  const invite = useInvite(orgId);
  const pending = invite.isPending;
  const [error, setError] = useState<string | null>(null);
  const [created, setCreated] = useState<CreatedInvitation | null>(null);
  const [copied, setCopied] = useState(false);

  const link =
    created && typeof window !== "undefined"
      ? `${window.location.origin}/invite?token=${encodeURIComponent(created.token)}`
      : "";

  function close(o: boolean) {
    onOpenChange(o);
    if (!o) {
      setEmail("");
      setRole("viewer");
      setError(null);
      setCreated(null);
      setCopied(false);
    }
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const inv = await invite.mutateAsync({ email: email.trim(), role });
      setCreated(inv);
      onCreated?.(inv);
    } catch (err) {
      setError(
        err instanceof ApiError && err.status === 409
          ? `${email} already has an open invitation or is a member.`
          : err instanceof ApiError
            ? err.message
            : "Something went wrong.",
      );
    }
  }

  async function copy() {
    await navigator.clipboard.writeText(link);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        {created ? (
          <div className="grid gap-5">
            <DialogHeader>
              <DialogTitle>Invitation created</DialogTitle>
              <DialogDescription>
                Send this link to <span className="font-medium text-foreground">{created.email}</span>. It
                expires in 7 days.
              </DialogDescription>
            </DialogHeader>
            <div className="flex gap-2">
              <Input readOnly value={link} className="font-mono text-xs" onFocus={(e) => e.target.select()} />
              <Button variant="outline" size="icon" onClick={copy} aria-label="Copy invite link">
                {copied ? <CheckIcon /> : <CopyIcon />}
              </Button>
            </div>
            <Alert>
              <TriangleAlertIcon />
              <AlertTitle>Copy it now</AlertTitle>
              <AlertDescription>
                For security we only store a hash of this link. You won&apos;t be able to see it again — revoke and
                re-invite if it&apos;s lost.
              </AlertDescription>
            </Alert>
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </div>
        ) : (
          <form onSubmit={onSubmit} className="grid gap-5">
            <DialogHeader>
              <DialogTitle>Invite to workspace</DialogTitle>
              <DialogDescription>They&apos;ll join with the role you choose. You can change it later.</DialogDescription>
            </DialogHeader>
            <FormError message={error} />
            <FieldGroup>
              <Field>
                <FieldLabel htmlFor="invite-email">Email</FieldLabel>
                <Input
                  id="invite-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="teammate@company.com"
                  maxLength={320}
                  required
                  autoFocus
                />
              </Field>
              <Field>
                <FieldLabel htmlFor="invite-role">Role</FieldLabel>
                <RoleSelect id="invite-role" value={role} roles={assignableRoles(actorRole)} onChange={setRole} className="w-full" />
              </Field>
            </FieldGroup>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => close(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={pending || !email.trim()}>
                {pending && <Loader2Icon className="animate-spin" />}
                Create invite link
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  );
}
