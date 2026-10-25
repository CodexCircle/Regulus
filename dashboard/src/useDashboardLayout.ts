import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError, fetchLayout, saveLayout } from "./api";
import { defaultLayout, parseLayout, type DashboardLayout } from "./layout";
import type { WidgetRegistry } from "./widgets/registry";

export type SaveState = "idle" | "saving" | "saved" | "error";

type Loaded = { status: "loading" } | { status: "error"; message: string } | { status: "ready"; layout: DashboardLayout };

interface Options {
  // Wait this long after the last change before saving (0 saves at once).
  saveDelayMs?: number;
  onUnauthorized?: () => void;
}

// useDashboardLayout loads the layout of the logged-in user and saves every
// change after a short delay. A user without a layout gets the default one.
export function useDashboardLayout(registry: WidgetRegistry, { saveDelayMs = 800, onUnauthorized }: Options = {}) {
  const [loaded, setLoaded] = useState<Loaded>({ status: "loading" });
  const [saveState, setSaveState] = useState<SaveState>("idle");
  const pending = useRef<DashboardLayout | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const unauthorized = useRef(onUnauthorized);
  useEffect(() => {
    unauthorized.current = onUnauthorized;
  }, [onUnauthorized]);

  useEffect(() => {
    const controller = new AbortController();
    fetchLayout(controller.signal).then(
      (res) => setLoaded({ status: "ready", layout: parseLayout(res.layout) ?? defaultLayout(registry) }),
      (err: unknown) => {
        if (controller.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) {
          unauthorized.current?.();
          return;
        }
        setLoaded({ status: "error", message: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => controller.abort();
  }, [registry]);

  const flush = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = undefined;
    const layout = pending.current;
    if (!layout) return Promise.resolve();
    pending.current = null;
    setSaveState("saving");
    return saveLayout(layout).then(
      () => setSaveState((s) => (pending.current ? s : "saved")),
      (err: unknown) => {
        if (err instanceof ApiError && err.status === 401) unauthorized.current?.();
        setSaveState("error");
      },
    );
  }, []);

  // Changes still waiting when the dashboard closes are saved right away.
  useEffect(
    () => () => {
      if (pending.current) {
        const layout = pending.current;
        pending.current = null;
        void saveLayout(layout).catch(() => undefined);
      }
      clearTimeout(timer.current);
    },
    [],
  );

  const update = useCallback(
    (layout: DashboardLayout) => {
      setLoaded({ status: "ready", layout });
      pending.current = layout;
      clearTimeout(timer.current);
      timer.current = setTimeout(() => void flush(), saveDelayMs);
    },
    [flush, saveDelayMs],
  );

  const reset = useCallback(() => update(defaultLayout(registry)), [update, registry]);

  return { loaded, saveState, update, reset };
}
