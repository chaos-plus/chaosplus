import { useLocale, useTranslations } from "use-intl"

import { LanguageSwitcher } from "@workspace/ui/components/i18n/language-switcher"
import type { Locale } from "@workspace/ui/i18n/config"

function LocalSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">{title}</h4>
      {children}
    </div>
  )
}

export function LanguageSwitcherDemo() {
  const t = useTranslations("showcase.demos.languageSwitcher")
  const locale = useLocale() as Locale
  return (
    <div className="space-y-8">
      <LocalSection title={t("switcher")}>
        <div className="flex flex-col gap-4">
          <LanguageSwitcher locale={locale} />
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>
      </LocalSection>
    </div>
  )
}
