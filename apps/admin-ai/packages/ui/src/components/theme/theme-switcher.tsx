import { memo, useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useTheme } from "next-themes"
import { useTranslations } from "use-intl"
import { Check, Monitor, Moon, Palette, Sun } from "lucide-react"
import { useColorTheme } from "@workspace/ui/hooks/use-color-theme"
import { cn } from "@workspace/ui/lib/utils"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog"
import { CustomThemePanel } from "@workspace/ui/components/theme/custom-theme-panel"

interface ThemePreview {
  bg: string
  primary: string
  radius: string
}

const previewMap: Record<string, ThemePreview> = {
  modern: { bg: "#fafafa", primary: "#171717", radius: "0.875rem" },
  business: { bg: "#fafaf9", primary: "#6366f1", radius: "0.75rem" },
  eyeCare: { bg: "#f5f0e6", primary: "#5a7c5a", radius: "0.5rem" },
  pinkKawaii: { bg: "#fff0f6", primary: "#e8388a", radius: "1.5rem" },
  neoBrutalism: { bg: "#fef08a", primary: "#ff006e", radius: "0px" },
  custom: { bg: "#fafafa", primary: "#6366f1", radius: "0.875rem" },
}

// Stable onClick via data-id — so React.memo comparison never fails on the
// function reference and items truly skip re-render when nothing changed
const ThemeItem = memo(function ThemeItem({
  id,
  name,
  isActive,
  onSelect,
}: {
  id: string
  name: string
  isActive: boolean
  onSelect: (id: string) => void
}) {
  const p = previewMap[id]
  return (
    <button
      type="button"
      aria-pressed={isActive ? "true" : "false"}
      onClick={() => onSelect(id)}
      className={cn(
        "flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
        isActive
          ? "bg-primary font-medium text-primary-foreground"
          : "text-popover-foreground hover:bg-accent hover:text-accent-foreground"
      )}
    >
      <span
        className="flex size-6 shrink-0 items-center justify-center border"
        style={{
          background: p?.bg,
          borderColor: isActive ? "rgba(255,255,255,0.3)" : "rgba(0,0,0,0.1)",
          borderRadius: p?.radius === "0px" ? "0" : "5px",
        }}
      >
        <span
          className="block size-2.5"
          style={{
            background: p?.primary,
            borderRadius: p?.radius === "0px" ? "0" : p?.radius,
          }}
        />
      </span>
      <span className="flex-1 truncate text-left">{name}</span>
      {id === "custom" ? (
        <Palette className="size-4 shrink-0 opacity-70" />
      ) : isActive ? (
        <Check className="size-4 shrink-0" />
      ) : null}
    </button>
  )
})

const MODE_ICONS = { system: Monitor, light: Sun, dark: Moon } as const

export interface ThemeSwitcherProps {
  className?: string
}

