import { useTranslations } from "use-intl"
import { Bell, ChevronDown, Settings, Trash2, User } from "lucide-react"

import { buttonVariants } from "../button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "../dropdown-menu"
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

export function DropdownMenuDemo() {
  const t = useTranslations("showcase.demos.dropdownMenu")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <DropdownMenu>
          <DropdownMenuTrigger
            className={buttonVariants({ variant: "outline" })}
          >
            {t("open")}
            <ChevronDown className="ml-1.5 size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start">
            <DropdownMenuItem>
              <User className="mr-2 size-4" />
              {t("profile")}
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Settings className="mr-2 size-4" />
              {t("settings")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem>{t("logout")}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </DemoSection>

      <LocalSection title={t("withIcons")}>
        <DropdownMenu>
          <DropdownMenuTrigger
            className={buttonVariants({ variant: "outline" })}
          >
            {t("open")}
            <ChevronDown className="ml-1.5 size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-48">
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t("account")}</DropdownMenuLabel>
              <DropdownMenuItem>
                <User className="mr-2 size-4" />
                {t("profile")}
              </DropdownMenuItem>
              <DropdownMenuItem>
                <Settings className="mr-2 size-4" />
                {t("settings")}
              </DropdownMenuItem>
              <DropdownMenuItem>
                <Bell className="mr-2 size-4" />
                {t("notifications")}
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel>{t("danger")}</DropdownMenuLabel>
              <DropdownMenuItem className="text-destructive focus:text-destructive">
                <Trash2 className="mr-2 size-4" />
                {t("delete")}
              </DropdownMenuItem>
            </DropdownMenuGroup>
          </DropdownMenuContent>
        </DropdownMenu>
      </LocalSection>

      <LocalSection title={t("withSubItems")}>
        <DropdownMenu>
          <DropdownMenuTrigger
            className={buttonVariants({ variant: "outline" })}
          >
            {t("open")}
            <ChevronDown className="ml-1.5 size-4" />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="start" className="w-48">
            <DropdownMenuItem>
              <User className="mr-2 size-4" />
              {t("profile")}
            </DropdownMenuItem>
            <DropdownMenuItem>
              <Settings className="mr-2 size-4" />
              {t("settings")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem disabled>{t("billing")}</DropdownMenuItem>
            <DropdownMenuItem>{t("help")}</DropdownMenuItem>
            <DropdownMenuItem>{t("keyboard")}</DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem>{t("logout")}</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </LocalSection>

      <LocalSection title={t("contextMenu")}>
        <div className="relative rounded-lg border border-dashed p-6 text-center text-sm text-muted-foreground">
          <p className="mb-3">{t("contextMenuHint")}</p>
          <DropdownMenu>
            <DropdownMenuTrigger
              className={buttonVariants({ variant: "secondary", size: "sm" })}
            >
              {t("appearance")}
              <ChevronDown className="ml-1.5 size-4" />
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuItem>{t("cut")}</DropdownMenuItem>
              <DropdownMenuItem>{t("copy")}</DropdownMenuItem>
              <DropdownMenuItem>{t("paste")}</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </LocalSection>
    </div>
  )
}
