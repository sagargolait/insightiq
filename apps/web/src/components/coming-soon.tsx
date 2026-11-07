import type { LucideIcon } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { PageHeader } from "@/components/page-header";

export function ComingSoon({
  title,
  description,
  icon: Icon,
  features,
  note,
}: {
  title: string;
  description: string;
  icon: LucideIcon;
  features: { title: string; body: string }[];
  note?: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-6">
      <PageHeader title={title} description={description} actions={<Badge variant="secondary">Coming soon</Badge>} />
      <Card className="overflow-hidden">
        <CardContent className="grid gap-8 md:grid-cols-[auto_1fr] md:items-center">
          <div className="mx-auto grid size-24 place-items-center rounded-2xl bg-primary/15 text-brand ring-1 ring-primary/25 md:mx-0">
            <Icon className="size-10" strokeWidth={1.5} />
          </div>
          <div className="grid gap-4 sm:grid-cols-3">
            {features.map((f) => (
              <div key={f.title}>
                <div className="text-sm font-medium">{f.title}</div>
                <p className="mt-1 text-sm text-muted-foreground">{f.body}</p>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
      {note && <p className="text-sm text-muted-foreground">{note}</p>}
    </div>
  );
}
