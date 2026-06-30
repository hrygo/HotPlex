# WebChat 中英双语切换规格（i18n）

**状态**: proposed · **日期**: 2026-06-30 · **关联 issue**: #818 · **关联**: [WebChat-v2-Revamp-Spec.md](./WebChat-v2-Revamp-Spec.md)、[WebChat-Multitenancy-Foundation-Design-Spec.md](./WebChat-Multitenancy-Foundation-Design-Spec.md) · **版本目标**: v1.31.x

---

## 1. 背景与动机

WebChat 当前存在三个相互矛盾的事实：

1. **`<html lang="zh-CN">` 硬编码**（`webchat/app/layout.tsx:108`）——声明中文界面
2. **95% UI 文本是英文** —— onboarding、settings、admin dashboard、chat UI、tool components、error boundaries、slash commands 全部硬编码英文
3. **`app/login/page.tsx` 与 `app/page.tsx` 含 ~38 条中文字符串** —— `mapAuthError` 错误码、邀请码提示、onboarding 欢迎卡片、"开始使用"按钮等

结果：屏幕阅读器读错语言、SEO 与浏览器翻译提示失灵、新用户首次访问看到中英混编的突兀体验（login 页中文报错 + 主界面英文按钮）。

代码层已有伏笔——`webchat/lib/api/sessions.ts:171` 注释明示：

> *"Labels are intentionally English. Do not translate to Chinese... If i18n is needed, use a proper i18n framework rather than hardcoded translations."*

本 spec 落地该注释指向的"proper i18n framework"，实现**运行时中英双语切换**，同时修复 `lang` 属性与实际语言不符的缺陷。

## 2. 范围（已与 stakeholder 确认）

| 决策点 | 选择 | 理由 |
|---|---|---|
| **默认 locale** | `en` | 现状 95% UI 英文，`en` 默认与主体一致，避免一次性大翻译回归风险；中文用户首访通过 `navigator.language` 自动切到 `zh-CN` |
| **运行时切换** | 客户端运行时（单 bundle） | `output: "export"` 静态导出锁定；服务端无渲染逻辑，运行时切换是唯一可行方案 |
| **Admin 是否纳入一期** | **纳入** | 一致性优先；admin 字符串约占总量的 50%，若拆期会出现"chat 已双语、admin 仍纯英文"的割裂体验 |
| **持久化层级** | localStorage（一期） | 避免后端 DB 迁移阻塞前端交付；跨设备同步留待 Phase 5 |
| **命名空间策略** | 按功能域分文件 | 支持 lazy load 与团队并行翻译；避免单一大 JSON 难以维护 |
| **翻译来源** | 人工翻译 + 同行 review | 机翻易出"开发者不识其码"的尴尬文案；~320 键规模可控 |
| **`<html lang>` 修复** | 一并修复 | i18n 落地天然修复，无需单独改动 |

**明确不做（一期）**：

- ❌ **后端 locale 字段**（`User.Locale` / `Workspace.Locale` / `SessionInfo.Locale`）—— 不引入 DB schema 变更，跨设备同步留待 Phase 5
- ❌ **服务端 `Accept-Language` 协商** —— Go 侧 `internal/webchat/server.go` 是纯文件服务器，运行时无渲染逻辑；服务端协商收益 ≈ 0
- ❌ **平台适配器 locale 捕获**（Slack `user.locale` / Feishu `sender.locale`）—— 与 WebChat i18n 解耦，独立 spec 推进
- ❌ **ICU MessageFormat** —— ~320 键中 < 5% 需要复数/性别；普通变量插值足够；如未来需要可挂 `i18next-icu` 插件，无需重写
- ❌ **第三种语言**（日/韩等）—— 当前需求仅中英；架构预留扩展点，但翻译资源不准备
- ❌ **URL 路由前缀**（`/en/...`、`/zh-CN/...`）—— 与静态导出 + 单 bundle 方案冲突；用 query param `?lang=zh-CN` 仅用于深链分享（可选）

