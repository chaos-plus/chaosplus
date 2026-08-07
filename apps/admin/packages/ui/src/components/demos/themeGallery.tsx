import { useTranslations } from "use-intl"
import { useTheme } from "next-themes"
import { useColorTheme } from "@workspace/ui/hooks/use-color-theme"
import { cn } from "@workspace/ui/lib/utils"
import { Button } from "../button"
import { Badge } from "../badge"
import { ThemeSwitcher } from "@workspace/ui/components/theme/theme-switcher"

const THEMES = [
  "modern",
  "business",
  "eyeCare",
  "pinkKawaii",
  "neoBrutalism",
  "custom",
] as const

const DEFAULT_PREVIEW = {
  bg: "#fafafa",
  primary: "#171717",
  text: "#171717",
  border: "#e5e5e5",
  radius: "0.875rem",
}
const themePreview: Record<string, typeof DEFAULT_PREVIEW> = {
  modern: {
    bg: "#fafafa",
    primary: "#171717",
    text: "#171717",
    border: "#e5e5e5",
    radius: "0.875rem",
  },
  business: {
    bg: "#fafaf9",
    primary: "#6366f1",
    text: "#1e1b4b",
    border: "#e0e7ff",
    radius: "0.75rem",
  },
  eyeCare: {
    bg: "#f5f0e6",
    primary: "#5a7c5a",
    text: "#2d3b2a",
    border: "#c8d9c8",
    radius: "0.5rem",
  },
  pinkKawaii: {
    bg: "#fff0f6",
    primary: "#e8388a",
    text: "#5c002e",
    border: "#ffd6e7",
    radius: "1.5rem",
  },
  neoBrutalism: {
    bg: "#fef08a",
    primary: "#ff006e",
    text: "#000000",
    border: "#000000",
    radius: "0px",
  },
  custom: {
    bg: "#fafafa",
    primary: "#6366f1",
    text: "#171717",
    border: "#e5e5e5",
    radius: "0.875rem",
  },
}

function ThemeCard({ themeId, label }: { themeId: string; label: string }) {
  const p = themePreview[themeId] ?? DEFAULT_PREVIEW
  const t = useTranslations("showcase.demos.themeGallery")
  return (
    <div
      style={{
        background: p.bg,
        borderColor: p.border,
        borderRadius: p.radius,
      }}
      className="flex flex-col gap-3 border p-4"
    >
      <div className="flex items-center justify-between">
        <span style={{ color: p.text, fontSize: "0.75rem", fontWeight: 600 }}>
          {label}
        </span>
        <span
          style={{
            background: p.primary,
            color: "#fff",
            borderRadius: p.radius,
            fontSize: "0.7rem",
            padding: "2px 8px",
          }}
        >
          {t("badge")}
        </span>
      </div>
      <div className="flex gap-2">
        <span
          style={{
            background: p.primary,
            color: "#fff",
            borderRadius: p.radius,
            fontSize: "0.75rem",
            padding: "4px 12px",
          }}
        >
          {t("button")}
        </span>
        <span
          style={{
            background: "transparent",
            color: p.primary,
            border: `1.5px solid ${p.primary}`,
            borderRadius: p.radius,
            fontSize: "0.75rem",
            padding: "4px 12px",
          }}
        >
          {t("outline")}
        </span>
      </div>
      <div
        style={{
          height: 6,
          background: p.border,
          borderRadius: 9999,
          overflow: "hidden",
        }}
      >
        <div style={{ width: "65%", height: "100%", background: p.primary }} />
      </div>
    </div>
  )
}

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

export function ThemeGalleryDemo() {
  const t = useTranslations("showcase.demos.themeGallery")
  const { colorTheme, setColorTheme } = useColorTheme()
  const { theme, resolvedTheme, setTheme } = useTheme()
  const selectedMode = theme ?? "system"
  const currentThemeLabel = THEMES.includes(
    colorTheme as (typeof THEMES)[number]
  )
    ? t(colorTheme as (typeof THEMES)[number])
    : colorTheme
  const currentModeLabel =
    resolvedTheme === "light" || resolvedTheme === "dark"
      ? t(resolvedTheme)
      : t("system")

  return (
    <div className="space-y-8">
      <LocalSection title={t("gallery")}>
        <div className="grid grid-cols-2 gap-3 sm:grid-cols-3">
          {THEMES.map((id) => (
            <button
              key={id}
              type="button"
              onClick={() => setColorTheme(id)}
              className="cursor-pointer rounded-xl text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              aria-pressed={colorTheme === id}
            >
              <div
                className={`rounded-xl ring-2 transition-all ${colorTheme === id ? "ring-primary" : "ring-transparent hover:ring-border"}`}
              >
                <ThemeCard themeId={id} label={t(id as "modern")} />
              </div>
            </button>
          ))}
        </div>
      </LocalSection>

      <LocalSection title={t("darkMode")}>
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="outline"
            onClick={() => setTheme("light")}
            aria-pressed={selectedMode === "light"}
            className={cn(
              selectedMode === "light" &&
                "border-primary bg-primary text-primary-foreground"
            )}
          >
            {t("light")}
          </Button>
          <Button
            variant="outline"
            onClick={() => setTheme("dark")}
            aria-pressed={selectedMode === "dark"}
            className={cn(
              selectedMode === "dark" &&
                "border-primary bg-primary text-primary-foreground"
            )}
          >
            {t("dark")}
          </Button>
          <Button
            variant="outline"
            onClick={() => setTheme("system")}
            aria-pressed={selectedMode === "system"}
            className={cn(
              selectedMode === "system" &&
                "border-primary bg-primary text-primary-foreground"
            )}
          >
            {t("system")}
          </Button>
          <Badge variant="secondary">
            {t("current")}: {currentThemeLabel} / {currentModeLabel}
          </Badge>
        </div>
      </LocalSection>

      <LocalSection title={t("quickSwitch")}>
        <div className="flex items-center gap-4">
          <ThemeSwitcher />
          <p className="text-sm text-muted-foreground">
            {t("quickSwitchDesc")}
          </p>
        </div>
      </LocalSection>
    </div>
  )
}
