/**
 * duration formats the elapsed time between two ISO timestamps as a short
 * human string ("420ms", "3.2s", "2m 05s"). Used by the run-page step rail
 * to show each stage's running/finished duration. Returns null when start
 * is not yet set (the stage hasn't started).
 */
export function duration(start?: string, end?: string): string | null {
  if (!start) return null;
  const s = new Date(start).getTime();
  const e = end ? new Date(end).getTime() : Date.now();
  const ms = Math.max(0, e - s);
  if (ms < 1000) return `${ms}ms`;
  if (ms < 60_000) return `${(ms / 1000).toFixed(1)}s`;
  const m = Math.floor(ms / 60_000);
  const sec = Math.floor((ms % 60_000) / 1000);
  return `${m}m ${sec.toString().padStart(2, '0')}s`;
}
