import { translate, type MessageKey } from "./i18n";

// A translator, as useT() returns it. Without one the words are English, so
// callers that do not care (and the tests) keep their old output.
export type Translate = (key: MessageKey, params?: Record<string, string | number>) => string;

const english: Translate = (key, params) => translate("en", key, params);

// Seconds as a short duration such as "3d 4h", "2h 5m", "5m 10s" or "8s"; the
// unit letters follow the language of t ("3g 4h" in Italian).
export function formatDuration(totalSeconds: number, t: Translate = english): string {
  const s = Math.max(0, Math.floor(totalSeconds));
  const days = Math.floor(s / 86400);
  const hours = Math.floor((s % 86400) / 3600);
  const minutes = Math.floor((s % 3600) / 60);
  const seconds = s % 60;
  const d = t("time.day");
  const h = t("time.hour");
  const m = t("time.minute");
  const sec = t("time.second");
  if (days > 0) return `${days}${d} ${hours}${h}`;
  if (hours > 0) return `${hours}${h} ${minutes}${m}`;
  if (minutes > 0) return `${minutes}${m} ${seconds}${sec}`;
  return `${seconds}${sec}`;
}

// How long ago an ISO timestamp was, or "never" when there is none.
export function formatAgo(iso: string | null, now: number = Date.now(), t: Translate = english): string {
  if (iso === null) return t("time.never");
  const seconds = (now - Date.parse(iso)) / 1000;
  return t("time.ago", { duration: formatDuration(seconds, t) });
}

const BYTE_UNITS = ["B", "KiB", "MiB", "GiB", "TiB", "PiB"];

// Bytes with binary units, such as "512 B", "1.5 KiB" or "15.3 GiB".
export function formatBytes(bytes: number): string {
  let value = Math.max(0, bytes);
  let unit = 0;
  while (value >= 1024 && unit < BYTE_UNITS.length - 1) {
    value /= 1024;
    unit++;
  }
  const digits = unit === 0 || value >= 100 ? 0 : 1;
  return `${value.toFixed(digits)} ${BYTE_UNITS[unit]}`;
}

export const formatRate = (bytesPerSecond: number) => `${formatBytes(bytesPerSecond)}/s`;

// A share between 0 and 100, with no decimals from 10 up.
export function formatPercent(percent: number): string {
  return `${percent >= 10 ? Math.round(percent) : percent.toFixed(1)}%`;
}
