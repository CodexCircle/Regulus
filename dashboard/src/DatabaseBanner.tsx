import { useEffect, useState } from "react";
import { fetchDatabaseStatus } from "./api";

// How often the page asks the hub, and how long "answering again" stays.
const POLL_MS = 10_000;
const RECOVERED_MS = 15_000;

export type DatabaseState = "ok" | "down" | "recovered";

// The texts are here, in both languages, until the translation mechanism of
// #125 exists; then they move there. The language comes from the browser.
const texts = {
  en: {
    down: "The database is not answering. Signing in and most pages will fail until it is back. The hub keeps running and reconnects by itself.",
    recovered: "The database is answering again.",
  },
  it: {
    down: "Il database non risponde. Accesso e gran parte delle pagine non funzioneranno finché non torna disponibile. L'hub resta in funzione e si riconnette da solo.",
    recovered: "Il database risponde di nuovo.",
  },
} as const;

const language = (): keyof typeof texts =>
  typeof navigator !== "undefined" && navigator.language.toLowerCase().startsWith("it") ? "it" : "en";

// useDatabaseState follows GET /api/database, which needs no login: while the
// database is down the hub cannot check a session, so nobody could be told
// through an endpoint that needs one. A failed request says nothing about the
// database (the page already reports an unreachable hub), so it changes nothing.
export function useDatabaseState(): DatabaseState {
  const [state, setState] = useState<DatabaseState>("ok");

  useEffect(() => {
    const controller = new AbortController();
    let recoveredTimer: ReturnType<typeof setTimeout> | undefined;

    const poll = () => {
      fetchDatabaseStatus(controller.signal).then(
        ({ ok }) => {
          if (!ok) {
            clearTimeout(recoveredTimer);
            setState("down");
            return;
          }
          setState((prev) => {
            if (prev !== "down") return prev;
            clearTimeout(recoveredTimer);
            recoveredTimer = setTimeout(() => setState("ok"), RECOVERED_MS);
            return "recovered";
          });
        },
        () => {
          // Unknown: keep what is shown.
        },
      );
    };

    poll();
    const timer = setInterval(poll, POLL_MS);
    return () => {
      controller.abort();
      clearInterval(timer);
      clearTimeout(recoveredTimer);
    };
  }, []);

  return state;
}

// DatabaseBanner tells everyone, signed in or not, when the hub's database
// stops answering and when it answers again. It shows nothing otherwise. It
// carries no detail: the reason stays in the hub's log, regulus status and the
// console.
export function DatabaseBanner() {
  const state = useDatabaseState();
  if (state === "ok") return null;
  const t = texts[language()];
  return state === "down" ? (
    <div className="banner banner-danger" role="alert">
      {t.down}
    </div>
  ) : (
    <div className="banner banner-ok" role="status">
      {t.recovered}
    </div>
  );
}
