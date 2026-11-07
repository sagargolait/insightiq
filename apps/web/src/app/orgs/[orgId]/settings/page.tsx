"use client";

import { useState } from "react";
import { CheckIcon, CopyIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { useOrg } from "@/components/org-provider";
import { PageHeader } from "@/components/page-header";
import { RoleBadge } from "@/components/role-badge";
import { formatDate } from "@/lib/format";
import { ROLE_DESCRIPTION } from "@/lib/permissions";

function CopyField({ id, label, value }: { id: string; label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <Field>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      <div className="flex gap-2">
        <Input id={id} readOnly value={value} className="font-mono text-xs" />
        <Button
          variant="outline"
          size="icon"
          aria-label={`Copy ${label}`}
          onClick={async () => {
            await navigator.clipboard.writeText(value);
            setCopied(true);
            setTimeout(() => setCopied(false), 1500);
          }}
        >
          {copied ? <CheckIcon /> : <CopyIcon />}
        </Button>
      </div>
    </Field>
  );
}

export default function SettingsPage() {
  const { org, can } = useOrg();

  return (
    <div className="flex flex-col gap-6">
      <PageHeader title="Settings" description="Workspace details and your access." />

      <Card>
        <CardHeader>
          <CardTitle>General</CardTitle>
          <CardDescription>Editing workspace details is coming soon.</CardDescription>
        </CardHeader>
        <CardContent>
          <FieldGroup className="max-w-lg">
            <Field>
              <FieldLabel htmlFor="org-name">Name</FieldLabel>
              <Input id="org-name" value={org.name} disabled />
            </Field>
            <Field>
              <FieldLabel htmlFor="org-slug">Slug</FieldLabel>
              <Input id="org-slug" value={org.slug} disabled />
              <FieldDescription>Created {formatDate(org.created_at)}.</FieldDescription>
            </Field>
            <CopyField id="org-id" label="Workspace ID" value={org.id} />
          </FieldGroup>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Your access</CardTitle>
          <CardDescription>What your role allows in this workspace.</CardDescription>
        </CardHeader>
        <CardContent className="flex items-start gap-3">
          <RoleBadge role={org.role} />
          <p className="text-sm text-muted-foreground">{ROLE_DESCRIPTION[org.role]}</p>
        </CardContent>
      </Card>

      {can("organization:delete") && (
        <Card className="ring-destructive/30">
          <CardHeader>
            <CardTitle className="text-destructive">Danger zone</CardTitle>
            <CardDescription>
              Deleting a workspace removes all dashboards, data sources and conversations for every member.
            </CardDescription>
          </CardHeader>
          <CardFooter className="justify-between gap-4">
            <span className="text-sm text-muted-foreground">Not available yet.</span>
            <Button variant="destructive" disabled>
              Delete workspace
            </Button>
          </CardFooter>
        </Card>
      )}
    </div>
  );
}
