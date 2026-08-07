import { useTranslations } from "use-intl"
import { Link } from "@workspace/ui/i18n/routing"
import { cn } from "@workspace/ui/lib/utils"

import type { ReactNode } from "react"

export interface NavItem {
  href: string
  labelKey: string
  children?: NavItem[]
}

export interface TopNavProps {
  items?: NavItem[]
  brand?: ReactNode
  trailing?: ReactNode
  navigationLabel?: string
  className?: string
}

export function TopNav({
  items = [],
  brand,
  trailing,
  navigationLabel,
  className,
}: TopNavProps) {
  const t = useTranslations("navigation")

  return (
    <header
      className={cn(
        "sticky top-0 z-50 border-b bg-background shadow-sm dark:bg-background",
        className
      )}
    >
      <div className="mx-auto flex h-14 items-center justify-between px-4">
        <div className="flex items-center gap-6">
          {brand ? (
            <div className="text-lg font-semibold">{brand}</div>
          ) : (
            <Link href="/" className="text-lg font-semibold">
              Chaosplus
            </Link>
          )}
          <nav
            className="hidden items-center gap-4 md:flex"
            aria-label={navigationLabel}
          >
            {items.map((item, index) => {
              const key = item.labelKey || item.href || index
              if (item.children?.length) {
                return (
                  <details key={key} className="group relative">
                    <summary className="flex cursor-pointer list-none items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
                      {t(item.labelKey)}
                      <span
                        aria-hidden="true"
                        className="text-xs transition-transform group-open:rotate-180"
                      >
                        v
                      </span>
                    </summary>
                    <div className="absolute top-full left-0 z-50 mt-2 min-w-44 rounded-md border bg-popover p-1 shadow-lg">
                      <Link
                        href={item.href}
                        className="block rounded-sm px-3 py-2 text-sm font-medium text-popover-foreground hover:bg-accent hover:text-accent-foreground"
                      >
                        {t(item.labelKey)}
                      </Link>
                      {item.children.map((child) => (
                        <Link
                          key={child.labelKey || child.href}
                          href={child.href}
                          className="block rounded-sm px-3 py-2 text-sm text-popover-foreground hover:bg-accent hover:text-accent-foreground"
                        >
                          {t(child.labelKey)}
                        </Link>
                      ))}
                    </div>
                  </details>
                )
              }
              return (
                <Link
                  key={key}
                  href={item.href}
                  className="text-sm text-muted-foreground hover:text-foreground"
                >
                  {t(item.labelKey)}
                </Link>
              )
            })}
          </nav>
        </div>
        {trailing && <div className="flex items-center gap-2">{trailing}</div>}
      </div>
      {items.length > 0 && (
        <nav
          className="flex gap-2 overflow-x-auto border-t px-4 py-2 md:hidden"
          aria-label={navigationLabel}
        >
          {items.map((item, index) => {
            const key = item.labelKey || item.href || index
            if (item.children?.length) {
              return (
                <details key={key} className="shrink-0">
                  <summary className="inline-flex cursor-pointer list-none rounded-md border px-3 py-1.5 text-sm text-muted-foreground">
                    {t(item.labelKey)}
                  </summary>
                  <div className="mt-1 grid gap-1 rounded-md border bg-popover p-1">
                    <Link
                      href={item.href}
                      className="rounded-sm px-3 py-1.5 text-sm font-medium text-popover-foreground"
                    >
                      {t(item.labelKey)}
                    </Link>
                    {item.children.map((child) => (
                      <Link
                        key={child.labelKey || child.href}
                        href={child.href}
                        className="rounded-sm px-3 py-1.5 text-sm text-popover-foreground"
                      >
                        {t(child.labelKey)}
                      </Link>
                    ))}
                  </div>
                </details>
              )
            }
            return (
              <Link
                key={key}
                href={item.href}
                className="inline-flex shrink-0 rounded-md border px-3 py-1.5 text-sm text-muted-foreground"
              >
                {t(item.labelKey)}
              </Link>
            )
          })}
        </nav>
      )}
    </header>
  )
}
