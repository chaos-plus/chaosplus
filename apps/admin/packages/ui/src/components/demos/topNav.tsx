import { useTranslations } from "use-intl"

import { TopNav } from "@workspace/ui/components/layout/top-nav"

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

export function TopNavDemo() {
  const t = useTranslations("showcase.demos.topNav")
  return (
    <div className="space-y-8">
      <LocalSection title={t("preview")}>
        <div className="overflow-hidden rounded-lg border">
          <TopNav
            className="relative top-auto z-0"
            brand="MyApp"
            items={[
              { href: "#", labelKey: "home" },
              { href: "#", labelKey: "about" },
              { href: "#", labelKey: "settings" },
            ]}
            trailing={
              <span className="rounded-full bg-primary px-3 py-1 text-xs font-medium text-primary-foreground">
                {t("trailingBadge")}
              </span>
            }
          />
        </div>
        <p className="text-sm text-muted-foreground">{t("description")}</p>
      </LocalSection>
    </div>
  )
}
