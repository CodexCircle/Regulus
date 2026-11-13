import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { en, type MessageKey } from "./en";
import { it } from "./it";

export type { MessageKey } from "./en";

export type Lang = "en" | "it";

// Shown in the language switch, each in its own language.
export const LANGUAGES: { code: Lang; name: string }[] = [
  { code: "en", name: "English" },
  { code: "it", name: "Italiano" },
];

export const catalogs: Record<Lang, Record<MessageKey, string>> = { en, it };

const STORAGE_KEY = "regulus.language";

const isLang = (v: unknown): v is Lang => v === "en" || v === "it";

// detectLanguage returns the saved choice, else Italian for an Italian
// browser, else English. Storage can be blocked (private windows): then the
// browser language decides.
export function detectLanguage(): Lang {
  try {
    const saved = window.localStorage.getItem(STORAGE_KEY);
    if (isLang(saved)) return saved;
  } catch {
    // no storage
  }
  return typeof navigator !== "undefined" && navigator.language.toLowerCase().startsWith("it") ? "it" : "en";
}

// translate looks a key up in the language's catalog and fills {name}
// placeholders from params; an unknown placeholder is left as written.
export function translate(lang: Lang, key: MessageKey, params?: Record<string, string | number>): string {
  const text = catalogs[lang][key] ?? en[key];
  if (!params) return text;
  return text.replace(/\{(\w+)\}/g, (whole, name: string) => (name in params ? String(params[name]) : whole));
}

interface LanguageState {
  lang: Lang;
  setLang: (lang: Lang) => void;
}

const LanguageContext = createContext<LanguageState | null>(null);

// LanguageProvider holds the language of the page. Components also work
// without it (they use the detected language), which keeps small tests simple.
export function LanguageProvider({ children }: { children: ReactNode }) {
  const [lang, setLangState] = useState<Lang>(detectLanguage);
  const setLang = useCallback((next: Lang) => {
    setLangState(next);
    try {
      window.localStorage.setItem(STORAGE_KEY, next);
    } catch {
      // not saved; the choice still applies to this page
    }
  }, []);
  useEffect(() => {
    document.documentElement.lang = lang;
  }, [lang]);
  const value = useMemo(() => ({ lang, setLang }), [lang, setLang]);
  return <LanguageContext.Provider value={value}>{children}</LanguageContext.Provider>;
}

export function useLanguage(): LanguageState {
  const ctx = useContext(LanguageContext);
  return ctx ?? { lang: detectLanguage(), setLang: () => {} };
}

// useT returns the translation function of the current language.
export function useT(): (key: MessageKey, params?: Record<string, string | number>) => string {
  const { lang } = useLanguage();
  return useCallback((key, params) => translate(lang, key, params), [lang]);
}
