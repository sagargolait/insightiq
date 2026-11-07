"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { Loader2Icon } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/components/form-error";
import { ApiError } from "@/lib/api";
import { useCreateOrg } from "@/lib/queries";
import { SLUG_PATTERN, slugify } from "@/lib/format";

export function CreateOrgDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const createOrg = useCreateOrg();
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [slugTouched, setSlugTouched] = useState(false);
  const pending = createOrg.isPending;
  const [error, setError] = useState<ApiError | null>(null);

  const effectiveSlug = slugTouched ? slug : slugify(name);
  const slugInvalid =
    effectiveSlug.length > 0 && (effectiveSlug.length < 3 || !SLUG_PATTERN.test(effectiveSlug));
  const slugTaken = error?.code === "SLUG_TAKEN";

  function reset() {
    setName("");
    setSlug("");
    setSlugTouched(false);
    setError(null);
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    try {
      const org = await createOrg.mutateAsync({ name: name.trim(), slug: effectiveSlug });
      toast.success(`${org.name} created`);
      onOpenChange(false);
      reset();
      router.push(`/orgs/${org.id}`);
    } catch (err) {
      setError(err instanceof ApiError ? err : new ApiError(0, "UNKNOWN", "Something went wrong."));
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) reset();
      }}
    >
      <DialogContent className="sm:max-w-md">
        <form onSubmit={onSubmit} className="grid gap-5">
          <DialogHeader>
            <DialogTitle>Create a workspace</DialogTitle>
            <DialogDescription>
              Workspaces hold your dashboards, data sources and team. You&apos;ll be the owner.
            </DialogDescription>
          </DialogHeader>
          {!slugTaken && <FormError message={error?.message} />}
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="org-name">Name</FieldLabel>
              <Input
                id="org-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="Acme Inc."
                maxLength={150}
                required
                autoFocus
              />
            </Field>
            <Field data-invalid={slugInvalid || slugTaken || undefined}>
              <FieldLabel htmlFor="org-slug">URL slug</FieldLabel>
              <div className="flex items-center rounded-lg border border-input dark:bg-input/30 focus-within:border-ring focus-within:ring-3 focus-within:ring-ring/50">
                <span className="pl-2.5 text-sm text-muted-foreground">insightiq.app/</span>
                <Input
                  id="org-slug"
                  value={effectiveSlug}
                  onChange={(e) => {
                    setSlugTouched(true);
                    setSlug(e.target.value.toLowerCase());
                  }}
                  className="border-0 bg-transparent! pl-0.5 shadow-none ring-0! focus-visible:ring-0"
                  aria-invalid={slugInvalid || slugTaken}
                  maxLength={100}
                  required
                />
              </div>
              {slugTaken ? (
                <FieldError>{error?.message}</FieldError>
              ) : slugInvalid ? (
                <FieldError>3–100 characters: lowercase letters, numbers and single hyphens.</FieldError>
              ) : (
                <FieldDescription>Lowercase letters, numbers and hyphens.</FieldDescription>
              )}
            </Field>
          </FieldGroup>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={pending || !name.trim() || slugInvalid || !effectiveSlug}>
              {pending && <Loader2Icon className="animate-spin" />}
              Create workspace
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
