import { useTranslations } from "use-intl"

import { Label } from "../label"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "../select"
import { DemoSection } from "./_section"

export function SelectDemo() {
  const t = useTranslations("showcase.demos.select")
  const roleItems = [
    { value: "admin", label: t("admin") },
    { value: "editor", label: t("editor") },
    { value: "viewer", label: t("viewer") },
    { value: "guest", label: t("guest") },
  ]
  const projectItems = [
    { value: "chaosplus", label: "Chaosplus" },
    { value: "portal", label: t("adminPortal") },
    { value: "docs", label: t("documentation") },
  ]

  return (
    <div className="space-y-8">
      <DemoSection titleKey="default">
        <Select items={roleItems}>
          <SelectTrigger className="w-[220px]">
            <SelectValue placeholder={t("placeholder")} />
          </SelectTrigger>
          <SelectContent>
            {roleItems.map((item) => (
              <SelectItem key={item.value} value={item.value}>
                {item.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </DemoSection>

      <DemoSection titleKey="withLabel">
        <div className="grid w-full max-w-sm gap-1.5">
          <Label htmlFor="demo-project">{t("project")}</Label>
          <Select items={projectItems}>
            <SelectTrigger id="demo-project">
              <SelectValue placeholder={t("projectPlaceholder")} />
            </SelectTrigger>
            <SelectContent>
              {projectItems.map((item) => (
                <SelectItem key={item.value} value={item.value}>
                  {item.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      </DemoSection>
    </div>
  )
}
