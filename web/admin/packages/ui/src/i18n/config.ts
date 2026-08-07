// Built-in locales (ADR-09): canonical codes, the single source of truth the
// backend mirrors (pkg/i18n.builtin) and the consistency test pins. Additional
// languages arrive as S3 language packs, registered at runtime — not here.
export const locales = ["en-US", "zh-CN", "ms-MY"] as const

export type Locale = (typeof locales)[number]

export const defaultLocale: Locale = "en-US"
export const rtlLocales: Locale[] = []

export function isRTL(locale: Locale): boolean {
  return rtlLocales.includes(locale)
}