## 3. 现状评估

### 3.1 技术栈关键事实

| 维度 | 现状 | 对 i18n 的影响 |
|---|---|---|
| Next.js 16.2.6（App Router） | `output: "export"` 静态导出 | ❌ 不能用 SSR 路由级 i18n（`accept-language` 协商） |
| 几乎 100% `"use client"` | 仅 `app/layout.tsx` 是 server component | ✅ React Context 方案可触达所有组件 |
| Tailwind v4 + 无组件库 | 纯手写组件，无 shadcn/ui 抽象 | ⚠️ 文本硬编码在 JSX，无拦截层，需逐文件提取 |
| TypeScript strict 6.0.3 | 路径别名 `@/*` | ✅ 类型安全翻译键（typed `t("chat.send")`） |
| go:embed 静态服务 | `internal/webchat/server.go` 纯文件服务器 | ❌ Go 侧无 locale 协商 |
| 数据模型 | `User`/`Workspace`/`Session` 均无 locale 字段 | ⚠️ 用户级偏好需后端改动（留待 Phase 5） |

### 3.2 字符串分布（工作量基线）

- **43 个文件**含硬编码 UI 文本
- **~370–420 个**字符串实例，**~280–320 个**去重翻译键
- **95% 英文 / 5% 中文**（中文集中在 `app/login/page.tsx` 30 条 + `app/page.tsx` 8 条）
- **~60 处模板字面量**需变量插值（确认对话框、toast 通知占主体）

**类别分布**：

| 类别 | 计数 | 典型示例 |
|---|---|---|
| 按钮标签 | ~60 | `Sign In`、`Cancel`、`Delete`、`Save`、`Connect` |
| Toast 通知 | ~40 | `Cron job "{{name}}" successfully deleted.` |
| 表单标签 | ~25 | `Username`、`Workspace Name`、`Admin Token` |
| 占位符 | ~30 | `Search history...`、`Type a message...` |
| 工具提示 | ~30 | `Collapse sidebar`、`Copy User ID` |
| 状态标签 | ~25 | `Connected`、`Active`、`PREPARING`、`ERROR` |
| 确认对话框 | ~20 | `Are you sure you want to terminate session "{{id}}"?` |
| 错误信息 | ~20 | `Something went wrong`、`Connection failed` |
| 加载/空状态 | ~15 | `Loading...`、`No workspaces` |
| 导航/标题 | ~15 | `Dashboard`、`Session Inspector` |

**密度 Top 5（占总工作量 ~35%）**：

| 排名 | 文件 | 字符串数 | 优先级 |
|---|---|---|---|
| 1 | `app/login/page.tsx` | ~40 | 🔴 中英混编，必须最先 |
| 2 | `app/admin/sessions/page.tsx` | ~30 | 🔴 CRUD 确认/toast 密集 |
| 3 | `app/admin/page.tsx` | ~30 | 🔴 仪表盘状态/指标 |
| 4 | `app/admin/cron/detail/page.tsx` | ~25 | 🔴 表单 + CRUD |
| 5 | `app/admin/cron/page.tsx` | ~20 | 🟡 |

### 3.3 现有 i18n 基础设施

**无**。`package.json` 无 `next-intl` / `react-i18next` / `i18next` / `lingui` / `formatjs` 任一依赖；无 `messages/` / `locales/` / `i18n/` / `translations/` 目录；`next.config.mjs` 无 `i18n` 配置块。

## 4. 技术选型

### 4.1 候选对比

