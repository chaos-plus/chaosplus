import { useTranslations } from "use-intl"

import { Input } from "../input"
import { Label } from "../label"

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

export function LabelDemo() {
  const t = useTranslations("showcase.demos.label")
  return (
    <div className="space-y-8">
      <LocalSection title={t("withInput")}>
        <div className="grid w-full max-w-sm items-center gap-1.5">
          <Label htmlFor="label-demo-username">{t("username")}</Label>
          <Input
            id="label-demo-username"
            placeholder={t("usernamePlaceholder")}
          />
        </div>
      </LocalSection>

      <LocalSection title={t("required")}>
        <div className="grid w-full max-w-sm items-center gap-1.5">
          <Label htmlFor="label-demo-email">
            {t("email")}
            <span className="ml-1 text-destructive">*</span>
          </Label>
          <Input
            id="label-demo-email"
            type="email"
            placeholder={t("emailPlaceholder")}
          />
        </div>
      </LocalSection>
    </div>
  )
}
