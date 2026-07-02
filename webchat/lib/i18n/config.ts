import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
import enChat from "../../locales/en/chat.json";
import enAdmin from "../../locales/en/admin.json";
import enErrors from "../../locales/en/errors.json";

import zhCommon from "../../locales/zh-CN/common.json";
import zhAuth from "../../locales/zh-CN/auth.json";
import zhChat from "../../locales/zh-CN/chat.json";
import zhAdmin from "../../locales/zh-CN/admin.json";
import zhErrors from "../../locales/zh-CN/errors.json";

export const defaultNS = "common";
export const supportedLngs = ["en", "zh-CN"] as const;
export type AppLocale = (typeof supportedLngs)[number];

export const resources = {
  en: {
    common: enCommon,
    auth: enAuth,
    chat: enChat,
    admin: enAdmin,
    errors: enErrors,
  },
  "zh-CN": {
    common: zhCommon,
    auth: zhAuth,
    chat: zhChat,
    admin: zhAdmin,
    errors: zhErrors,
  },
} as const;

void i18n
  .use(initReactI18next)
  .use(LanguageDetector)
  .init({
    resources,
    fallbackLng: "zh-CN",
    supportedLngs: [...supportedLngs],
    defaultNS,
    detection: {
      order: ["localStorage"],
      lookupLocalStorage: "hotplex.locale",
      caches: ["localStorage"],
    },
    interpolation: {
      escapeValue: false,
    },
    returnObjects: true,
  });

export default i18n;