| 库 | 包大小（gzipped） | 静态导出适配 | 类型安全 | 运行时切换 | 成熟度 | 结论 |
|---|---|---|---|---|---|---|
| **i18next + react-i18next** | ~33 KB | ✅ 完美 | ⚠️ 需配 `i18next-typescript` | ✅ `changeLanguage()` | ⭐⭐⭐⭐⭐ | **🥇 选定** |
| next-intl | ~15 KB | ⚠️ 偏向 SSR/per-locale static；client-only 模式可工作但非主战场 | ✅ 内置 | ✅ | ⭐⭐⭐⭐ | 🥈 备选 |
| typesafe-i18n | ~1 KB | ✅ | ✅ 编译期生成 | ✅ | ⭐⭐⭐ | 极轻量场景，工具链不够成熟 |
| lingui | ~5 KB | ✅ | ✅ | ⚠️ 编译期提取 | ⭐⭐⭐⭐ | 配置复杂，extractor 不稳定 |
| FormatJS / react-intl | ~30 KB | ✅ | ⚠️ | ✅ | ⭐⭐⭐⭐⭐ | ICU 过度，< 5% 键需要复数 |

### 4.2 选定：`i18next` + `react-i18next` + `i18next-browser-languagedetector`

**理由**：

1. **静态导出零摩擦**：locale JSON 直接 `import` 进 bundle，运行时切换无需 fetch，无需多份构建产物
2. **成熟度最高**：文档/示例/工具链最齐，社区生态稳定，未来招人无学习成本
3. **namespaces 支持**：天然支持按功能域分文件（`chat.json` / `admin.json`），可按路由懒加载
4. **运行时切换**：`i18n.changeLanguage('en')` 一行 API，组件自动 re-render
5. **ICU 可扩展**：如未来真需要复数/性别，挂 `i18next-icu` 插件即可，无需重写
6. **类型安全可达**：通过 `i18next-typescript` 或自定义 type augmentation 实现 `t("chat.send")` 的键自动补全与拼写检查

### 4.3 备选触发条件

若实施过程中发现 bundle size 不可接受（> 50 KB gzipped 增量），切换到 `typesafe-i18n`。决策点：Phase 1 完成后测 bundle 报告。

## 5. 架构设计

### 5.1 目录结构

```
webchat/
├── locales/                          # 新增
│   ├── en/
│   │   ├── common.json               # 按钮/通用（Cancel/Confirm/Copy/Loading/Error）
│   │   ├── auth.json                 # login 页面 + onboarding
│   │   ├── chat.json                 # thread/composer/session panel/welcome screen
│   │   ├── admin.json                # dashboard/cron/sessions/bots/workspaces/api-keys/settings
│   │   └── errors.json               # 错误码映射 + 错误兜底页（error.tsx / global-error.tsx）
│   └── zh-CN/
│       └── (镜像 en/ 结构)
├── lib/
│   └── i18n/                         # 新增
│       ├── config.ts                 # i18next 初始化（resources + detection + fallback）
│       ├── client.tsx                # I18nextProvider wrapper（client 组件）
│       ├── types.ts                  # 类型增强：从 en/ 推导 TypedT
│       └── use-language.ts           # hook: 当前 locale + changeLanguage 包装
├── components/
│   └── LanguageSwitcher.tsx          # 新增：复用于 chat header + admin nav
└── app/
    └── layout.tsx                    # 修改：包裹 <I18nProvider>，动态 lang 属性
```

### 5.2 Locale 检测优先级

```
1. localStorage["hotplex.locale"]      ← 用户显式切换后持久化（最高优先级）
2. navigator.language                   ← 浏览器偏好（首访检测，含 fallback 链 "zh-CN" → "zh"）
3. "en" (default)                       ← 兜底（不是 zh-CN，匹配现状主体）
```

实现：`i18next-browser-languagedetector` 插件，配置：

