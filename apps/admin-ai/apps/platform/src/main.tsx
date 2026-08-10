import { StrictMode } from "react"
import { createRoot } from "react-dom/client"
import { RouterProvider } from "react-router"
import { IntlAppProvider } from "@workspace/ui/i18n/intl-provider"
import "./index.css"
import { ThemeProvider } from "./components/theme-provider"
import { currentLocale, loadAppMessages } from "./i18n"
import { router } from "./router"

// 先加载当前语言的词条再挂载,避免首屏闪一遍 key。
const locale = currentLocale()
const messages = await loadAppMessages(locale)

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <IntlAppProvider initialLocale={locale} initialMessages={messages} loadMessages={loadAppMessages}>
      <ThemeProvider>
        <RouterProvider router={router} />
      </ThemeProvider>
    </IntlAppProvider>
  </StrictMode>
)
