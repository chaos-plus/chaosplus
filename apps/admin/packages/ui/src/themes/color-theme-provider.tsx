import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react"
import { useTheme } from "next-themes"

import { themes, defaultThemeId, type Theme } from "./registry"

const STORAGE_KEY = "app-color-theme"
const CUSTOM_LIGHT_KEY = "app-custom-theme-light"
const CUSTOM_DARK_KEY = "app-custom-theme-dark"
const THEME_COOKIE_MAX_AGE = 60 * 60 * 24 * 365
export const CUSTOM_VARIABLE_KEYS = [
  "--background",
  "--foreground",
  "--card",
  "--card-foreground",
  "--popover",
  "--popover-foreground",
  "--primary",
  "--primary-foreground",
  "--secondary",
  "--secondary-foreground",
  "--muted",
  "--muted-foreground",
  "--accent",
  "--accent-foreground",
  "--destructive",
  "--destructive-foreground",
  "--border",
  "--input",
  "--ring",
  "--font-body",
  "--font-heading",
  "--radius",
  "--border-width",
  "--shadow-sm",
  "--shadow-md",
  "--shadow-lg",
]
const CUSTOM_VARIABLE_KEY_SET = new Set<string>(CUSTOM_VARIABLE_KEYS)
const COLOR_KEYS = new Set(
  CUSTOM_VARIABLE_KEYS.filter(
    (key) =>
      ![
        "--font-body",
        "--font-heading",
        "--radius",
        "--border-width",
        "--shadow-sm",
        "--shadow-md",
        "--shadow-lg",
      ].includes(key)
  )
)
const FONT_VALUES = new Set([
  "var(--font-sans), system-ui, sans-serif",
  'Georgia, "Times New Roman", serif',
  '"Courier New", monospace',
  "system-ui, sans-serif",
])
const SHADOW_VALUES = new Set([
  "none",
  "0 1px 2px 0 rgb(0 0 0 / 0.05)",
  "0 4px 6px -1px rgb(0 0 0 / 0.08), 0 2px 4px -2px rgb(0 0 0 / 0.04)",
  "0 10px 15px -3px rgb(0 0 0 / 0.08), 0 4px 6px -4px rgb(0 0 0 / 0.04)",
  "0 1px 2px 0 rgb(0 0 0 / 0.08)",
  "0 8px 16px -4px rgb(0 0 0 / 0.12), 0 4px 8px -4px rgb(0 0 0 / 0.06)",
  "0 20px 25px -5px rgb(0 0 0 / 0.12), 0 8px 10px -6px rgb(0 0 0 / 0.06)",
  "3px 3px 0 0 rgb(0 0 0 / 1)",
  "5px 5px 0 0 rgb(0 0 0 / 1)",
  "8px 8px 0 0 rgb(0 0 0 / 1)",
  "0 1px 2px 0 rgb(99 102 241 / 0.2)",
  "0 4px 18px rgb(99 102 241 / 0.35)",
  "0 10px 25px -5px rgb(99 102 241 / 0.4)",
])

export function isValidCustomThemeVariable(key: string, value: string) {
  if (!CUSTOM_VARIABLE_KEY_SET.has(key)) return false
  if (COLOR_KEYS.has(key)) {
    return (
      /^#[0-9A-Fa-f]{6}$/.test(value) ||
      /^rgb\(\d{1,3} \d{1,3} \d{1,3}\)$/.test(value) ||
      /^oklch\([0-9.]+ [0-9.]+ [0-9.]+\)$/.test(value)
    )
  }
  if (key === "--font-body" || key === "--font-heading")
    return FONT_VALUES.has(value)
  if (key === "--radius")
    return /^(0|0\.\d{1,3}|1(\.\d{1,3})?|2(\.0{1,3})?)rem$/.test(value)
  if (key === "--border-width") return /^[0-4]px$/.test(value)
  if (key.startsWith("--shadow-")) return SHADOW_VALUES.has(value)
  return false
}

export function sanitizeCustomThemeVariables(value: unknown): ThemeVariables {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {}
  const sanitized: ThemeVariables = {}
  Object.entries(value as Record<string, unknown>).forEach(
    ([key, rawValue]) => {
      if (
        typeof rawValue === "string" &&
        isValidCustomThemeVariable(key, rawValue)
      ) {
        sanitized[key] = rawValue
      }
    }
  )
  return sanitized
}

export type ThemeVariables = Record<string, string>

interface ColorThemeContextValue {
  themes: Theme[]
  colorTheme: string
  setColorTheme: (id: string) => void
  customLight: ThemeVariables
  customDark: ThemeVariables
  setCustomVariable: (
    mode: "light" | "dark",
    key: string,
    value: string
  ) => void
  resetCustomVariables: (mode?: "light" | "dark") => void
  isCustom: boolean
}

