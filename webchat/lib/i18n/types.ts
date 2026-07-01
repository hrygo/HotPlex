import "i18next";
import type common from "../../locales/en/common.json";
import type auth from "../../locales/en/auth.json";
import type chat from "../../locales/en/chat.json";
import type admin from "../../locales/en/admin.json";
import type errors from "../../locales/en/errors.json";

declare module "i18next" {
  interface CustomTypeOptions {
    defaultNS: "common";
    resources: {
      common: typeof common;
      auth: typeof auth;
      chat: typeof chat;
      admin: typeof admin;
      errors: typeof errors;
    };
  }
}
