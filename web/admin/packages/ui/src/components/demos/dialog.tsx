import { useTranslations } from "use-intl"

import { Button, buttonVariants } from "../button"
import { Input } from "../input"
import { Label } from "../label"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "../dialog"
import { DemoSection } from "./_section"

export function DialogDemo() {
  const t = useTranslations("showcase.demos.dialog")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="alert">
        <Dialog>
          <DialogTrigger className={buttonVariants({ variant: "outline" })}>
            {t("open")}
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("confirmTitle")}</DialogTitle>
              <DialogDescription>{t("confirmDesc")}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="outline">{t("cancel")}</Button>
              <Button>{t("continue")}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </DemoSection>

      <DemoSection titleKey="form">
        <Dialog>
          <DialogTrigger className={buttonVariants({ variant: "default" })}>
            {t("editProfile")}
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("editProfile")}</DialogTitle>
              <DialogDescription>{t("editProfileDesc")}</DialogDescription>
            </DialogHeader>
            <div className="grid gap-4 py-4">
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="name" className="text-right">
                  {t("name")}
                </Label>
                <Input id="name" defaultValue="Alice" className="col-span-3" />
              </div>
              <div className="grid grid-cols-4 items-center gap-4">
                <Label htmlFor="username" className="text-right">
                  {t("username")}
                </Label>
                <Input
                  id="username"
                  defaultValue="@alice"
                  className="col-span-3"
                />
              </div>
            </div>
            <DialogFooter>
              <Button type="submit">{t("saveChanges")}</Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </DemoSection>
    </div>
  )
}
