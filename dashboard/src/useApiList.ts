import { useCallback, useEffect, useState } from "react";
import { ApiError } from "./api";
import { useSession } from "./session";

export type ListState<T> = { kind: "loading" } | { kind: "error"; message: string } | { kind: "ready"; items: T[] };

// useApiList loads a list once and again whenever reload is called. A 401
// answer tells the session the login has ended.
export function useApiList<T>(load: (signal: AbortSignal) => Promise<T[]>): [ListState<T>, () => void] {
  const session = useSession();
  const expired = session?.expired;
  const [state, setState] = useState<ListState<T>>({ kind: "loading" });
  const [round, setRound] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    load(controller.signal).then(
      (items) => setState({ kind: "ready", items }),
      (err: unknown) => {
        if (controller.signal.aborted) return;
        if (err instanceof ApiError && err.status === 401) {
          expired?.();
          return;
        }
        setState({ kind: "error", message: err instanceof Error ? err.message : String(err) });
      },
    );
    return () => controller.abort();
  }, [load, round, expired]);

  return [state, useCallback(() => setRound((n) => n + 1), [])];
}

export const errorMessage = (err: unknown) => (err instanceof Error ? err.message : String(err));
