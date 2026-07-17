"use client";

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { supportedLngs, type AppLocale } from "./config";

export function useLanguage() {
  const { i18n } = useTranslation();
  const [locale, setLocale] = useState<AppLocale>((i18n.language || "zh-CN") as AppLocale);

  useEffect(() => {
    if (i18n.language) {
      document.documentElement.lang = i18n.language;
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setLocale(i18n.language as AppLocale);
    }
  }, [i18n.language]);

  const changeLanguage = async (lng: AppLocale) => {
    await i18n.changeLanguage(lng);
    setLocale(lng);
  };

  return {
    locale,
    changeLanguage,
    supported: supportedLngs,
  };
}
