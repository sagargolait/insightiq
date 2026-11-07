"use client";

import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { ROLE_DESCRIPTION, ROLE_LABEL, type Role } from "@/lib/permissions";

export function RoleSelect({
  value,
  roles,
  onChange,
  disabled,
  id,
  className,
}: {
  value: Role;
  roles: Role[];
  onChange: (role: Role) => void;
  disabled?: boolean;
  id?: string;
  className?: string;
}) {
  return (
    <Select
      value={value}
      onValueChange={(v) => v && onChange(v as Role)}
      disabled={disabled}
      items={roles.map((r) => ({ value: r, label: ROLE_LABEL[r] }))}
    >
      <SelectTrigger id={id} className={className ?? "w-32"} size="sm">
        <SelectValue />
      </SelectTrigger>
      <SelectContent className="w-72">
        {roles.map((r) => (
          <SelectItem key={r} value={r}>
            <div className="grid gap-0.5">
              <span>{ROLE_LABEL[r]}</span>
              <span className="text-xs text-muted-foreground whitespace-normal">{ROLE_DESCRIPTION[r]}</span>
            </div>
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
