import { useTranslations } from "use-intl"
import {
  Bold,
  Italic,
  Underline,
  AlignLeft,
  AlignCenter,
  AlignRight,
} from "lucide-react"

import { Toggle } from "../toggle"
import { DemoSection } from "./_section"

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

export function ToggleDemo() {
  const t = useTranslations("showcase.demos.toggle")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="variants">
        <div className="flex flex-wrap gap-3">
          <Toggle aria-label={t("default")}>{t("default")}</Toggle>
          <Toggle variant="outline" aria-label={t("outline")}>
            {t("outline")}
          </Toggle>
          <Toggle variant="ghost" aria-label={t("ghost")}>
            {t("ghost")}
          </Toggle>
        </div>
      </DemoSection>

      <DemoSection titleKey="sizes">
        <div className="flex flex-wrap items-center gap-3">
          <Toggle size="sm" aria-label={t("small")}>
            {t("small")}
          </Toggle>
          <Toggle size="default" aria-label={t("default")}>
            {t("default")}
          </Toggle>
          <Toggle size="lg" aria-label={t("large")}>
            {t("large")}
          </Toggle>
        </div>
      </DemoSection>

      <DemoSection titleKey="states">
        <div className="flex flex-wrap gap-3">
          <Toggle defaultPressed aria-label={t("pressed")}>
            {t("pressed")}
          </Toggle>
          <Toggle aria-label={t("unpressed")}>{t("unpressed")}</Toggle>
          <Toggle disabled aria-label={t("disabled")}>
            {t("disabled")}
          </Toggle>
        </div>
      </DemoSection>

      <LocalSection title={t("withIcons")}>
        <div className="flex flex-wrap gap-2">
          <Toggle size="sm" variant="outline" aria-label="Bold">
            <Bold className="size-4" />
          </Toggle>
          <Toggle size="sm" variant="outline" aria-label="Italic">
            <Italic className="size-4" />
          </Toggle>
          <Toggle size="sm" variant="outline" aria-label="Underline">
            <Underline className="size-4" />
          </Toggle>
        </div>
        <div className="flex flex-wrap gap-2">
          <Toggle size="sm" aria-label="Align left">
            <AlignLeft className="size-4" />
          </Toggle>
          <Toggle size="sm" aria-label="Align center">
            <AlignCenter className="size-4" />
          </Toggle>
          <Toggle size="sm" aria-label="Align right">
            <AlignRight className="size-4" />
          </Toggle>
        </div>
      </LocalSection>
    </div>
  )
}
