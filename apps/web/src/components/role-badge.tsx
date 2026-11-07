import { Badge } from "@/components/ui/badge";
import { ROLE_LABEL, type Role } from "@/lib/permissions";
import { cn } from "@/lib/utils";

const tone: Record<Role, string> = {
  owner: "bg-primary/15 text-brand border-primary/30",
  admin: "bg-chart-2/15 text-foreground border-chart-2/30",
  analyst: "bg-chart-3/15 text-foreground border-chart-3/30",
  viewer: "bg-muted text-muted-foreground border-border",
};

export function RoleBadge({ role, className }: { role: Role; className?: string }) {
  return (
    <Badge variant="outline" className={cn(tone[role], className)}>
      {ROLE_LABEL[role]}
    </Badge>
  );
}
