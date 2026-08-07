import { useEffect, useState } from "react"
import { useTheme } from "next-themes"
import { useTranslations } from "use-intl"
import { Moon, Palette, Sun } from "lucide-react"

import { useColorTheme } from "@workspace/ui/hooks/use-color-theme"
import { Button, buttonVariants } from "@workspace/ui/components/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@workspace/ui/components/dialog"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/select"
import { CustomThemePanel } from "@workspace/ui/components/theme/custom-theme-panel"
import { cn } from "@workspace/ui/lib/utils"

export function ThemeToggle({ className }: { className?: string }) {
  const { resolvedTheme, setTheme } = useTheme()
  const { themes, colorTheme, setColorTheme, isCustom } = useColorTheme()
  const t = useTranslations("themes")
  const [panelOpen, setPanelOpen] = useState(false)
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    const handle = setTimeout(() => setMounted(true), 0)
    return () => clearTimeout(handle)
  }, [])

  const isDark = resolvedTheme === "dark"
  const themeItems = themes.map((theme) => ({
    value: theme.id,
    label: t(theme.nameKey),
  }))

  return (
    <div className={cn("flex items-center gap-2", className)}>
      <Select
        items={themeItems}
        value={colorTheme}
        onValueChange={(value: string | null) => setColorTheme(value ?? "")}
      >
        <SelectTrigger className="h-8 w-[9rem] text-xs" aria-label={t("label")}>
          <SelectValue placeholder={t("label")} />
        </SelectTrigger>
        <SelectContent>
          {themeItems.map((theme) => (
            <SelectItem key={theme.value} value={theme.value}>
              {theme.label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {isCustom && mounted && (
        <Dialog open={panelOpen} onOpenChange={setPanelOpen}>
          <DialogTrigger
            className={buttonVariants({ variant: "outline", size: "icon-sm" })}
            aria-label={t("customize")}
          >
            <Palette className="size-3.5" />
          </DialogTrigger>
          <DialogContent className="max-h-[90vh] max-w-2xl overflow-auto">
            <DialogHeader>
              <DialogTitle>{t("custom")}</DialogTitle>
              <DialogDescription>{t("customDescription")}</DialogDescription>
            </DialogHeader>
            <CustomThemePanel />
          </DialogContent>
        </Dialog>
      )}

      <Button
        variant="outline"
        size="sm"
        onClick={() => setTheme(isDark ? "light" : "dark")}
        className="text-xs"
      >
        {mounted ? (
          <>
            {isDark ? (
              <Sun className="size-3.5" />
            ) : (
              <Moon className="size-3.5" />
            )}
            <span>{isDark ? t("mode.light") : t("mode.dark")}</span>
          </>
        ) : (
          <span>{t("mode.light")}</span>
        )}
      </Button>
    </div>
  )
}
