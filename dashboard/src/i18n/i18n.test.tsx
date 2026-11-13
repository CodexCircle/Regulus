import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { en } from "./en";
import { it as italian } from "./it";
import { catalogs, detectLanguage, LanguageProvider, translate, useLanguage, useT, type Lang } from "./index";

afterEach(() => {
  vi.restoreAllMocks();
  document.documentElement.removeAttribute("lang");
});

const placeholders = (text: string) => [...text.matchAll(/\{(\w+)\}/g)].map((m) => m[1]).sort();

describe("the catalogs", () => {
  it("have the same keys in English and Italian", () => {
    // The compiler already refuses a missing Italian key; this catches an
    // extra one, and a catalog edited around the types.
    expect(Object.keys(italian).sort()).toEqual(Object.keys(en).sort());
  });

  it("have no empty text", () => {
    for (const lang of ["en", "it"] as Lang[]) {
      for (const [key, text] of Object.entries(catalogs[lang])) {
        expect(text.trim(), `${lang}: ${key}`).not.toBe("");
      }
    }
  });

  it("use the same placeholders in both languages", () => {
    for (const key of Object.keys(en) as (keyof typeof en)[]) {
      expect(placeholders(italian[key]), key).toEqual(placeholders(en[key]));
    }
  });
});

describe("translate", () => {
  it("returns the text of the language", () => {
    expect(translate("en", "nav.servers")).toBe("Servers");
    expect(translate("it", "nav.servers")).toBe("Server");
    expect(translate("it", "nav.signOut")).toBe("Esci");
  });

  it("fills placeholders and leaves unknown ones as written", () => {
    // A catalog with a placeholder, to test the filling itself.
    const original = catalogs.en["app.brand"];
    (catalogs.en as Record<string, string>)["app.brand"] = "Hi {name}, {other}";
    try {
      expect(translate("en", "app.brand", { name: "Ada" })).toBe("Hi Ada, {other}");
      expect(translate("en", "app.brand", { name: 3, other: "x" })).toBe("Hi 3, x");
    } finally {
      (catalogs.en as Record<string, string>)["app.brand"] = original;
    }
  });
});

describe("detectLanguage", () => {
  it("follows the browser language", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("it-IT");
    expect(detectLanguage()).toBe("it");
    vi.spyOn(navigator, "language", "get").mockReturnValue("fr-FR");
    expect(detectLanguage()).toBe("en");
  });

  it("prefers the saved choice", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("it-IT");
    window.localStorage.setItem("regulus.language", "en");
    expect(detectLanguage()).toBe("en");
  });

  it("ignores a saved value it does not know, and blocked storage", () => {
    vi.spyOn(navigator, "language", "get").mockReturnValue("it-IT");
    window.localStorage.setItem("regulus.language", "klingon");
    expect(detectLanguage()).toBe("it");
    vi.spyOn(Storage.prototype, "getItem").mockImplementation(() => {
      throw new Error("blocked");
    });
    expect(detectLanguage()).toBe("it");
  });
});

function Probe() {
  const t = useT();
  const { lang, setLang } = useLanguage();
  return (
    <>
      <p>{t("nav.updates")}</p>
      <button type="button" onClick={() => setLang(lang === "en" ? "it" : "en")}>
        switch
      </button>
    </>
  );
}

describe("LanguageProvider", () => {
  it("switches the language, saves it and sets <html lang>", async () => {
    render(
      <LanguageProvider>
        <Probe />
      </LanguageProvider>,
    );
    expect(screen.getByText("Updates")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("en");

    await act(async () => {
      await userEvent.click(screen.getByRole("button", { name: "switch" }));
    });
    expect(screen.getByText("Aggiornamenti")).toBeInTheDocument();
    expect(document.documentElement.lang).toBe("it");
    expect(window.localStorage.getItem("regulus.language")).toBe("it");
  });

  it("works without a provider", () => {
    render(<Probe />);
    expect(screen.getByText("Updates")).toBeInTheDocument();
  });
});

describe("the areas", () => {
  it("keep their keys to themselves, so a spread cannot hide a duplicate", async () => {
    const { areas } = await import("./en");
    const seen = new Map<string, string>();
    for (const [area, messages] of Object.entries(areas)) {
      for (const key of Object.keys(messages)) {
        expect(key.startsWith(`${area === "core" ? "" : area + "."}`), `${key} in ${area}`).toBe(true);
        expect(seen.get(key), `${key} is in ${seen.get(key)} and ${area}`).toBeUndefined();
        seen.set(key, area);
      }
    }
  });
});
