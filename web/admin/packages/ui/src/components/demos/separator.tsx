import { useTranslations } from "use-intl"

import { Separator } from "../separator"
import { DemoSection } from "./_section"

export function SeparatorDemo() {
  const t = useTranslations("showcase.demos.separator")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="horizontal">
        <div className="space-y-2">
          <div className="text-sm font-medium">{t("sectionA")}</div>
          <Separator />
          <div className="text-sm font-medium">{t("sectionB")}</div>
        </div>
      </DemoSection>

      <DemoSection titleKey="vertical">
        <div className="flex h-5 items-center space-x-4 text-sm">
          <span>{t("home")}</span>
          <Separator orientation="vertical" />
          <span>{t("settings")}</span>
          <Separator orientation="vertical" />
          <span>{t("help")}</span>
        </div>
      </DemoSection>
    </div>
  )
}
