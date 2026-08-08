import type { ComponentProps } from "react"
import { ThemeProvider as NextThemesProvider } from "next-themes"
import { ColorThemeProvider } from "@workspace/ui/themes/color-theme-provider"

export function ThemeProvider({
  children,
}: ComponentProps<typeof NextThemesProvider>) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
    >
      <ColorThemeProvider>{children}</ColorThemeProvider>
    </NextThemesProvider>
  )
}
