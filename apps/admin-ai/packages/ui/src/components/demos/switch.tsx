import { useState } from "react"
import { useTranslations } from "use-intl"

import { Label } from "../label"
import { Switch } from "../switch"
import { DemoSection } from "./_section"

export function SwitchDemo() {
  const t = useTranslations("showcase.demos.switch")
  const [values, setValues] = useState({
    wifi: true,
    bluetooth: false,
    notifications: true,
  })

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <div className="flex items-center space-x-2">
          <Switch id="demo-airplane" />
          <Label htmlFor="demo-airplane">{t("airplane")}</Label>
        </div>
      </DemoSection>

      <DemoSection titleKey="settingsList">
        <div className="w-full max-w-sm space-y-4 rounded-lg border p-4">
          {[
            { key: "wifi", label: t("wifi"), desc: t("wifiDesc") },
            {
              key: "bluetooth",
              label: t("bluetooth"),
              desc: t("bluetoothDesc"),
            },
            {
              key: "notifications",
              label: t("notifications"),
              desc: t("notificationsDesc"),
            },
          ].map(({ key, label, desc }) => (
            <div key={key} className="flex items-center justify-between">
              <div className="space-y-0.5">
                <Label htmlFor={`demo-${key}`}>{label}</Label>
                <p className="text-xs text-muted-foreground">{desc}</p>
              </div>
              <Switch
                id={`demo-${key}`}
                checked={values[key as keyof typeof values]}
                onCheckedChange={(checked) =>
                  setValues((prev) => ({ ...prev, [key]: checked === true }))
                }
              />
            </div>
          ))}
        </div>
      </DemoSection>
    </div>
  )
}
