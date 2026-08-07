import { useTranslations } from "use-intl"
import { Check, CreditCard } from "lucide-react"

import { Button } from "../button"
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "../card"
import { DemoSection } from "./_section"

export function CardDemo() {
  const t = useTranslations("showcase.demos.card")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="profile">
        <Card className="max-w-sm">
          <CardHeader>
            <CardTitle>{t("upgradeTitle")}</CardTitle>
            <CardDescription>{t("upgradeDesc")}</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between text-sm">
              <span>{t("monthly")}</span>
              <span className="font-semibold">$29/mo</span>
            </div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Check className="size-4 text-primary" />
              <span>{t("unlimited")}</span>
            </div>
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Check className="size-4 text-primary" />
              <span>{t("priority")}</span>
            </div>
          </CardContent>
          <CardFooter className="flex flex-col gap-2 pr-2 pb-2 sm:flex-row">
            <Button className="w-full sm:w-auto">{t("upgrade")}</Button>
            <Button variant="outline" className="w-full sm:w-auto">
              {t("learnMore")}
            </Button>
          </CardFooter>
        </Card>
      </DemoSection>

      <DemoSection titleKey="compact">
        <Card className="max-w-sm">
          <CardContent className="flex items-center gap-4 pt-6">
            <div className="flex size-12 items-center justify-center rounded-full bg-primary/10">
              <CreditCard className="size-6 text-primary" />
            </div>
            <div className="space-y-1">
              <p className="font-medium">{t("payment")}</p>
              <p className="text-sm text-muted-foreground">{t("visa")}</p>
            </div>
          </CardContent>
        </Card>
      </DemoSection>
    </div>
  )
}
