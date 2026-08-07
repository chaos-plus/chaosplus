import { useTranslations } from "use-intl"
import { Loader2, Mail, Plus, Search, Settings } from "lucide-react"

import { Button } from "../button"
import { DemoSection } from "./_section"

export function ButtonDemo() {
  const t = useTranslations("showcase.demos.button")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="variants">
        <div className="flex flex-wrap gap-3">
          <Button>{t("primary")}</Button>
          <Button variant="secondary">{t("secondary")}</Button>
          <Button variant="outline">{t("outline")}</Button>
          <Button variant="ghost">{t("ghost")}</Button>
          <Button variant="destructive">{t("destructive")}</Button>
          <Button variant="link">{t("link")}</Button>
        </div>
      </DemoSection>

      <DemoSection titleKey="sizes">
        <div className="flex flex-wrap items-center gap-3">
          <Button size="xs">{t("extraSmall")}</Button>
          <Button size="sm">{t("small")}</Button>
          <Button size="default">{t("default")}</Button>
          <Button size="lg">{t("large")}</Button>
        </div>
      </DemoSection>

      <DemoSection titleKey="states">
        <div className="flex flex-wrap gap-3">
          <Button disabled>{t("disabled")}</Button>
          <Button disabled variant="outline">
            {t("disabledOutline")}
          </Button>
          <Button>
            <Loader2 className="mr-1.5 size-4 animate-spin" />
            {t("loading")}
          </Button>
        </div>
      </DemoSection>

      <DemoSection titleKey="withIcons">
        <div className="flex flex-wrap gap-3">
          <Button>
            <Mail className="mr-1.5 size-4" />
            {t("sendEmail")}
          </Button>
          <Button variant="outline">
            <Plus className="mr-1.5 size-4" />
            {t("addUser")}
          </Button>
          <Button size="icon">
            <Search className="size-4" />
          </Button>
          <Button size="icon" variant="secondary">
            <Settings className="size-4" />
          </Button>
        </div>
      </DemoSection>
    </div>
  )
}