```typescript
// lib/i18n/config.ts
import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import LanguageDetector from "i18next-browser-languagedetector";

import enCommon from "../../locales/en/common.json";
import enAuth from "../../locales/en/auth.json";
// ... 其余 namespace

export const defaultNS = "common";
export const supportedLngs = ["en", "zh-CN"] as const;
export type AppLocale = (typeof supportedLngs)[number];

export const resources = {
  en: { common: enCommon, auth: enAuth, /* ... */ },
  "zh-CN": { /* 镜像 */ },
} as const;

void i18n
  .use(initReactI18next)
  .use(LanguageDetector)
  .init({
    resources,
    fallbackLng: "en",
    supportedLngs: [...supportedLngs],
    defaultNS,
    detection: {
      order: ["localStorage", "navigator"],
      lookupLocalStorage: "hotplex.locale",
      caches: ["localStorage"],
    },
    interpolation: {
      // React 已转义，关闭 i18next 默认转义避免双重处理
      escapeValue: false,
    },
    returnObjects: true, // 支持 t("status.badge.map") 返回对象
  });

export default i18n;
```

### 5.3 Provider 接入

```tsx
// lib/i18n/client.tsx
"use client";
import { I18nextProvider } from "react-i18next";
import i18n from "./config";

export function I18nProvider({ children }: { children: React.ReactNode }) {
  return <I18nextProvider i18n={i18n}>{children}</I18nextProvider>;
}
```

```tsx
// app/layout.tsx（修改：在 ThemeProvider 外层包 I18nProvider）
import { I18nProvider } from "@/lib/i18n/client";

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" suppressHydrationWarning>
      <body>
        <I18nProvider>
          <ThemeProvider>
            <NuqsAdapter>{children}</NuqsAdapter>
          </ThemeProvider>
        </I18nProvider>
      </body>
    </html>
  );
}
```

### 5.4 `<html lang>` 动态同步

由于 `app/layout.tsx` 是 server component，无法读取 `localStorage`。采用**默认值 + 客户端同步**方案：

1. server 端 `lang="en"`（默认 locale）
2. 客户端 `useEffect` 同步：

```tsx
// lib/i18n/use-language.ts
"use client";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import type { AppLocale } from "./config";

export function useLanguage() {
  const { i18n } = useTranslation();
  const [locale, setLocale] = useState<AppLocale>(i18n.language as AppLocale);

  useEffect(() => {
    document.documentElement.lang = i18n.language;
  }, [i18n.language]);

  const changeLanguage = async (lng: AppLocale) => {
    await i18n.changeLanguage(lng);
    setLocale(lng);
  };

  return { locale, changeLanguage, supported: supportedLngs };
}
```

**FOUC（首屏闪烁）防护**：见 §8.1。

### 5.5 切换器 UI

**位置**（3 处复用同一组件）：

| 位置 | 文件 | 形态 |
|---|---|---|
| 聊天头部右侧（齿轮旁） | `app/components/chat/ChatContainer.assistant-ui.tsx` | 图标按钮 `🌐` + 下拉 |
| Admin 侧边栏底部（Logout 上方） | `components/admin/admin-nav.tsx` | 同上 |
| Settings → General（可选二级） | `settings-modal/general-tab.tsx` | 单选选项 |

**组件 API**：

```tsx
// components/LanguageSwitcher.tsx
"use client";
import { useLanguage } from "@/lib/i18n/use-language";
import { Languages } from "lucide-react"; // 已有依赖

export function LanguageSwitcher({ variant = "icon" }: { variant?: "icon" | "inline" }) {
  const { locale, changeLanguage, supported } = useLanguage();
  // 渲染下拉菜单，options = supported.map(lng => ({ value: lng, label: LOCALE_LABELS[lng] }))
  // LOCALE_LABELS = { en: "English", "zh-CN": "简体中文" }
}
```

切换逻辑：调用 `changeLanguage` → `i18n.changeLanguage()` 触发组件 re-render → `localStorage` 由 detector 缓存 → `document.documentElement.lang` 由 useEffect 同步。**无需页面刷新**。

## 6. 翻译键设计

### 6.1 Namespace 划分

| Namespace | 文件 | 键数（估） | 懒加载策略 |
|---|---|---|---|
| `common` | `common.json` | ~60 | 首屏加载（按钮/通用状态） |
| `auth` | `auth.json` | ~50 | login 路由加载 |
| `chat` | `chat.json` | ~80 | chat 路由加载 |
| `admin` | `admin.json` | ~150 | admin 路由加载（最大 namespace） |
| `errors` | `errors.json` | ~30 | 始终加载（错误兜底页可能任意路由触发） |

