import { useTranslations } from "use-intl"
import { Bell, Check, Settings } from "lucide-react"

import { Badge } from "../badge"
import { DemoSection } from "./_section"

export function BadgeDemo() {
  const t = useTranslations("showcase.demos.badge")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="variants">
        <div className="flex flex-wrap gap-2">
          <Badge>{t("default")}</Badge>
          <Badge variant="secondary">{t("secondary")}</Badge>
          <Badge variant="outline">{t("outline")}</Badge>
          <Badge variant="destructive">{t("destructive")}</Badge>
        </div>
      </DemoSection>

      <DemoSection titleKey="withIcons">
        <div className="flex flex-wrap gap-2">
          <Badge className="gap-1">
            <Check className="size-3" />
            {t("verified")}
          </Badge>
          <Badge variant="secondary" className="gap-1">
            <Bell className="size-3" />
            {t("new")}
          </Badge>
          <Badge variant="outline" className="gap-1">
            <Settings className="size-3" />
            {t("settings")}
          </Badge>
        </div>
      </DemoSection>
    </div>
  )
}
