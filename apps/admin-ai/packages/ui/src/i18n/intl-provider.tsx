import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
  type ReactNode,
} from "react"
import { IntlProvider } from "use-intl"

import { isRTL, locales, type Locale } from "./config"
import { loadMessages } from "./load-messages"

interface ClientLocaleContextValue {
  locale: string
  setLocale: (locale: Locale) => void
  isPending: boolean
}

const ClientLocaleContext = createContext<ClientLocaleContextValue | null>(null)

export function useClientLocale(): ClientLocaleContextValue {
  const ctx = useContext(ClientLocaleContext)
  if (!ctx)
    throw new Error("useClientLocale must be used within <IntlAppProvider>")
  return ctx
}

export interface IntlAppProviderProps {
  initialLocale: string
  initialMessages: Record<string, unknown>
  /** 覆盖切语言时的懒加载函数；默认只加载 UI 包自身消息。业务侧（apps/web）应传入
   * 做 ui+业务 deep-merge 的 loadAppMessages，否则切语言后业务文案不会更新，
   * 直到下次整页刷新。与源站 ClientIntlProviderProps 的 loadMessages 覆盖机制一致。 */
  loadMessages?: (locale: string) => Promise<Record<string, unknown>>
  timeZone?: string
  children: ReactNode
}

export function IntlAppProvider({
  initialLocale,
  initialMessages,
  loadMessages: loadMessagesProp = loadMessages,
  timeZone = "UTC",
  children,
}: IntlAppProviderProps) {
  const [locale, setLocaleState] = useState(initialLocale)
  const [messages, setMessages] = useState(initialMessages)
  const [isPending, setIsPending] = useState(false)

  useEffect(() => {
    document.documentElement.lang = locale
    document.documentElement.dir = isRTL(locale as Locale) ? "rtl" : "ltr"
  }, [locale])

  const setLocale = useCallback(
    (next: Locale) => {
      if (next === locale) return
      document.cookie = `NEXT_LOCALE=${next}; path=/; max-age=31536000; samesite=lax`
      setIsPending(true)
      void loadMessagesProp(next)
        .then((msgs) => {
          setMessages(msgs)
          setLocaleState(next)
          const parts = window.location.pathname.split("/")
          if (parts[1] && (locales as readonly string[]).includes(parts[1])) {
            parts[1] = next
            // 不走 React Router 导航，避免重挂 [locale] 子树丢失未保存表单状态。
            window.history.replaceState(
              null,
              "",
              parts.join("/") + window.location.search + window.location.hash
            )
          }
          document.documentElement.lang = next
          document.documentElement.dir = isRTL(next) ? "rtl" : "ltr"
        })
        .finally(() => setIsPending(false))
    },
    [loadMessagesProp, locale]
  )

  return (
    <ClientLocaleContext.Provider value={{ locale, setLocale, isPending }}>
      <IntlProvider locale={locale} messages={messages} timeZone={timeZone}>
        {children}
      </IntlProvider>
    </ClientLocaleContext.Provider>
  )
}
