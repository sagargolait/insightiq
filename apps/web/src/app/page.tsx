const metrics = [
  { label: "Total revenue", value: "$128,430", change: "+12.8%" },
  { label: "Active customers", value: "8,249", change: "+8.2%" },
  { label: "Conversion rate", value: "4.82%", change: "+1.4%" },
  { label: "Monthly churn", value: "2.14%", change: "-0.6%" }
];

export default function Home() {
  return (
    <main className="min-h-screen">
      <div className="mx-auto flex min-h-screen max-w-[1440px]">
        <aside className="hidden w-64 shrink-0 border-r border-white/10 bg-[#0b1220]/80 p-6 md:block">
          <div className="mb-12 flex items-center gap-3">
            <div className="grid h-9 w-9 place-items-center rounded-xl bg-indigo-400 text-lg font-bold text-slate-950">I</div>
            <div><div className="font-semibold tracking-wide">InsightIQ</div><div className="text-xs text-slate-400">BUSINESS INTELLIGENCE</div></div>
          </div>
          <div className="mb-3 text-xs font-semibold uppercase tracking-widest text-slate-500">Workspace</div>
          {["Overview", "Dashboards", "AI Copilot", "Data sources"].map((item, i) => (
            <div key={item} className={`mb-1 rounded-xl px-3 py-2.5 text-sm ${i === 0 ? "bg-indigo-400/15 text-indigo-200" : "text-slate-400 hover:bg-white/5"}`}>{item}</div>
          ))}
          <div className="mt-10 mb-3 text-xs font-semibold uppercase tracking-widest text-slate-500">Manage</div>
          {["Team members", "Settings", "Usage & billing"].map(item => <div key={item} className="mb-1 rounded-xl px-3 py-2.5 text-sm text-slate-400">{item}</div>)}
          <div className="mt-auto" />
          <div className="mt-20 rounded-2xl border border-white/10 bg-white/[0.03] p-4">
            <div className="text-sm font-medium">InsightIQ Pro</div><p className="mt-1 text-xs leading-5 text-slate-400">Your workspace is ready to grow.</p>
          </div>
        </aside>
        <section className="min-w-0 flex-1 p-6 md:p-10">
          <header className="mb-10 flex items-center justify-between">
            <div><div className="text-sm text-slate-400">Workspace / Overview</div><h1 className="mt-2 text-3xl font-semibold tracking-tight">Good morning</h1><p className="mt-2 text-sm text-slate-400">Here's what's happening with your business today.</p></div>
            <div className="rounded-xl border border-white/10 bg-white/5 px-4 py-2 text-sm text-slate-300">Demo workspace ▾</div>
          </header>
          <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
            {metrics.map(m => <div key={m.label} className="rounded-2xl border border-white/10 bg-[#111a2b]/80 p-5 shadow-xl shadow-black/10"><div className="text-sm text-slate-400">{m.label}</div><div className="mt-4 text-3xl font-semibold tracking-tight">{m.value}</div><div className="mt-3 text-xs text-emerald-300">{m.change} <span className="text-slate-500">vs. previous period</span></div></div>)}
          </div>
          <div className="mt-6 grid gap-6 xl:grid-cols-[1.6fr_1fr]">
            <div className="rounded-2xl border border-white/10 bg-[#111a2b]/80 p-6">
              <div className="flex items-center justify-between"><div><h2 className="font-semibold">Revenue overview</h2><p className="mt-1 text-xs text-slate-400">Monthly performance · Demo data</p></div><div className="rounded-lg border border-white/10 px-3 py-2 text-xs text-slate-300">Last 6 months</div></div>
              <div className="mt-8 flex h-56 items-end gap-3 border-b border-l border-white/10 px-3 pb-0">
                {[38,52,44,68,59,82,72,94,76,100,87,112].map((h,i) => <div key={i} className="flex-1 rounded-t-md bg-gradient-to-t from-indigo-500/40 to-indigo-300" style={{height:`${h/1.2}%`}} />)}
              </div>
              <div className="mt-3 flex justify-between text-xs text-slate-500"><span>May</span><span>Jun</span><span>Jul</span><span>Aug</span><span>Sep</span><span>Oct</span></div>
            </div>
            <div className="rounded-2xl border border-white/10 bg-[#111a2b]/80 p-6">
              <h2 className="font-semibold">AI Copilot</h2><p className="mt-1 text-xs text-slate-400">Ask questions about your business data.</p>
              <div className="mt-6 rounded-xl bg-indigo-400/10 p-4 text-sm leading-6 text-indigo-100">What were our top-performing products this month?</div>
              <div className="mt-4 rounded-xl border border-white/10 p-4 text-sm leading-6 text-slate-300">Connect a data source to start getting AI-powered insights. Your copilot will be available here.</div>
              <button className="mt-5 w-full rounded-xl bg-indigo-300 px-4 py-3 text-sm font-semibold text-slate-950">Connect data source</button>
            </div>
          </div>
          <p className="mt-8 text-center text-xs text-slate-600">InsightIQ · Milestone 1 · Demo interface with sample data</p>
        </section>
      </div>
    </main>
  );
}
