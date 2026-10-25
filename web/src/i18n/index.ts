import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./en";

const LANGUAGE_KEY = "commentloop:language";
const supported = ["en", "zh-CN"] as const;
type Language = (typeof supported)[number];

function readLanguage(): Language {
  const fromURL = new URLSearchParams(window.location.search).get("lang");
  if (fromURL === "en" || fromURL === "zh-CN") return fromURL;
  try {
    const saved = localStorage.getItem(LANGUAGE_KEY);
    if (saved === "en" || saved === "zh-CN") return saved;
  } catch {
    // localStorage may be unavailable.
  }
  return navigator.language.toLowerCase().startsWith("zh") ? "zh-CN" : "en";
}

// Chinese is the source language. Its dictionary maps each source key to
// itself so i18next never falls back to English for Chinese users.
const zhCN = Object.fromEntries(
  Object.keys(en).map((key) => [key, key.replace(/_(one|other)$/, "")]),
);

void i18n.use(initReactI18next).init({
  resources: {
    en: { translation: en },
    "zh-CN": { translation: zhCN },
  },
  lng: readLanguage(),
  fallbackLng: "en",
  supportedLngs: [...supported],
  interpolation: { escapeValue: false },
  returnNull: false,
});

i18n.on("languageChanged", (language) => {
  const next = language === "zh-CN" ? "zh-CN" : "en";
  try {
    localStorage.setItem(LANGUAGE_KEY, next);
  } catch {
    // Ignore storage restrictions.
  }
  document.documentElement.lang = next;
});

document.documentElement.lang = i18n.language;

export function toggleLanguage(): void {
  void i18n.changeLanguage(i18n.language === "zh-CN" ? "en" : "zh-CN");
}

export default i18n;
