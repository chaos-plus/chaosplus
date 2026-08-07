import { useEffect, useState } from "react"
import { useTranslations } from "use-intl"

import { Progress } from "../progress"
import { DemoSection } from "./_section"

export function ProgressDemo() {
  const t = useTranslations("showcase.demos.progress")
  const [animated, setAnimated] = useState(0)

  useEffect(() => {
    const timer = setTimeout(() => setAnimated(72), 500)
    return () => clearTimeout(timer)
  }, [])

  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <div className="w-full max-w-sm space-y-2">
          <div className="flex justify-between text-sm text-muted-foreground">
            <span>{t("uploading")}</span>
            <span>60%</span>
          </div>
          <Progress value={60} />
        </div>
      </DemoSection>

      <DemoSection titleKey="states">
        <div className="w-full max-w-sm space-y-4">
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{t("low")} — 10%</p>
            <Progress value={10} />
          </div>
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{t("mid")} — 50%</p>
            <Progress value={50} />
          </div>
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">{t("high")} — 90%</p>
            <Progress value={90} />
          </div>
          <div className="space-y-1.5">
            <p className="text-xs text-muted-foreground">
              {t("complete")} — 100%
            </p>
            <Progress value={100} />
          </div>
        </div>
      </DemoSection>

      <DemoSection titleKey="loading">
        <div className="w-full max-w-sm space-y-2">
          <div className="flex justify-between text-sm text-muted-foreground">
            <span>{t("loading")}</span>
            <span>{animated}%</span>
          </div>
          <Progress value={animated} />
        </div>
      </DemoSection>
    </div>
  )
}
