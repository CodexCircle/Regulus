// Seconds as a short duration such as "3d 4h", "2h 5m", "5m 10s" or "8s".
export function formatDuration(totalSeconds: number): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  if (days > 0) return `${days}d ${hours}h`;
  if (hours > 0) return `${hours}h ${minutes}m`;
  if (minutes > 0) return `${minutes}m ${seconds}s`;
  return `${seconds}s`;
}

// How long ago an ISO timestamp was, or "never" when there is none.
export function formatAgo(iso: string | null, now: number = Date.now()): string {
  if (iso === null) return "never";
  const seconds = (now - Date.parse(iso)) / 1000;
  return `${formatDuration(seconds)} ago`;
}
