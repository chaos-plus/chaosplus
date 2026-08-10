import { useTranslations } from "use-intl"
import { Lock, Mail } from "lucide-react"

import { Input } from "../input"
import { Label } from "../label"
import { DemoSection } from "./_section"

export function InputDemo() {
  const t = useTranslations("showcase.demos.input")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <div className="grid w-full max-w-sm items-center gap-1.5">
          <Label htmlFor="demo-email">{t("email")}</Label>
          <Input
            id="demo-email"
            type="email"
            placeholder={t("emailPlaceholder")}
          />
          <p className="text-xs text-muted-foreground">{t("emailHint")}</p>
        </div>
      </DemoSection>

      <DemoSection titleKey="withIcon">
        <div className="relative w-full max-w-sm">
          <Mail className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input className="pl-9" placeholder={t("withIconPlaceholder")} />
        </div>
      </DemoSection>

      <DemoSection titleKey="states">
        <div className="grid w-full max-w-sm gap-3">
          <Input placeholder={t("disabled")} disabled />
          <Input placeholder={t("error")} aria-invalid="true" />
          <div className="relative">
            <Lock className="absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              className="pl-9"
              type="password"
              defaultValue="password123"
            />
          </div>
        </div>
      </DemoSection>
    </div>
  )
}
