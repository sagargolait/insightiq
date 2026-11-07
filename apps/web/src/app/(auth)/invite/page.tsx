"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { Loader2Icon, MailOpenIcon } from "lucide-react";
import { Button, buttonVariants } from "@/components/ui/button";
import { FormError } from "@/components/form-error";
import { ApiError } from "@/lib/api";
import { useAcceptInvitation, useMe } from "@/lib/queries";

function AcceptInvite() {
  const router = useRouter();
  const params = useSearchParams();
  const token = params.get("token") ?? "";
  // Signed-out is a valid state here: show sign-in links instead of redirecting.
  const me = useMe({ authOptional: true });
  const user = me.isPending ? undefined : (me.data ?? null);
  const acceptInvitation = useAcceptInvitation();
  // Stays true through the redirect after success.
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const here = `/invite?token=${encodeURIComponent(token)}`;

  async function accept() {
    setPending(true);
    setError(null);
    try {
      const org = await acceptInvitation.mutateAsync(token);
      router.replace(`/orgs/${org.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "Something went wrong. Try again.");
      setPending(false);
    }
  }

  if (!token) {
    return <FormError message="This invitation link is missing its token. Ask for a new invite." />;
  }

  return (
    <div className="flex flex-col gap-6">
      <div className="grid size-11 place-items-center rounded-full bg-accent text-accent-foreground">
        <MailOpenIcon className="size-5" />
      </div>
      <div>
        <h1 className="text-3xl font-semibold tracking-tight">You&apos;ve been invited</h1>
        <p className="mt-1 text-sm text-muted-foreground">
          Join your team&apos;s InsightIQ workspace. Invitations are tied to the email they were sent to.
        </p>
      </div>
      <FormError message={error} />

      {user === undefined && <Loader2Icon className="size-5 animate-spin text-muted-foreground" />}

      {user === null && (
        <div className="flex flex-col gap-2">
          <Link href={{ pathname: "/login", query: { next: here } }} className={buttonVariants({ size: "lg" })}>
            Sign in to accept
          </Link>
          <Link
            href={{ pathname: "/register", query: { next: here } }}
            className={buttonVariants({ size: "lg", variant: "outline" })}
          >
            Create an account
          </Link>
        </div>
      )}

      {user && (
        <div className="flex flex-col gap-3">
          <p className="rounded-lg border bg-muted/40 px-3 py-2 text-sm">
            Signed in as <span className="font-medium">{user.email}</span>
          </p>
          <Button size="lg" onClick={accept} disabled={pending}>
            {pending && <Loader2Icon className="animate-spin" />}
            Accept invitation
          </Button>
        </div>
      )}
    </div>
  );
}

export default function InvitePage() {
  return (
    <Suspense>
      <AcceptInvite />
    </Suspense>
  );
}
