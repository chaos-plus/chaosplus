import { useState } from "react"
import { useTranslations } from "use-intl"

import { Input } from "../input"
import { Label } from "../label"
import { DemoSection } from "./_section"

export function ColorPickerDemo() {
  const t = useTranslations("showcase.demos.colorPicker")
  const [color, setColor] = useState("#6366f1")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="native">
        <div className="flex items-center gap-4">
          <input
            type="color"
            value={color}
            onChange={(e) => setColor(e.target.value)}
            className="size-12 cursor-pointer rounded-lg border bg-transparent p-1"
          />
          <div className="space-y-1">
            <Label>{t("selected")}</Label>
            <Input
              value={color}
              onChange={(e) => setColor(e.target.value)}
              className="w-32"
            />
          </div>
        </div>
      </DemoSection>

      <DemoSection titleKey="presets">
        <div className="flex flex-wrap gap-2">
          {[
            "#ef4444",
            "#f97316",
            "#eab308",
            "#22c55e",
            "#3b82f6",
            "#6366f1",
            "#a855f7",
            "#ec4899",
          ].map((c) => (
            <button
              key={c}
              type="button"
              onClick={() => setColor(c)}
              className="size-8 rounded-full border-2 border-white shadow-sm ring-1 ring-border transition-transform hover:scale-110"
              style={{ background: c }}
              aria-label={`${t("select")} ${c}`}
            />
          ))}
        </div>
      </DemoSection>
    </div>
  )
}
