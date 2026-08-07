import { useTranslations } from "use-intl"
import { locales, type Locale } from "@workspace/ui/i18n/config"
import { useClientLocale } from "@workspace/ui/i18n/intl-provider"
import { cn } from "@workspace/ui/lib/utils"

export function LocaleSwitcher({
  locale,
  className,
}: {
  /** @deprecated The current locale now comes from ClientIntlProvider; kept for backward compatibility with old callers */
  locale?: Locale
  className?: string
}) {
  const { locale: current, setLocale } = useClientLocale()
  const t = useTranslations("localeSwitcher")

  return (
    <select
      value={current ?? locale}
      onChange={(e) => setLocale(e.target.value as Locale)}
      className={cn(
        "h-8 rounded-md border bg-background px-2 text-xs",
        className
      )}
      aria-label={t("label")}
    >
      {locales.map((loc) => (
        <option key={loc} value={loc}>
          {t(loc)}
        </option>
      ))}
    </select>
  )
}
