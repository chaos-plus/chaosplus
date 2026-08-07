import { useState } from "react"
import { useTranslations } from "use-intl"

import { Label } from "../label"
import { Textarea } from "../textarea"
import { DemoSection } from "./_section"

export function TextareaDemo() {
  const t = useTranslations("showcase.demos.textarea")
  const [value, setValue] = useState("")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label htmlFor="demo-message">{t("message")}</Label>
          <Textarea id="demo-message" placeholder={t("messagePlaceholder")} />
        </div>
      </DemoSection>

      <DemoSection titleKey="withCount">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label htmlFor="demo-bio">{t("bio")}</Label>
          <Textarea
            id="demo-bio"
            value={value}
            onChange={(e) => setValue(e.target.value)}
            placeholder={t("bioPlaceholder")}
            maxLength={160}
          />
          <div className="text-right text-xs text-muted-foreground">
            {value.length}/160
          </div>
        </div>
      </DemoSection>

      <DemoSection titleKey="disabled">
        <Textarea placeholder={t("disabled")} disabled className="max-w-sm" />
      </DemoSection>
    </div>
  )
}
