import { useTranslations } from "use-intl"

import { Button, buttonVariants } from "../button"
import { Input } from "../input"
import { Label } from "../label"
import { Popover, PopoverContent, PopoverTrigger } from "../popover"
import { DemoSection } from "./_section"

export function PopoverDemo() {
  const t = useTranslations("showcase.demos.popover")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="simple">
        <Popover>
          <PopoverTrigger className={buttonVariants({ variant: "outline" })}>
            {t("open")}
          </PopoverTrigger>
          <PopoverContent className="w-56">
            <div className="space-y-2">
              <h4 className="font-medium">{t("title")}</h4>
              <p className="text-sm text-muted-foreground">
                {t("description")}
              </p>
            </div>
          </PopoverContent>
        </Popover>
      </DemoSection>

      <DemoSection titleKey="withForm">
        <Popover>
          <PopoverTrigger className={buttonVariants({ variant: "default" })}>
            {t("subscribe")}
          </PopoverTrigger>
          <PopoverContent className="w-80">
            <div className="grid gap-4">
              <div className="space-y-2">
                <h4 className="font-medium">{t("subscribeTitle")}</h4>
                <p className="text-sm text-muted-foreground">
                  {t("subscribeDesc")}
                </p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="popover-email">{t("email")}</Label>
                <Input id="popover-email" placeholder="name@example.com" />
              </div>
              <Button className="w-full">{t("subscribe")}</Button>
            </div>
          </PopoverContent>
        </Popover>
      </DemoSection>
    </div>
  )
}
