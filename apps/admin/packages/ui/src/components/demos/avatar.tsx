import { useTranslations } from "use-intl"

import { Avatar, AvatarImage, AvatarFallback } from "../avatar"
import { DemoSection } from "./_section"

export function AvatarDemo() {
  const t = useTranslations("showcase.demos.avatar")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="sizes">
        <div className="flex flex-wrap items-end gap-4">
          <div className="flex flex-col items-center gap-1">
            <Avatar size="sm">
              <AvatarImage src="https://github.com/shadcn.png" alt="@shadcn" />
              <AvatarFallback>SC</AvatarFallback>
            </Avatar>
            <span className="text-xs text-muted-foreground">{t("small")}</span>
          </div>
          <div className="flex flex-col items-center gap-1">
            <Avatar size="default">
              <AvatarImage src="https://github.com/shadcn.png" alt="@shadcn" />
              <AvatarFallback>SC</AvatarFallback>
            </Avatar>
            <span className="text-xs text-muted-foreground">
              {t("default")}
            </span>
          </div>
          <div className="flex flex-col items-center gap-1">
            <Avatar size="lg">
              <AvatarImage src="https://github.com/shadcn.png" alt="@shadcn" />
              <AvatarFallback>SC</AvatarFallback>
            </Avatar>
            <span className="text-xs text-muted-foreground">{t("large")}</span>
          </div>
          <div className="flex flex-col items-center gap-1">
            <Avatar size="xl">
              <AvatarImage src="https://github.com/shadcn.png" alt="@shadcn" />
              <AvatarFallback>SC</AvatarFallback>
            </Avatar>
            <span className="text-xs text-muted-foreground">{t("xlarge")}</span>
          </div>
        </div>
      </DemoSection>

      <DemoSection titleKey="states">
        {/* Fallback shown when no image is provided (initials / muted bg). */}
        <div className="flex flex-wrap items-center gap-4">
          <Avatar>
            <AvatarFallback>AL</AvatarFallback>
          </Avatar>
          <Avatar>
            <AvatarFallback className="bg-primary/10 text-primary">
              BO
            </AvatarFallback>
          </Avatar>
          <Avatar>
            <AvatarFallback className="bg-secondary text-secondary-foreground">
              CR
            </AvatarFallback>
          </Avatar>
        </div>
      </DemoSection>

      <DemoSection titleKey="group">
        <div className="flex -space-x-3">
          <Avatar className="border-2 border-background">
            <AvatarImage src="https://github.com/shadcn.png" alt="User 1" />
            <AvatarFallback>U1</AvatarFallback>
          </Avatar>
          <Avatar className="border-2 border-background">
            <AvatarFallback>U2</AvatarFallback>
          </Avatar>
          <Avatar className="border-2 border-background">
            <AvatarFallback>U3</AvatarFallback>
          </Avatar>
          <Avatar className="border-2 border-background bg-muted">
            <AvatarFallback className="text-xs">+5</AvatarFallback>
          </Avatar>
        </div>
      </DemoSection>
    </div>
  )
}
