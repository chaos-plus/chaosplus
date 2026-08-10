import { useTranslations } from "use-intl"

import { buttonVariants } from "../button"
import { Input } from "../input"
import { Label } from "../label"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
  SheetClose,
} from "../sheet"
import { DemoSection } from "./_section"

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

export function SheetDemo() {
  const t = useTranslations("showcase.demos.sheet")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="sides">
        <div className="flex flex-wrap gap-3">
          <Sheet>
            <SheetTrigger className={buttonVariants({ variant: "outline" })}>
              {t("openRight")}
            </SheetTrigger>
            <SheetContent side="right">
              <SheetHeader>
                <SheetTitle>{t("rightTitle")}</SheetTitle>
                <SheetDescription>{t("rightDesc")}</SheetDescription>
              </SheetHeader>
              <div className="flex-1 px-6 py-4 text-sm text-muted-foreground">
                {t("rightContent")}
              </div>
            </SheetContent>
          </Sheet>

          <Sheet>
            <SheetTrigger className={buttonVariants({ variant: "outline" })}>
              {t("openLeft")}
            </SheetTrigger>
            <SheetContent side="left">
              <SheetHeader>
                <SheetTitle>{t("leftTitle")}</SheetTitle>
                <SheetDescription>{t("leftDesc")}</SheetDescription>
              </SheetHeader>
              <div className="flex-1 px-6 py-4 text-sm text-muted-foreground">
                {t("leftContent")}
              </div>
            </SheetContent>
          </Sheet>

          <Sheet>
            <SheetTrigger className={buttonVariants({ variant: "outline" })}>
              {t("openBottom")}
            </SheetTrigger>
            <SheetContent side="bottom">
              <SheetHeader>
                <SheetTitle>{t("bottomTitle")}</SheetTitle>
                <SheetDescription>{t("bottomDesc")}</SheetDescription>
              </SheetHeader>
              <div className="flex-1 px-6 py-4 text-sm text-muted-foreground">
                {t("bottomContent")}
              </div>
            </SheetContent>
          </Sheet>
        </div>
      </DemoSection>

      <LocalSection title={t("withForm")}>
        <Sheet>
          <SheetTrigger className={buttonVariants()}>
            {t("editProfile")}
          </SheetTrigger>
          <SheetContent side="right">
            <SheetHeader>
              <SheetTitle>{t("editProfile")}</SheetTitle>
              <SheetDescription>{t("editProfileDesc")}</SheetDescription>
            </SheetHeader>
            <div className="grid flex-1 gap-4 px-6 py-4">
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="sheet-name" className="text-right">
                  {t("name")}
                </Label>
                <Input
                  id="sheet-name"
                  defaultValue="Alice Chen"
                  className="col-span-3"
                />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="sheet-email" className="text-right">
                  {t("email")}
                </Label>
                <Input
                  id="sheet-email"
                  defaultValue="alice@example.com"
                  className="col-span-3"
                />
              </div>
            </div>
            <SheetFooter>
              <SheetClose className={buttonVariants({ variant: "outline" })}>
                {t("cancel")}
              </SheetClose>
              <SheetClose className={buttonVariants()}>{t("save")}</SheetClose>
            </SheetFooter>
          </SheetContent>
        </Sheet>
      </LocalSection>
    </div>
  )
}