> 一期为简化，所有 namespace 直接 `import` 进 `resources`（合计 ~370 键，bundle 增量 ~15 KB gzipped）。若未来 namespace 膨胀，再切换为 `i18next-http-backend` 按需 fetch。

### 6.2 键命名约定

**规则**：`<scope>.<element>.<variant>` —— 全小写，点分层级，语义优先于位置。

```jsonc
// locales/en/common.json
{
  "action": {
    "cancel": "Cancel",
    "confirm": "Confirm",
    "save": "Save",
    "saving": "Saving...",
    "delete": "Delete",
    "create": "Create",
    "retry": "Retry"
  },
  "status": {
    "loading": "Loading...",
    "connected": "Connected",
    "connecting": "Connecting...",
    "disconnected": "Disconnected",
    "error": "Error"
  },
  "lang": {
    "switch": "Switch language",
    "en": "English",
    "zh-CN": "简体中文"
  }
}
```

```jsonc
// locales/en/admin.json（节选）
{
  "cron": {
    "confirm": {
      "delete": "Are you sure you want to permanently delete cron job \"{{name}}\"? This action is irreversible.",
      "trigger": "Manually execute cron job \"{{name}}\" right now?",
      "toggle": {
        "enabled": "Enable cron job \"{{name}}\"?",
        "disabled": "Disable cron job \"{{name}}\"?"
      }
    },
    "toast": {
      "deleted": "Cron job \"{{name}}\" successfully deleted.",
      "updated": "Cron job \"{{name}}\" updated — takes effect for new executions."
    }
  }
}
```

### 6.3 变量插值模式

i18next 默认使用 `{{var}}` 双花括号语法（与 JSX `{{}}` 样式对象语法视觉相似但语义无关，仅在字符串字面量内生效）。

**对应原代码 → 翻译键**：

| 原代码 | 翻译键调用 |
|---|---|
| `` `操作失败(${code})，请重试或联系管理员。` `` | `t("auth:error.operation_failed", { code })` |
| `` `Are you sure you want to terminate session "${truncateId(id)}"?` `` | `t("admin:session.confirm.terminate", { id: truncateId(id) })` |
| `` `Session "${truncateId(id)}" successfully terminated.` `` | `t("admin:session.toast.terminated", { id: truncateId(id) })` |
| `` `Polling Health (x${pollingAttempts})` `` | `t("admin:dashboard.health_polling", { count: pollingAttempts })` |

**禁止**：在翻译键中嵌入复杂表达式（三元、函数调用）。所有计算必须在调用前完成，仅传最终值。

### 6.4 类型安全

通过 module augmentation 让 TypeScript 校验键名：

```typescript
// lib/i18n/types.ts
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
```

调用 `t("admni:cron.delete")`（拼写错误）将编译失败。IDE 提供 namespace + 键路径自动补全。

## 7. 实施计划

### Phase 1 — 基础设施 + 入口本地化（~6h）

**目标**：跑通端到端切换；覆盖最高密度文件。

| 步骤 | 文件 | 工作量 |
|---|---|---|
| 1.1 装库 + 初始化 | `package.json`、`lib/i18n/{config,client,types,use-language}.ts(x)` | 1.0h |
| 1.2 Provider 接入 | `app/layout.tsx` + `document.documentElement.lang` 同步 | 0.5h |
| 1.3 切换器组件 | `components/LanguageSwitcher.tsx` | 1.0h |
| 1.4 `locales/en/auth.json` + `zh-CN/auth.json` | 提取 `app/login/page.tsx`（40 strings）+ onboarding（10 strings） | 1.5h |
| 1.5 `app/login/page.tsx` 改造 | 替换硬编码 + `mapAuthError` 改为查 `t("auth:error.{{code}}")` | 1.0h |
| 1.6 `app/page.tsx` 改造 | 替换 onboarding 中英文字符串 | 0.5h |
| 1.7 聊天核心 | `components/assistant-ui/{thread,WelcomeScreen,CopyButton}.tsx` | 0.5h |

