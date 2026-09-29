export function formatBytes(n: number | null | undefined): string {
  if (n == null || !Number.isFinite(n)) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v >= 100 || i === 0 ? v.toFixed(0) : v.toFixed(1)} ${units[i]}`;
}

export function formatDate(s: string | null | undefined): string {
  if (!s) return "—";
  const d = new Date(s);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "—";
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function formatRelative(s: string | null | undefined, now = Date.now()): string {
  if (!s) return "—";
  const t = new Date(s).getTime();
  if (Number.isNaN(t) || t < 946684800000) return "—";
  const diff = Math.round((t - now) / 1000);
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
  if (abs < 60) return rtf.format(diff, "second");
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute");
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour");
  return rtf.format(Math.round(diff / 86400), "day");
}

export function formatDuration(seconds: number): string {
  if (!seconds || seconds < 0) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  return `${m}m`;
}

// parseMinutes turns "15, 5 1" into [15, 5, 1]: positive, unique, descending.
export function parseMinutes(s: string): number[] {
  return [
    ...new Set(
      s
        .split(/[\s,;]+/)
        .map(Number)
        .filter((n) => Number.isInteger(n) && n > 0),
    ),
  ].sort((a, b) => b - a);
}
