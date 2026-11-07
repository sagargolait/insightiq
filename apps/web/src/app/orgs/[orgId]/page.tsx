"use client";

import Link from "next/link";
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from "recharts";
import {
  ArrowRightIcon,
  ArrowUpRightIcon,
  BarChart3Icon,
  CheckIcon,
  DatabaseIcon,
  PercentIcon,
  TrendingDownIcon,
  TrendingUpIcon,
  UserPlusIcon,
  UsersIcon,
  WalletIcon,
  type LucideIcon,
} from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { buttonVariants } from "@/components/ui/button";
import { Card, CardAction, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart";
import { Skeleton } from "@/components/ui/skeleton";
import { useOrg } from "@/components/org-provider";
import { RoleBadge } from "@/components/role-badge";
import { useSession } from "@/components/session-provider";
import { Sparkline } from "@/components/sparkline";
import { UserAvatar } from "@/components/user-menu";
import { useInvitations, useMembers } from "@/lib/queries";
import { cn } from "@/lib/utils";

// Sample data until data sources + dashboards ship (see Milestone 2).
type Kpi = {
  label: string;
  value: string;
  change: number;
  icon: LucideIcon;
  color: string;
  trend: number[];
  lowerIsBetter?: boolean;
};

const kpis: Kpi[] = [
  { label: "Total revenue", value: "$128,430", change: 12.8, icon: WalletIcon, color: "var(--chart-1)", trend: [42, 48, 45, 52, 58, 55, 63, 70, 68, 79, 88, 96] },
  { label: "Active customers", value: "8,249", change: 8.2, icon: UsersIcon, color: "var(--chart-2)", trend: [60, 62, 61, 65, 64, 68, 70, 69, 73, 74, 77, 80] },
  { label: "Conversion rate", value: "4.82%", change: 1.4, icon: PercentIcon, color: "var(--chart-4)", trend: [4.1, 4.3, 4.2, 4.4, 4.3, 4.5, 4.6, 4.5, 4.7, 4.6, 4.8, 4.82] },
  { label: "Monthly churn", value: "2.14%", change: -0.6, icon: TrendingDownIcon, color: "var(--chart-5)", trend: [3.1, 3.0, 2.9, 2.95, 2.7, 2.6, 2.5, 2.45, 2.3, 2.25, 2.2, 2.14], lowerIsBetter: true },
];

const revenue = [
  { month: "May", revenue: 64200, target: 60000 },
  { month: "Jun", revenue: 71800, target: 66000 },
  { month: "Jul", revenue: 69400, target: 72000 },
  { month: "Aug", revenue: 88100, target: 78000 },
  { month: "Sep", revenue: 97300, target: 84000 },
  { month: "Oct", revenue: 128430, target: 90000 },
];
const revenueTotal = revenue.reduce((s, r) => s + r.revenue, 0);
const targetTotal = revenue.reduce((s, r) => s + r.target, 0);
const vsTarget = ((revenueTotal / targetTotal - 1) * 100).toFixed(1);

const chartConfig = {
  revenue: { label: "Revenue", color: "var(--chart-1)" },
  target: { label: "Target", color: "var(--chart-2)" },
} satisfies ChartConfig;

const usd = new Intl.NumberFormat("en-US", { style: "currency", currency: "USD", maximumFractionDigits: 0 });

function greeting() {
  const h = new Date().getHours();
  return h < 12 ? "Good morning" : h < 18 ? "Good afternoon" : "Good evening";
}

function ProgressRing({ value, total }: { value: number; total: number }) {
  const r = 22;
  const c = 2 * Math.PI * r;
  return (
    <div className="relative grid size-14 shrink-0 place-items-center">
      <svg viewBox="0 0 56 56" className="absolute inset-0 -rotate-90" aria-hidden>
        <circle cx="28" cy="28" r={r} fill="none" stroke="var(--muted)" strokeWidth="5" />
        <circle
          cx="28"
          cy="28"
          r={r}
          fill="none"
          stroke="var(--chart-1)"
          strokeWidth="5"
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={c * (1 - value / total)}
          className="transition-[stroke-dashoffset] duration-700"
        />
      </svg>
      <span className="text-sm font-semibold tabular-nums">
        {value}/{total}
      </span>
    </div>
  );
}

export default function OverviewPage() {
  const { user } = useSession();
  const { org, can } = useOrg();
  const members = useMembers(org.id);
  const canInvite = can("members:manage");
  const invites = useInvitations(org.id, { enabled: canInvite });

  const teamGrown = (members.data?.length ?? 0) > 1 || (invites.data?.length ?? 0) > 0;
  const steps = [
    { title: "Create your workspace", body: "You're in. Nice.", icon: CheckIcon, done: true },
    {
      title: "Invite your team",
      body: "Bring in analysts and stakeholders.",
      icon: UserPlusIcon,
      done: teamGrown,
      href: canInvite ? `/orgs/${org.id}/invitations?new=1` : undefined,
    },
    { title: "Connect a data source", body: "Postgres, BigQuery, CSV and more.", icon: DatabaseIcon, done: false, soon: true },
    { title: "Build your first dashboard", body: "Turn queries into shareable views.", icon: BarChart3Icon, done: false, soon: true },
  ];
  const doneCount = steps.filter((s) => s.done).length;
  const today = new Intl.DateTimeFormat(undefined, { weekday: "long", month: "long", day: "numeric" }).format(new Date());

  return (
    <div className="flex flex-col gap-6">
      {/* Hero */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
        <div className="min-w-0">
          <p className="label-caps">{today}</p>
          <h1 className="mt-2 text-2xl font-semibold tracking-tight md:text-[2rem] md:leading-tight">
            {greeting()}, <span className="text-gradient">{user.name.split(" ")[0]}</span>
          </h1>
          <p className="mt-1 text-sm text-muted-foreground">Here&apos;s how {org.name} is trending this period.</p>
        </div>
        <div className="flex shrink-0 gap-2">
          <Link href={`/orgs/${org.id}/members`} className={buttonVariants({ variant: "outline" })}>
            <UsersIcon /> Team
          </Link>
          {canInvite && (
            <Link href={`/orgs/${org.id}/invitations?new=1`} className={buttonVariants()}>
              <UserPlusIcon /> Invite people
            </Link>
          )}
        </div>
      </div>

      {/* KPIs */}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {kpis.map((k) => {
          const good = k.lowerIsBetter ? k.change < 0 : k.change > 0;
          const Trend = k.change > 0 ? TrendingUpIcon : TrendingDownIcon;
          return (
            <Card key={k.label} className="gap-3 pb-0">
              <CardHeader>
                <div className="flex items-center gap-2.5">
                  <div
                    className="grid size-8 place-items-center rounded-lg ring-1 ring-inset"
                    style={{
                      color: k.color,
                      background: `color-mix(in srgb, ${k.color} 14%, transparent)`,
                      ["--tw-ring-color" as string]: `color-mix(in srgb, ${k.color} 28%, transparent)`,
                    }}
                  >
                    <k.icon className="size-4" />
                  </div>
                  <CardDescription className="font-medium">{k.label}</CardDescription>
                </div>
                <CardAction>
                  <Badge
                    variant="outline"
                    className={cn(
                      "gap-1 tabular-nums",
                      good
                        ? "border-success/30 bg-success/10 text-success"
                        : "border-destructive/30 bg-destructive/10 text-destructive",
                    )}
                  >
                    <Trend />
                    {k.change > 0 ? "+" : ""}
                    {k.change}%
                  </Badge>
                </CardAction>
              </CardHeader>
              <CardContent className="flex items-end justify-between gap-4">
                <div className="pb-4">
                  <div className="text-[1.75rem] leading-none font-semibold tracking-tight tabular-nums">{k.value}</div>
                  <div className="mt-2 text-xs text-muted-foreground">vs. previous 30 days</div>
                </div>
                <Sparkline data={k.trend} color={k.color} className="mb-4 h-10 w-24 shrink-0" />
              </CardContent>
            </Card>
          );
        })}
      </div>

      <div className="grid gap-6 xl:grid-cols-[1.7fr_1fr]">
        {/* Revenue */}
        <Card>
          <CardHeader>
            <CardDescription className="font-medium">Revenue · last 6 months</CardDescription>
            <CardTitle className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
              <span className="text-[1.75rem] leading-none tracking-tight tabular-nums">{usd.format(revenueTotal)}</span>
              <span className="inline-flex items-center gap-1 text-sm font-medium text-success">
                <ArrowUpRightIcon className="size-4" />
                {vsTarget}% above target
              </span>
            </CardTitle>
            <CardAction className="flex items-center gap-3 text-xs text-muted-foreground">
              <span className="hidden items-center gap-1.5 sm:inline-flex">
                <span className="size-2 rounded-full bg-chart-1" /> Revenue
              </span>
              <span className="hidden items-center gap-1.5 sm:inline-flex">
                <span className="h-0.5 w-3 rounded-full bg-chart-2" /> Target
              </span>
              <Badge variant="outline" className="font-normal text-muted-foreground">
                Sample data
              </Badge>
            </CardAction>
          </CardHeader>
          <CardContent>
            <ChartContainer config={chartConfig} className="aspect-auto h-72 w-full">
              <AreaChart data={revenue} margin={{ left: 4, right: 8, top: 12 }}>
                <defs>
                  <linearGradient id="fillRevenue" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-revenue)" stopOpacity={0.45} />
                    <stop offset="100%" stopColor="var(--color-revenue)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid vertical={false} strokeDasharray="3 6" />
                <XAxis dataKey="month" tickLine={false} axisLine={false} tickMargin={10} />
                <YAxis
                  tickLine={false}
                  axisLine={false}
                  width={44}
                  tickFormatter={(v: number) => `$${Math.round(v / 1000)}k`}
                />
                <ChartTooltip cursor={{ stroke: "var(--border)" }} content={<ChartTooltipContent indicator="line" />} />
                <Area
                  dataKey="target"
                  type="monotone"
                  stroke="var(--color-target)"
                  strokeDasharray="5 5"
                  fill="none"
                  strokeWidth={1.5}
                />
                <Area
                  dataKey="revenue"
                  type="monotone"
                  stroke="var(--color-revenue)"
                  fill="url(#fillRevenue)"
                  strokeWidth={2.5}
                  activeDot={{ r: 5, strokeWidth: 2, stroke: "var(--background)" }}
                />
              </AreaChart>
            </ChartContainer>
          </CardContent>
        </Card>

        {/* Get started */}
        <Card>
          <CardHeader className="flex items-center gap-4">
            <ProgressRing value={doneCount} total={steps.length} />
            <div>
              <CardTitle>Get started</CardTitle>
              <CardDescription>Finish setting up {org.name}.</CardDescription>
            </div>
          </CardHeader>
          <CardContent>
            <ul className="flex flex-col gap-2">
              {steps.map((s) => {
                const Icon = s.done ? CheckIcon : s.icon;
                const body = (
                  <>
                    <div
                      className={cn(
                        "grid size-9 shrink-0 place-items-center rounded-lg ring-1 ring-inset",
                        s.done
                          ? "bg-success/12 text-success ring-success/25"
                          : "bg-muted text-muted-foreground ring-border",
                      )}
                    >
                      <Icon className="size-4" />
                    </div>
                    <div className="min-w-0 flex-1">
                      <div className={cn("text-sm font-medium", s.done && "text-muted-foreground")}>{s.title}</div>
                      <div className="truncate text-xs text-muted-foreground">{s.body}</div>
                    </div>
                    {s.soon && (
                      <Badge variant="outline" className="font-normal text-muted-foreground">
                        Soon
                      </Badge>
                    )}
                    {s.href && !s.done && (
                      <ArrowRightIcon className="size-4 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                    )}
                  </>
                );
                return (
                  <li key={s.title}>
                    {s.href && !s.done ? (
                      <Link
                        href={s.href}
                        className="group flex items-center gap-3 rounded-lg border border-primary/30 bg-primary/8 p-2.5 transition-colors hover:bg-primary/14"
                      >
                        {body}
                      </Link>
                    ) : (
                      <div className="flex items-center gap-3 rounded-lg border border-transparent p-2.5">{body}</div>
                    )}
                  </li>
                );
              })}
            </ul>
          </CardContent>
        </Card>
      </div>

      {/* Team */}
      <Card>
        <CardHeader>
          <CardTitle>Team</CardTitle>
          <CardDescription>
            {members.data
              ? `${members.data.length} member${members.data.length === 1 ? "" : "s"}${
                  invites.data?.length ? ` · ${invites.data.length} pending invite${invites.data.length === 1 ? "" : "s"}` : ""
                }`
              : "Loading…"}
          </CardDescription>
          <CardAction>
            <Link href={`/orgs/${org.id}/members`} className={buttonVariants({ variant: "ghost", size: "sm" })}>
              View all <ArrowRightIcon />
            </Link>
          </CardAction>
        </CardHeader>
        <CardContent>
          {members.isPending ? (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {Array.from({ length: 3 }).map((_, i) => (
                <Skeleton key={i} className="h-16 rounded-lg" />
              ))}
            </div>
          ) : (
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {members.data?.slice(0, 6).map((m) => (
                <div
                  key={m.user_id}
                  className="flex items-center gap-3 rounded-lg border bg-background/40 p-3 transition-colors hover:border-primary/30"
                >
                  <UserAvatar user={m} className="size-9" />
                  <div className="grid min-w-0 flex-1 text-sm leading-tight">
                    <span className="truncate font-medium">
                      {m.name}
                      {m.user_id === user.id && <span className="font-normal text-muted-foreground"> (you)</span>}
                    </span>
                    <span className="truncate text-xs text-muted-foreground">{m.email}</span>
                  </div>
                  <RoleBadge role={m.role} />
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
