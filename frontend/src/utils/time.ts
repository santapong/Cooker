/**
 * timeAgo — compact relative time for list meta: "just now", "4m ago",
 * "in 2h", "3d ago"; beyond a week, the local date. Unknown → "—".
 */
export function timeAgo(iso: string | null | undefined, now: number = Date.now()): string {
  if (!iso) return '—';
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return '—';
  const diff = t - now;
  const abs = Math.abs(diff);
  const future = diff > 0;
  if (abs < 45_000) return future ? 'in a moment' : 'just now';
  const units: [number, string][] = [
    [60_000, 'm'],
    [3_600_000, 'h'],
    [86_400_000, 'd'],
  ];
  let label = '';
  if (abs < 3_600_000) label = `${Math.round(abs / units[0][0])}${units[0][1]}`;
  else if (abs < 86_400_000) label = `${Math.round(abs / units[1][0])}${units[1][1]}`;
  else if (abs < 7 * 86_400_000) label = `${Math.round(abs / units[2][0])}${units[2][1]}`;
  else return new Date(t).toLocaleDateString();
  return future ? `in ${label}` : `${label} ago`;
}

/** Short id for mono meta: first 8 chars. */
export function shortId(id: string | null | undefined): string {
  return (id ?? '').slice(0, 8);
}
