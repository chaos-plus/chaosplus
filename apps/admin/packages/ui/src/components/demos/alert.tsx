import { useTranslations } from "use-intl"
import { AlertCircle, CheckCircle2, Info, TriangleAlert } from "lucide-react"

import { Alert, AlertTitle, AlertDescription } from "../alert"
import { DemoSection } from "./_section"

export function AlertDemo() {
  const t = useTranslations("showcase.demos.alert")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="variants">
        <div className="flex flex-col gap-3">
          <Alert>
            <Info className="h-4 w-4" />
            <AlertDescription>{t("defaultMsg")}</AlertDescription>
          </Alert>
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertDescription>{t("destructiveMsg")}</AlertDescription>
          </Alert>
          <Alert variant="success">
            <CheckCircle2 className="h-4 w-4" />
            <AlertDescription>{t("successMsg")}</AlertDescription>
          </Alert>
          <Alert variant="warning">
            <TriangleAlert className="h-4 w-4" />
            <AlertDescription>{t("warningMsg")}</AlertDescription>
          </Alert>
        </div>
      </DemoSection>

      <DemoSection titleKey="withLabel">
        <div className="flex flex-col gap-3">
          <Alert>
            <Info className="h-4 w-4" />
            <AlertTitle>{t("infoTitle")}</AlertTitle>
            <AlertDescription>{t("infoDesc")}</AlertDescription>
          </Alert>
          <Alert variant="destructive">
            <AlertCircle className="h-4 w-4" />
            <AlertTitle>{t("errorTitle")}</AlertTitle>
            <AlertDescription>{t("errorDesc")}</AlertDescription>
          </Alert>
          <Alert variant="success">
            <CheckCircle2 className="h-4 w-4" />
            <AlertTitle>{t("successTitle")}</AlertTitle>
            <AlertDescription>{t("successDesc")}</AlertDescription>
          </Alert>
          <Alert variant="warning">
            <TriangleAlert className="h-4 w-4" />
            <AlertTitle>{t("warningTitle")}</AlertTitle>
            <AlertDescription>{t("warningDesc")}</AlertDescription>
          </Alert>
        </div>
      </DemoSection>
    </div>
  )
}