**验收**：

- 切换器在 chat header 显示，点击切换中英即时生效
- login 页面中英完全切换（包括错误码）
- onboarding 卡片中英完全切换
- `<html lang>` 反映当前 locale
- `pnpm build` 通过，bundle 报告显示新增 < 50 KB gzipped

### Phase 2 — 聊天主流程 + 设置（~5h）

| 步骤 | 文件 | 字符串数 |
|---|---|---|
| 2.1 `locales/en/chat.json` + `zh-CN/chat.json` | — | ~80 |
| 2.2 `ChatContainer.assistant-ui.tsx`（核心 shell） | ~15 |
| 2.3 `SessionPanel.tsx`、`NewSessionModal.tsx`、`NewWorkspaceForm.tsx`、`ThemeToggle.tsx` | ~22 |
| 2.4 settings-modal 全部 tab（general/ai-config/members/profile） | ~30 |
| 2.5 assistant-ui 工具组件（Terminal/FileDiff/Search/Permission/Agent/Todo/List/Compact/LoadingSkeleton） | ~15 |
| 2.6 `context/admin-ui-context.tsx`（默认 Confirm/Cancel） | ~2 |

**验收**：聊天主流程（开新会话/发送消息/查看工具结果/调整设置）中英完全切换。

### Phase 3 — Admin 后台（~5h）

| 步骤 | 文件 | 字符串数 |
|---|---|---|
| 3.1 `locales/en/admin.json` + `zh-CN/admin.json` | — | ~150 |
| 3.2 `admin-nav.tsx` + `admin-shell.tsx`（导航 + 切换器接入） | ~10 |
| 3.3 `app/admin/page.tsx`（仪表盘） | ~30 |
| 3.4 `app/admin/{sessions,cron,workspaces,bots,api-keys,settings}/*.tsx` | ~125 |
| 3.5 `app/admin/login/page.tsx` + `app/admin/sessions/detail/page.tsx` | ~18 |
| 3.6 `components/admin/*`（badge/card/editor/resource-states 等） | ~20 |

**验收**：admin 后台所有页面（列表/详情/表单/确认对话框/toast）中英完全切换。

### Phase 4 — 模板字面量归并 + 错误兜底（~2h）

| 步骤 | 内容 |
|---|---|
| 4.1 梳理 ~60 处 `${var}` 插值，归并为命名消息模式（`admin:cron.confirm.delete` 等） | 
| 4.2 `app/error.tsx` + `app/global-error.tsx` 改造（4 + 3 字符串） |
| 4.3 `locales/en/errors.json` + `zh-CN/errors.json` 完备 |

**验收**：所有模板字面量已 i18n 化；错误兜底页中英切换。

### Phase 5（不在本期）— 后端集成

见 §9。

## 8. 边界与风险

### 8.1 FOUC（首屏语言闪烁）防护

**问题**：服务端渲染 `lang="en"` + 默认英文 UI；客户端 hydration 后若 `localStorage` 是 `zh-CN`，会闪烁一帧英文。

**防护**：在 `<head>` 注入 inline 同步脚本，**早于 React hydration** 读取 localStorage 并设置 `document.documentElement.lang` + 给 `<html>` 加 `data-locale` 属性。i18next detector 也会读到同一 localStorage 值，初始化时直接用对应语言，避免组件层闪烁。

```tsx
// app/layout.tsx
<head>
  <script dangerouslySetInnerHTML={{ __html: `
    (function() {
      try {
        var lng = localStorage.getItem('hotplex.locale');
        if (lng) document.documentElement.lang = lng;
      } catch (e) {}
    })();
  `}} />
</head>
```

