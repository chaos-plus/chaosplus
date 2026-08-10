import { useTranslations } from "use-intl"
import { Plus, Settings, User } from "lucide-react"

import { buttonVariants } from "../button"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "../tooltip"
import { DemoSection } from "./_section"

export function TooltipDemo() {
  const t = useTranslations("showcase.demos.tooltip")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="basic">
        <TooltipProvider>
          <Tooltip>
            <TooltipTrigger className={buttonVariants({ variant: "outline" })}>
              {t("hover")}
            </TooltipTrigger>
            <TooltipContent>{t("content")}</TooltipContent>
          </Tooltip>
        </TooltipProvider>
      </DemoSection>

      <DemoSection titleKey="iconButtons">
        <TooltipProvider>
          <div className="flex gap-2">
            <Tooltip>
              <TooltipTrigger
                className={buttonVariants({ size: "icon", variant: "outline" })}
              >
                <Plus className="size-4" />
              </TooltipTrigger>
              <TooltipContent>{t("add")}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                className={buttonVariants({ size: "icon", variant: "outline" })}
              >
                <Settings className="size-4" />
              </TooltipTrigger>
              <TooltipContent>{t("settings")}</TooltipContent>
            </Tooltip>
            <Tooltip>
              <TooltipTrigger
                className={buttonVariants({ size: "icon", variant: "outline" })}
              >
                <User className="size-4" />
              </TooltipTrigger>
              <TooltipContent>{t("profile")}</TooltipContent>
            </Tooltip>
          </div>
        </TooltipProvider>
      </DemoSection>
    </div>
  )
}
