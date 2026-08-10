import { defaultLocale, locales, type Locale } from "@workspace/ui/i18n/config"
import { loadMessages as loadUiMessages } from "@workspace/ui/i18n/load-messages"

const LANG_KEY = "platform-lang"

/** 旧版只存了 "zh"/"en" 这类短码,这里兼容映射到完整 locale。 */
const SHORT: Record<string, Locale> = { zh: "zh-CN", en: "en-US", ms: "ms-MY" }

export function currentLocale(): Locale {
  const raw = localStorage.getItem(LANG_KEY) ?? ""
  if ((locales as readonly string[]).includes(raw)) return raw as Locale
  return SHORT[raw] ?? defaultLocale
}

export function persistLocale(locale: Locale): void {
  localStorage.setItem(LANG_KEY, locale)
}

function deepMerge(a: Record<string, unknown>, b: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = { ...a }
  for (const [k, v] of Object.entries(b)) {
    const prev = out[k]
    out[k] =
      prev && typeof prev === "object" && v && typeof v === "object" && !Array.isArray(v)
        ? deepMerge(prev as Record<string, unknown>, v as Record<string, unknown>)
        : v
  }
  return out
}

/** UI 包词条 + platform 业务词条(业务覆盖 UI)。 */
export async function loadAppMessages(locale: string): Promise<Record<string, unknown>> {
  const ui = await loadUiMessages(locale)
  const app = (await import(`./messages/${locale}.json`)) as { default: Record<string, unknown> }
  return deepMerge(ui, app.default)
}