> 由于 `output: "export"`，整个 HTML 是静态产物，inline script 会在浏览器解析阶段立即执行，早于任何 React 代码。

### 8.2 E2E 测试适配

**问题**：`webchat/e2e/chat.spec.ts` 当前用中文字符串作为选择器（`输入消息...`、`会话`、`会话列表`、`关闭`）。切换到英文默认后，选择器失效。

**对策**：

1. 优先方案：将测试选择器从"文本匹配"改为 `data-testid`（更稳健，与语言无关）
2. 备选方案：测试 setup 中显式 `localStorage.setItem("hotplex.locale", "zh-CN")` 锁定中文
3. 翻译期间冻结 E2E 测试，待 Phase 4 完成后统一更新断言

### 8.3 漏译防护（lint）

**问题**：43 个文件分布广，人工 review 难以保证全覆盖。

**对策**：引入 ESLint 自定义规则（或 `react-no-untranslated-strings` 类社区插件），扫描 JSX 中的裸字符串字面量并警告。规则配置：

- 白名单：`className`、`key`、`aria-*`（部分）、`data-*`、`alt`（部分）
- 命中报警：所有 `>文本<`、`title="..."`、`placeholder="..."`、`aria-label="..."`

> 一期可仅在 CI 中开 warning 级别，不阻塞合并；二期升级为 error。

### 8.4 Bundle size 预算

| 项 | 估算（gzipped） |
|---|---|
| i18next core | ~25 KB |
| react-i18next | ~3 KB |
| i18next-browser-languagedetector | ~1 KB |
| locale JSON（en + zh-CN，~640 键） | ~15 KB |
| **总增量** | **~44 KB** |

预算上限：**50 KB gzipped**。超出则评估切换 `typesafe-i18n`。

### 8.5 已排除（不适用 i18n）

| 文件 | 原因 |
|---|---|
| 7 个含 `console.*` 的文件 | 日志字符串非用户可见 |
| `lib/session-select.ts`、`workspace-path.ts` 等 | 仅中文注释，无用户可见字符串 |
| `e2e/chat.spec.ts` | 测试选择器，§8.2 单独处理 |
| `lib/ai-sdk-transport/` 协议消息 | AEP 协议层，非 UI |
| Agent 生成的对话内容 | 用户输入 + LLM 输出，与 UI 语言解耦 |

## 9. 后续演进（Phase 5+，独立 spec）

下列能力**不在本 spec 范围**，留待独立 spec 推进：

| 能力 | 涉及改动 | 价值 |
|---|---|---|
| `User.Locale` 字段 + DB 迁移 | `internal/security/identity_provider.go` + SQLiteStore/pgStore | 跨设备 locale 同步 |
| Slack 适配器捕获 `user.locale` | `internal/messaging/slack/*.go` | 平台消息按用户语言回复 |
| Feishu 适配器捕获 `sender.locale` | `internal/messaging/feishu/*.go` | 同上 |
| `Workspace.Locale` 字段 | `internal/session/multitenancy_store.go` | 团队/工作区级语言偏好 |
| `internal/webchat/server.go` cookie 注入 | 读 `Accept-Language` + cookie，注入 `<script>` | 首屏零闪烁服务端方案 |
| Slack/Feishu 卡片模板 i18n | `internal/messaging/feishu/*.go`、`internal/messaging/slack/*.go` | 平台侧消息双语 |

## 10. 验收标准

### 功能验收

