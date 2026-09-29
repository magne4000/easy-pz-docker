import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import en from "./en.json";

// English only. Keys are the English strings themselves, so the en
// resource only needs entries where the wording differs from the key.
i18n.use(initReactI18next).init({
  lng: "en",
  fallbackLng: "en",
  resources: { en: { translation: en } },
  keySeparator: false,
  nsSeparator: false,
  interpolation: { escapeValue: false },
  returnNull: false,
});
