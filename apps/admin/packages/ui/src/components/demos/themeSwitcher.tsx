import { useTranslations } from "use-intl"

import { ThemeSwitcher } from "@workspace/ui/components/theme/theme-switcher"

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

export function ThemeSwitcherDemo() {
  const t = useTranslations("showcase.demos.themeSwitcher")
  return (
    <div className="space-y-8">
      <LocalSection title={t("switcher")}>
        <div className="flex flex-col gap-4">
          <ThemeSwitcher />
          <p className="text-sm text-muted-foreground">{t("description")}</p>
        </div>
      </LocalSection>
    </div>
  )
}