- [ ] **AC-1**：聊天头部 + admin 侧边栏可见语言切换器
- [ ] **AC-2**：点击切换器，UI 文本（按钮/标签/占位符/状态/toast/确认对话框）在中英之间即时切换，**无需页面刷新**
- [ ] **AC-3**：切换后，`<html lang>` 属性同步更新（可通过浏览器 DevTools 验证）
- [ ] **AC-4**：刷新页面 / 关闭重开浏览器，locale 偏好持久保留
- [ ] **AC-5**：清空 localStorage 首访，locale 根据 `navigator.language` 自动选择（中文浏览器 → `zh-CN`，英文浏览器 → `en`）
- [ ] **AC-6**：login 页面的 `mapAuthError` 17 个错误码 + onboarding 文案完全中英双语
- [ ] **AC-7**：chat 主流程（新会话/发送/工具卡片/设置）中英完全双语
- [ ] **AC-8**：admin 后台所有页面（dashboard/cron/sessions/bots/workspaces/api-keys/settings/members）中英完全双语
- [ ] **AC-9**：所有确认对话框 + toast 通知含变量插值（如 `Cron job "xxx" deleted`）中英均正确插值

### 非功能验收

- [ ] **AC-10**：`pnpm build` 通过，无 TypeScript 错误
- [ ] **AC-11**：bundle 总增量 ≤ 50 KB gzipped（webpack bundle analyzer 验证）
- [ ] **AC-12**：Lighthouse 性能分数不下降超过 3 分（基线：当前分数 ± 3）
- [ ] **AC-13**：E2E 测试通过（按 §8.2 方案适配后）
- [ ] **AC-14**：自定义 ESLint 规则在 CI 中运行，无新增未翻译字符串 warning
- [ ] **AC-15**：3 个浏览器测试（Chrome / Safari / Firefox）一致行为

### 边界验收

- [ ] **AC-16**：Agent 生成的对话内容不受 UI locale 影响（验证：切换到英文后，仍能用中文与 Agent 对话且 Agent 回复语言由 prompt 决定）
- [ ] **AC-17**：WebSocket 协议消息（AEP v1）不受 locale 影响
- [ ] **AC-18**：日期/数字格式化（如 dashboard 指标）保持原样，不引入 `Intl.NumberFormat` 改动（独立优化）

## 11. 工作量估算

| Phase | 工时 | 输出 |
|---|---|---|
| Phase 1：基础设施 + 入口 | ~6h | i18n 框架 + 切换器 + login/onboarding/chat-core 本地化 |
| Phase 2：聊天主流程 + 设置 | ~5h | chat namespace 完备 |
| Phase 3：Admin 后台 | ~5h | admin namespace 完备 |
| Phase 4：模板归并 + 错误兜底 | ~2h | 全部模板字面量 i18n 化 |
| 翻译 review + 测试 | ~3h | 人工翻译校对、E2E 适配、跨浏览器验证 |
| **总计** | **~21h** | 完整中英双语切换 |

**建议拆分为 4 个 PR**（每 Phase 一个），确保每步可独立 review、可回滚。Phase 1 是奠基 PR，必须先合并；Phase 2-4 可并行（namespace 隔离，无文件冲突）。

---

## 附录 A：参考文档

- i18next 官方文档：<https://www.i18next.com/>
- react-i18next：<https://react.i18next.com/>
- 静态导出 + i18n 模式：<https://www.i18next.com/misc/creating-own-server#fallback-vs-run-time-detection>
- 项目内 spec 索引：[docs/specs/README.md](./README.md)

## 附录 B：现有字符串清单（Top 15 文件）

详见探索报告。本 spec 落地时，建议先用脚本（如 `i18next-parser`）自动扫描一遍，生成 `locales/en/*.json` 骨架，再人工补全 zh-CN 翻译。

```bash
# 自动提取示例（在 webchat/ 下执行）
npx i18next-parser --config i18next-parser.config.js --output locales/en/$NAMESPACE.json
```

`i18next-parser` 配置：

```js
// webchat/i18next-parser.config.js
module.exports = {
  contexts: ["app", "components"],
  extensions: [".tsx", ".ts"],
  output: "locales/en/$NAMESPACE.json",
  defaultNamespace: "common",
  namespaces: ["common", "auth", "chat", "admin", "errors"],
  lexers: {
    tsx: [{ lexer: "JsxLexer", attr: "i18nKey" }],
  },
};
```
