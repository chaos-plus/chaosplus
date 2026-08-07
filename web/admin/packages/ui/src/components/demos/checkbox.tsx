import { useState } from "react"
import { useTranslations } from "use-intl"

import { Checkbox } from "../checkbox"
import { Label } from "../label"
import { DemoSection } from "./_section"

export function CheckboxDemo() {
  const t = useTranslations("showcase.demos.checkbox")
  const [items, setItems] = useState({
    news: true,
    marketing: false,
    security: true,
  })
  const values = Object.values(items)
  const allChecked = values.every(Boolean)
  const someChecked = values.some(Boolean)

  const setAll = (checked: boolean) => {
    setItems({
      news: checked,
      marketing: checked,
      security: checked,
    })
  }

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <div className="flex items-center space-x-2">
          <Checkbox id="demo-terms" />
          <Label htmlFor="demo-terms">{t("terms")}</Label>
        </div>
      </DemoSection>

      <DemoSection titleKey="group">
        <div className="space-y-3 rounded-lg border p-4">
          <div className="flex items-center space-x-2 border-b pb-3">
            <Checkbox
              id="demo-all"
              checked={allChecked}
              indeterminate={!allChecked && someChecked}
              onCheckedChange={(checked) => setAll(checked)}
            />
            <Label htmlFor="demo-all">{t("terms")}</Label>
          </div>
          {[
            { key: "news", label: t("news") },
            { key: "marketing", label: t("marketing") },
            { key: "security", label: t("security") },
          ].map(({ key, label }) => (
            <div key={key} className="flex items-center space-x-2">
              <Checkbox
                id={`demo-${key}`}
                checked={items[key as keyof typeof items]}
                onCheckedChange={(checked) =>
                  setItems((prev) => ({ ...prev, [key]: checked === true }))
                }
              />
              <Label htmlFor={`demo-${key}`}>{label}</Label>
            </div>
          ))}
        </div>
      </DemoSection>

      <DemoSection titleKey="disabled">
        <div className="flex items-center space-x-2">
          <Checkbox id="demo-disabled" disabled />
          <Label htmlFor="demo-disabled" className="text-muted-foreground">
            {t("disabledOption")}
          </Label>
        </div>
      </DemoSection>
    </div>
  )
}
