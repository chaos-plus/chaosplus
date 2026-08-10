import { memo, useCallback, useEffect, useRef, useState } from "react"
import { useTranslations } from "use-intl"
import { Check, Globe } from "lucide-react"
import { locales, type Locale } from "@workspace/ui/i18n/config"
import { useClientLocale } from "@workspace/ui/i18n/intl-provider"
import { cn } from "@workspace/ui/lib/utils"

interface LangInfo {
  code: Locale
  tag: string
  nativeName: string
  englishName: string
}

const languageMap: Record<Locale, { nativeName: string; englishName: string }> =
  {
    "en-US": { nativeName: "English (US)", englishName: "English (US)" },
    "zh-CN": { nativeName: "简体中文", englishName: "Simplified Chinese" },
    "ms-MY": { nativeName: "Bahasa Melayu", englishName: "Malay" },
  }

function localeTag(code: string) {
  return code.split("-")[0]!.toUpperCase()
}

const languages: LangInfo[] = locales.map((code) => ({
  code,
  tag: localeTag(code),
  ...languageMap[code],
}))

const LangItem = memo(function LangItem({
  lang,
  isActive,
  onClick,
}: {
  lang: LangInfo
  isActive: boolean
  onClick: () => void
}) {
  return (
    <button
      aria-pressed={isActive ? "true" : "false"}
      onClick={onClick}
      className={cn(
        "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-sm transition-colors",
        isActive
          ? "bg-primary font-medium text-primary-foreground"
          : "text-popover-foreground hover:bg-accent hover:text-accent-foreground"
      )}
    >
      <span
        className={cn(
          "inline-flex w-7 shrink-0 items-center justify-center rounded text-[10px] leading-5 font-semibold",
          isActive
            ? "bg-primary-foreground/20 text-primary-foreground"
            : "bg-muted text-muted-foreground"
        )}
      >
        {lang.tag}
      </span>
      <span className="min-w-0 flex-1 truncate text-left">
        {lang.nativeName}
      </span>
      {isActive && <Check className="size-3.5 shrink-0" />}
    </button>
  )
})

export interface LanguageSwitcherProps {
  /** @deprecated The current locale now comes from ClientIntlProvider; kept for backward compatibility with old callers */
  locale?: Locale
  className?: string
}

export function LanguageSwitcher({
  locale: localeProp,
  className,
}: LanguageSwitcherProps) {
  const t = useTranslations("localeSwitcher")
  const { locale: current, setLocale } = useClientLocale()
  const locale = current ?? localeProp
  const [open, setOpen] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  const closeAndRestoreFocus = useCallback(() => {
    setOpen(false)
    window.requestAnimationFrame(() => triggerRef.current?.focus())
  }, [])

  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        closeAndRestoreFocus()
      }
    }
    document.addEventListener("mousedown", handleClick)
    return () => document.removeEventListener("mousedown", handleClick)
  }, [open, closeAndRestoreFocus])

  useEffect(() => {
    if (!open) return
    const handle = window.requestAnimationFrame(() => {
      panelRef.current
        ?.querySelector<HTMLButtonElement>("button:not([disabled])")
        ?.focus()
    })
    return () => window.cancelAnimationFrame(handle)
  }, [open])

  const switchLanguage = (nextLocale: Locale) => {
    closeAndRestoreFocus()
    if (nextLocale === locale) return
    setLocale(nextLocale)
  }

  const currentLang =
    languages.find((l) => l.code === locale) ?? (languages[0] as LangInfo)

  return (
    <div ref={ref} className={cn("relative", className)}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex h-8 items-center gap-1.5 rounded-md px-2 text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none"
        aria-label={t("label")}
        aria-expanded={open}
      >
        <Globe className="size-4 shrink-0" />
        <span className="hidden max-w-[100px] truncate text-xs sm:inline">
          {currentLang.nativeName}
        </span>
      </button>

      {open && (
        <div
          ref={panelRef}
          role="group"
          aria-label={t("label")}
          onKeyDown={(event) => {
            if (event.key === "Escape") {
              event.stopPropagation()
              closeAndRestoreFocus()
            }
          }}
          className="absolute top-full right-0 z-50 mt-2 flex max-h-80 w-56 flex-col rounded-md border border-border bg-popover shadow-md"
        >
          <div className="border-b border-border px-3 py-2 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
            {t("label")}
          </div>
          <div className="overflow-y-auto p-1">
            {languages.map((lang) => (
              <LangItem
                key={lang.code}
                lang={lang}
                isActive={locale === lang.code}
                onClick={() => switchLanguage(lang.code)}
              />
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
