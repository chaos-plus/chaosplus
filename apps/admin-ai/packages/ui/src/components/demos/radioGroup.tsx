import { useState } from "react"
import { useTranslations } from "use-intl"

import { Label } from "../label"
import { RadioGroup, RadioGroupItem, RadioGroupOption } from "../radio-group"
import { DemoSection } from "./_section"

export function RadioGroupDemo() {
  const t = useTranslations("showcase.demos.radioGroup")
  const [plan, setPlan] = useState("starter")
  const [direction, setDirection] = useState("north")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="single">
        <RadioGroup
          value={plan}
          onValueChange={(val) => setPlan(val as string)}
          className="gap-3"
        >
          {[
            { value: "starter", label: t("starter") },
            { value: "pro", label: t("pro") },
            { value: "enterprise", label: t("enterprise") },
          ].map(({ value, label }) => (
            <RadioGroupOption key={value} value={value} label={label} />
          ))}
        </RadioGroup>
        <p className="mt-2 text-sm text-muted-foreground">
          {t("selected")}: {plan}
        </p>
      </DemoSection>

      <DemoSection titleKey="withLabel">
        <RadioGroup
          value={direction}
          onValueChange={(val) => setDirection(val as string)}
          className="flex flex-row gap-6"
        >
          {["north", "south", "east", "west"].map((dir) => (
            <RadioGroupOption key={dir} value={dir} label={t(dir)} />
          ))}
        </RadioGroup>
      </DemoSection>

      <DemoSection titleKey="disabled">
        <RadioGroup defaultValue="option1" disabled className="gap-3">
          <div className="flex items-center space-x-2">
            <RadioGroupItem value="option1" id="demo-disabled-1" />
            <Label htmlFor="demo-disabled-1" className="text-muted-foreground">
              {t("option1")}
            </Label>
          </div>
          <div className="flex items-center space-x-2">
            <RadioGroupItem value="option2" id="demo-disabled-2" />
            <Label htmlFor="demo-disabled-2" className="text-muted-foreground">
              {t("option2")}
            </Label>
          </div>
        </RadioGroup>
      </DemoSection>
    </div>
  )
}
