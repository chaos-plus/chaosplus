import { useState } from "react"
import { useTranslations } from "use-intl"

import { Slider } from "../slider"
import { DemoSection } from "./_section"

export function SliderDemo() {
  const t = useTranslations("showcase.demos.slider")
  const [volume, setVolume] = useState(40)
  const [range, setRange] = useState<number[]>([20, 80])

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <div className="w-full max-w-sm space-y-3">
          <div className="flex justify-between text-sm">
            <span className="text-muted-foreground">{t("volume")}</span>
            <span className="font-medium">{volume}</span>
          </div>
          <Slider
            value={volume}
            onValueChange={(val) => setVolume(val as number)}
            min={0}
            max={100}
            step={1}
          />
        </div>
      </DemoSection>

      <DemoSection titleKey="group">
        <div className="w-full max-w-sm space-y-3">
          <div className="flex justify-between text-sm">
            <span className="text-muted-foreground">{t("priceRange")}</span>
            <span className="font-medium">
              ${range[0]} – ${range[1]}
            </span>
          </div>
          <Slider
            value={range}
            onValueChange={(val) => setRange(val as number[])}
            min={0}
            max={200}
            step={5}
          />
        </div>
      </DemoSection>

      <DemoSection titleKey="disabled">
        <div className="w-full max-w-sm">
          <Slider value={30} min={0} max={100} disabled />
        </div>
      </DemoSection>
    </div>
  )
}
