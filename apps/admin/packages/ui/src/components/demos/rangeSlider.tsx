import { useState } from "react"
import { useTranslations } from "use-intl"

import { Input } from "../input"
import { Label } from "../label"
import { Slider } from "../slider"
import { DemoSection } from "./_section"

export function RangeSliderDemo() {
  const t = useTranslations("showcase.demos.rangeSlider")
  const [value, setValue] = useState(50)
  const [range, setRange] = useState<[number, number]>([20, 80])

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <div className="grid w-full max-w-sm gap-3">
          <Label htmlFor="demo-volume">
            {t("volume")}: {value}%
          </Label>
          <Input
            id="demo-volume"
            type="range"
            min={0}
            max={100}
            value={value}
            onChange={(e) => setValue(Number(e.target.value))}
          />
        </div>
      </DemoSection>

      <DemoSection titleKey="dual">
        <div className="grid w-full max-w-sm gap-3">
          <Label>
            {t("priceRange")}: ${range[0]} - ${range[1]}
          </Label>
          <Slider
            value={range}
            onValueChange={(next) => setRange(next as [number, number])}
            min={0}
            max={100}
            step={1}
            minStepsBetweenValues={1}
          />
        </div>
      </DemoSection>
    </div>
  )
}