export function ThemeSwitcher({ className }: ThemeSwitcherProps) {
  const t = useTranslations("themes")
  const { theme: modeValue, setTheme: setMode } = useTheme()
  const { themes, colorTheme, setColorTheme } = useColorTheme()
  const [open, setOpen] = useState(false)
  const [panelOpen, setPanelOpen] = useState(false)
  const [mounted, setMounted] = useState(false)
  const ref = useRef<HTMLDivElement>(null)
  const triggerRef = useRef<HTMLButtonElement>(null)
  const panelRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect -- mounting gate required to avoid SSR/client mismatch for theme mode buttons
    setMounted(true)
  }, [])

  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("mousedown", handleClick)
    return () => document.removeEventListener("mousedown", handleClick)
  }, [open])

  useEffect(() => {
    if (!open) return
    const handle = window.requestAnimationFrame(() => {
      panelRef.current
        ?.querySelector<HTMLButtonElement>("button:not([disabled])")
        ?.focus()
    })

    return () => window.cancelAnimationFrame(handle)
  }, [open])

  const currentMode =
    modeValue === "system" || !modeValue ? "system" : modeValue
  const validColorTheme = themes.some((th) => th.id === colorTheme)
    ? colorTheme
    : "modern"

  // Translated once per locale/theme set.
  const themeNames = useMemo(
    () => Object.fromEntries(themes.map((th) => [th.id, t(th.nameKey)])),
    [themes, t]
  )

  const closeAndRestoreFocus = useCallback(() => {
    setOpen(false)
    window.requestAnimationFrame(() => triggerRef.current?.focus())
  }, [])

  // Stable callbacks keep ThemeItem memo comparisons effective.
  const handleSelect = useCallback(
    (id: string) => {
      setColorTheme(id)
      // Selecting "Custom" IS the configure action: open the palette editor every time
      // (so clicking it again re-opens the editor).
      if (id === "custom") {
        setOpen(false)
        setPanelOpen(true)
      } else {
        closeAndRestoreFocus()
      }
    },
    [closeAndRestoreFocus, setColorTheme]
  )

  const handleMode = useCallback(
    (value: "system" | "light" | "dark") => {
      setMode(value)
      closeAndRestoreFocus()
    },
    [closeAndRestoreFocus, setMode]
  )

  return (
    <div ref={ref} className={cn("relative", className)}>
      <button
        ref={triggerRef}
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex h-8 items-center gap-1.5 rounded-md px-2 text-sm text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none"
        aria-label={t("label")}
        aria-expanded={open}
      >
        <Palette className="size-4 shrink-0" />
        <span className="hidden max-w-[90px] truncate text-xs sm:inline">
          {themeNames[validColorTheme] ?? validColorTheme}
        </span>
      </button>

      {/*
        Always in the DOM — the 10 ThemeItem components are mounted once.
        Toggling `hidden` is pure CSS (display:none), avoiding the mount/unmount
        cost that previously blocked the main thread on every open/close.
      */}
      <div
        ref={panelRef}
        hidden={!open}
        role="group"
        aria-label={t("label")}
        onKeyDown={(event) => {
          if (event.key === "Escape") {
            event.stopPropagation()
            closeAndRestoreFocus()
          }
        }}
        className="absolute top-full right-0 z-50 mt-2 w-64 rounded-md border border-border bg-popover p-1 shadow-md"
      >
        <div className="mb-1 px-2 py-1 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
          {t("appearance")}
        </div>
        <div
          className="mb-2 flex gap-1"
          role="group"
          aria-label={t("appearance")}
        >
          {(["system", "light", "dark"] as const).map((value) => {
            const Icon = MODE_ICONS[value]
            return (
              <button
                key={value}
                type="button"
                aria-pressed={
                  mounted && currentMode === value ? "true" : "false"
                }
                onClick={() => handleMode(value)}
                className={cn(
                  "flex flex-1 items-center justify-center gap-1.5 rounded-lg px-2 py-1.5 text-xs transition-colors",
                  mounted && currentMode === value
                    ? "bg-primary text-primary-foreground"
                    : "text-popover-foreground hover:bg-accent hover:text-accent-foreground"
                )}
              >
                <Icon className="size-3.5 shrink-0" />
                <span>{t(`mode.${value}`)}</span>
              </button>
            )
          })}
        </div>

        <div className="my-1 h-px bg-border" />

        <div className="mb-1 px-2 py-1 text-xs font-semibold tracking-wider text-muted-foreground uppercase">
          {t("label")}
        </div>
        <div role="group" aria-label={t("label")}>
          {themes.map((themeItem) => (
            <ThemeItem
              key={themeItem.id}
              id={themeItem.id}
              name={themeNames[themeItem.id] ?? themeItem.id}
              isActive={colorTheme === themeItem.id}
              onSelect={handleSelect}
            />
          ))}
        </div>
      </div>

      <Dialog open={panelOpen} onOpenChange={setPanelOpen}>
        <DialogContent className="max-h-[90vh] max-w-2xl overflow-auto">
          <DialogHeader>
            <DialogTitle>{t("custom")}</DialogTitle>
            <DialogDescription>{t("customDescription")}</DialogDescription>
          </DialogHeader>
          <CustomThemePanel />
        </DialogContent>
      </Dialog>
    </div>
  )
}
