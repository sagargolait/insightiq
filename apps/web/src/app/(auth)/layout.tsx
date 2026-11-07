import { LockKeyholeIcon, ShieldCheckIcon, SparklesIcon, TrendingUpIcon, UsersIcon } from "lucide-react";
import { Logo } from "@/components/brand";
import { Sparkline } from "@/components/sparkline";
import { ThemeToggle } from "@/components/theme-toggle";

const points = [
  { icon: ShieldCheckIcon, title: "Tenant-isolated", body: "Row-level security per workspace." },
  { icon: UsersIcon, title: "Role-based access", body: "Owners, admins, analysts, viewers." },
  { icon: LockKeyholeIcon, title: "Secure sessions", body: "HttpOnly cookies, hashed tokens." },
];

const bars = [38, 52, 44, 61, 57, 72, 66, 84, 79, 96];

/** Decorative product preview — illustrative sample numbers only. */
function ProductPreview() {
  return (
    <div aria-hidden className="relative mx-auto h-[340px] w-full max-w-[520px] select-none">
      {/* Main chart panel */}
      <div className="panel absolute inset-x-6 top-6 rounded-xl p-5 ring-1 ring-border">
        <div className="flex items-start justify-between">
          <div>
            <div className="text-xs font-medium text-muted-foreground">Revenue · Q4</div>
            <div className="mt-1.5 text-2xl font-semibold tracking-tight tabular-nums">$519,230</div>
          </div>
          <div className="inline-flex items-center gap-1 rounded-md border border-success/30 bg-success/10 px-1.5 py-0.5 text-xs font-medium text-success">
            <TrendingUpIcon className="size-3" /> 18.4%
          </div>
        </div>
        <div className="mt-5 flex h-28 items-end gap-2">
          {bars.map((h, i) => (
            <div
              key={i}
              className="flex-1 rounded-t-[3px] bg-linear-to-t from-primary/40 to-chart-1"
              style={{ height: `${h}%`, opacity: 0.45 + (i / bars.length) * 0.55 }}
            />
          ))}
        </div>
      </div>

      {/* Floating KPI */}
      <div className="panel absolute bottom-6 -left-2 w-52 rounded-xl p-4 ring-1 ring-border">
        <div className="text-xs font-medium text-muted-foreground">Active customers</div>
        <div className="mt-1 flex items-end justify-between gap-3">
          <div className="text-xl font-semibold tracking-tight tabular-nums">8,249</div>
          <Sparkline data={[3, 4, 3.6, 4.8, 4.4, 5.6, 6.1, 7]} color="var(--chart-2)" className="h-8 w-20" />
        </div>
      </div>

      {/* Copilot bubble */}
      <div className="panel absolute -right-2 bottom-0 w-64 rounded-xl p-4 ring-1 ring-primary/30">
        <div className="flex items-center gap-2 text-xs font-medium text-brand">
          <SparklesIcon className="size-3.5" /> AI Copilot
        </div>
        <p className="mt-2 text-sm leading-snug">
          Revenue is up <span className="font-semibold text-success">18%</span> — driven by enterprise renewals in
          the EU region.
        </p>
      </div>
    </div>
  );
}

export default function AuthLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="grid min-h-svh lg:grid-cols-[minmax(0,1fr)_minmax(0,1.15fr)]">
      <div className="relative flex flex-col p-6 md:p-10">
        <div className="flex items-center justify-between">
          <Logo />
          <ThemeToggle />
        </div>
        <div className="flex flex-1 items-center justify-center py-10">
          <div className="w-full max-w-sm">{children}</div>
        </div>
        <p className="text-center text-xs text-muted-foreground">© {new Date().getFullYear()} InsightIQ</p>
      </div>

      <div className="relative m-3 hidden overflow-hidden rounded-2xl border bg-sidebar lg:flex lg:flex-col lg:justify-center lg:gap-12 lg:p-14">
        <div aria-hidden className="bg-grid pointer-events-none absolute inset-0 opacity-70" />
        <div
          aria-hidden
          className="pointer-events-none absolute -top-40 left-1/2 size-[640px] -translate-x-1/2 rounded-full bg-[radial-gradient(closest-side,var(--glow),transparent)] blur-2xl"
        />
        <div
          aria-hidden
          className="pointer-events-none absolute -right-32 -bottom-40 size-[480px] rounded-full bg-[radial-gradient(closest-side,color-mix(in_srgb,var(--chart-4)_22%,transparent),transparent)] blur-2xl"
        />

        <div className="relative max-w-lg">
          <div className="inline-flex items-center gap-2 rounded-full border bg-background/50 px-3 py-1 text-xs font-medium text-muted-foreground backdrop-blur">
            <span className="size-1.5 rounded-full bg-success shadow-[0_0_8px_var(--success)]" />
            Business intelligence, with an AI analyst built in
          </div>
          <h2 className="mt-5 text-4xl leading-[1.1] font-semibold tracking-tight text-balance">
            Turn your business data into <span className="text-gradient">answers.</span>
          </h2>
          <p className="mt-4 max-w-md text-base text-muted-foreground">
            Dashboards, an AI copilot and your data sources — in one shared, secure workspace.
          </p>
        </div>

        <div className="relative">
          <ProductPreview />
        </div>

        <ul className="relative grid grid-cols-3 gap-4">
          {points.map(({ icon: Icon, title, body }) => (
            <li key={title} className="flex flex-col gap-2">
              <Icon className="size-4 text-brand" />
              <div className="text-sm font-medium">{title}</div>
              <div className="text-xs leading-relaxed text-muted-foreground">{body}</div>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}
