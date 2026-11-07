export function initials(name: string): string {
  return (
    name
      .trim()
      .split(/\s+/)
      .slice(0, 2)
      .map((p) => p[0]?.toUpperCase() ?? "")
      .join("") || "?"
  );
}

const dateFmt = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
export const formatDate = (iso: string) => dateFmt.format(new Date(iso));

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
export function relativeTime(iso: string): string {
  const diffMs = new Date(iso).getTime() - Date.now();
  const days = Math.round(diffMs / 86_400_000);
  if (Math.abs(days) >= 1) return rtf.format(days, "day");
  const hours = Math.round(diffMs / 3_600_000);
  return rtf.format(hours, "hour");
}

/** Matches the API's slug rule: ^[a-z0-9]+(-[a-z0-9]+)*$, 3–100 chars. */
export const SLUG_PATTERN = /^[a-z0-9]+(-[a-z0-9]+)*$/;

export function slugify(input: string): string {
  return input
    .toLowerCase()
    .normalize("NFKD")
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 100);
}

/** Only allow same-site relative redirects (blocks //evil.com). */
export function safeNext(next: string | null | undefined, fallback = "/orgs"): string {
  return next && /^\/(?![/\\])/.test(next) ? next : fallback;
}
