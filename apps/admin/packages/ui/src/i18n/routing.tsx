import { useCallback, useMemo, type ComponentProps } from "react"
import {
  Link as RouterLink,
  useLocation,
  useNavigate,
  useParams,
} from "react-router"

import { locales, defaultLocale, type Locale } from "./config"

export const routing = { locales, defaultLocale } as const

function isLocale(seg: string | undefined): seg is Locale {
  return !!seg && (locales as readonly string[]).includes(seg)
}

/** 当前 URL 的 locale 段，取不到则默认。 */
export function useCurrentLocale(): string {
  const params = useParams()
  const fromParam = params.locale
  if (isLocale(fromParam)) return fromParam
  return defaultLocale
}

/** 给一个不含 locale 的 href 加上 locale 前缀。 */
export function getPathname(args: { href: string; locale: string }): string {
  const clean = args.href.startsWith("/") ? args.href : `/${args.href}`
  return `/${args.locale}${clean === "/" ? "" : clean}`
}

/** locale-aware Link：href 是不含 locale 的路径，内部补前缀。 */
export interface LinkProps extends Omit<
  ComponentProps<typeof RouterLink>,
  "to"
> {
  href: string
  locale?: string
}

export function Link({ href, locale, ...rest }: LinkProps) {
  const current = useCurrentLocale()
  const to = getPathname({ href, locale: locale ?? current })
  return <RouterLink to={to} {...rest} />
}

export function useRouter() {
  const navigate = useNavigate()
  const current = useCurrentLocale()
  const push = useCallback(
    (href: string, opts?: { locale?: string }) =>
      navigate(getPathname({ href, locale: opts?.locale ?? current })),
    [navigate, current]
  )
  const replace = useCallback(
    (href: string, opts?: { locale?: string }) =>
      navigate(getPathname({ href, locale: opts?.locale ?? current }), {
        replace: true,
      }),
    [navigate, current]
  )
  // next/navigation 的 useRouter() 返回一个跨渲染保持稳定引用的 router 对象，调用方
  // Scope providers can safely place the stable router in an effect dependency list.
  // 这里若不做 useMemo，每次渲染都会返回新的字面量对象，导致依赖它的 effect 每次渲染
  // 都重新执行，最终可能造成 "Maximum update depth exceeded" 循环。
  return useMemo(() => ({ push, replace }), [push, replace])
}

/** next-intl usePathname 返回不含 locale 前缀的路径，这里复刻。 */
export function usePathname(): string {
  const { pathname } = useLocation()
  const parts = pathname.split("/")
  if (isLocale(parts[1])) {
    const stripped = "/" + parts.slice(2).join("/")
    return stripped === "/" ? "/" : stripped.replace(/\/$/, "")
  }
  return pathname
}

/**
 * 兼容旧调用点：客户端硬跳转（会触发一次整页导航）。
 *
 * next-intl 的 redirect() 原本在服务端渲染时抛出以中断响应；SPA 没有这种服务端语义，
 * 因此这里改为浏览器端硬跳转。新代码应优先改用 useRouter().replace（无整页刷新）。
 *
 * 支持两种调用形态（对当前 29 个调用文件的 grep 结果显示：目前没有文件从
 * '@workspace/ui/i18n/routing' 导入 redirect ——真实存在的 redirect(...) 调用点
 * 全部来自 Next.js 'next/navigation' 的服务端 redirect，属于后续任务迁移这些页面时
 * 需要改写为 useRouter().replace 的范畴，不经过本函数。这里仍按文档化的稳定导出面
 * 同时支持对象与裸字符串两种形态，覆盖 next-intl navigation.redirect 的实际签名）：
 *   - redirect({ href: '/iam/users', locale: 'en-US' })
 *   - redirect('/iam/users')
 */
export function redirect(
  hrefOrArgs: string | { href: string; locale?: string }
): never {
  const args =
    typeof hrefOrArgs === "string" ? { href: hrefOrArgs } : hrefOrArgs
  const rawLocale =
    args.locale ??
    (typeof window !== "undefined"
      ? window.location.pathname.split("/")[1]
      : defaultLocale)
  const locale = isLocale(rawLocale) ? rawLocale : defaultLocale
  const to = getPathname({ href: args.href, locale })
  if (typeof window !== "undefined") window.location.assign(to)
  throw new Error("redirect")
}