const ColorThemeContext = createContext<ColorThemeContextValue | null>(null)

export function ColorThemeProvider({
  children,
  initialColorTheme = defaultThemeId,
}: {
  children: ReactNode
  initialColorTheme?: string
}) {
  const { resolvedTheme } = useTheme()
  const [colorTheme, setColorThemeState] = useState(initialColorTheme)
  const [customLight, setCustomLight] = useState<ThemeVariables>({})
  const [customDark, setCustomDark] = useState<ThemeVariables>({})
  const [mounted, setMounted] = useState(false)

  useEffect(() => {
    const handle = setTimeout(() => {
      setMounted(true)
      if (typeof window === "undefined") return

      const storedTheme = localStorage.getItem(STORAGE_KEY)
      const validTheme = themes.some((t) => t.id === storedTheme)
        ? storedTheme
        : defaultThemeId
      setColorThemeState(validTheme || defaultThemeId)
      try {
        const light = localStorage.getItem(CUSTOM_LIGHT_KEY)
        const dark = localStorage.getItem(CUSTOM_DARK_KEY)
        if (light)
          setCustomLight(sanitizeCustomThemeVariables(JSON.parse(light)))
        if (dark) setCustomDark(sanitizeCustomThemeVariables(JSON.parse(dark)))
      } catch {
        // ignore invalid stored theme
      }
    }, 0)
    return () => clearTimeout(handle)
  }, [])

  // Single effect — all DOM mutations in one synchronous block so the browser
  // does exactly one style recalculation (vs two with separate effects).
  useEffect(() => {
    if (!mounted || !resolvedTheme) return

    const root = document.documentElement
    const isDark = resolvedTheme === "dark"
    const custom = isDark ? customDark : customLight

    root.setAttribute("data-theme", colorTheme)
    localStorage.setItem(STORAGE_KEY, colorTheme)

    CUSTOM_VARIABLE_KEYS.forEach((key) => root.style.removeProperty(key))
    if (colorTheme === "custom") {
      Object.entries(custom).forEach(([key, value]) => {
        root.style.setProperty(key, value)
      })
    }
  }, [colorTheme, resolvedTheme, customLight, customDark, mounted])

  const setColorTheme = useCallback((id: string) => {
    if (typeof window !== "undefined") {
      localStorage.setItem(STORAGE_KEY, id)
      document.cookie = `${STORAGE_KEY}=${id}; path=/; max-age=${THEME_COOKIE_MAX_AGE}; samesite=lax`
    }
    setColorThemeState(id)
  }, [])

  const setCustomVariable = useCallback(
    (mode: "light" | "dark", key: string, value: string) => {
      if (!isValidCustomThemeVariable(key, value)) return
      const setState = mode === "light" ? setCustomLight : setCustomDark
      const storageKey = mode === "light" ? CUSTOM_LIGHT_KEY : CUSTOM_DARK_KEY
      setState((prev) => {
        const next = { ...prev, [key]: value }
        if (typeof window !== "undefined") {
          localStorage.setItem(storageKey, JSON.stringify(next))
        }
        return next
      })
    },
    []
  )

  const resetCustomVariables = useCallback((mode?: "light" | "dark") => {
    if (!mode || mode === "light") {
      setCustomLight({})
      if (typeof window !== "undefined")
        localStorage.removeItem(CUSTOM_LIGHT_KEY)
    }
    if (!mode || mode === "dark") {
      setCustomDark({})
      if (typeof window !== "undefined")
        localStorage.removeItem(CUSTOM_DARK_KEY)
    }
  }, [])

  // Stable object — only changes when actual theme state changes.
  // Prevents all useColorTheme() consumers from re-rendering when the provider
  // re-renders due to resolvedTheme updates from next-themes.
  const value = useMemo(
    () => ({
      themes,
      colorTheme,
      setColorTheme,
      customLight,
      customDark,
      setCustomVariable,
      resetCustomVariables,
      isCustom: colorTheme === "custom",
    }),
    [
      colorTheme,
      customLight,
      customDark,
      setColorTheme,
      setCustomVariable,
      resetCustomVariables,
    ]
  )

  return (
    <ColorThemeContext.Provider value={value}>
      {children}
    </ColorThemeContext.Provider>
  )
}

export function useColorTheme(): ColorThemeContextValue {
  const ctx = useContext(ColorThemeContext)
  if (!ctx) {
    throw new Error("useColorTheme must be used within ColorThemeProvider")
  }
  return ctx
}
