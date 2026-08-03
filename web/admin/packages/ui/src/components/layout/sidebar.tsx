import { useState } from "react"
import { useTranslations } from "use-intl"
import { Link, usePathname } from "@workspace/ui/i18n/routing"
import { cn } from "@workspace/ui/lib/utils"
import { ChevronRight } from "lucide-react"

import { computeActiveHref, normalizePath } from "./sidebar-active"

export interface SidebarItem {
  href: string
  /** i18n translation key — used for static hardcoded menus. */
  labelKey?: string
  /** Direct label string — used for dynamic menus from the API (takes priority over labelKey). */
  label?: string
  icon?: React.ReactNode
  children?: SidebarItem[]
  /** Permission required to see this item. When absent, the item is always visible. */
  perm?: string
}

export interface SidebarGroup {
  title?: string
  titleKey?: string
  items: SidebarItem[]
}

export interface SidebarProps {
  groups?: SidebarGroup[]
  className?: string
  /** Rendered above the nav groups (e.g. brand logo + utility controls). */
  header?: React.ReactNode
  /**
   * Predicate gating items by their `perm`. Injected by the app (which owns auth state) so
   * this package stays dependency-free. Defaults to allow-all.
   */
  canAccess?: (perm: string) => boolean
}

/** Keep an item if it has no `perm` or the user can access it; drop parents left childless. */
function filterItems(
  items: SidebarItem[],
  canAccess: (perm: string) => boolean
): SidebarItem[] {
  const out: SidebarItem[] = []
  for (const item of items) {
    if (item.perm && !canAccess(item.perm)) continue
    if (item.children && item.children.length > 0) {
      const children = filterItems(item.children, canAccess)
      if (children.length === 0) continue
      out.push({ ...item, children })
    } else {
      out.push(item)
    }
  }
  return out
}

function SidebarItemRender({
  item,
  activeHref,
  depth = 0,
}: {
  item: SidebarItem
  activeHref: string | null
  depth?: number
}) {
  const t = useTranslations("navigation")
  const normalizedHref = normalizePath(item.href)
  const hasChildren = Boolean(item.children && item.children.length > 0)
  // Only a leaf can win the highlight pill; the active section's parent merely expands/emphasizes.
  const isActive = !hasChildren && normalizedHref === activeHref
  const containsActive =
    activeHref != null &&
    (activeHref === normalizedHref ||
      activeHref.startsWith(`${normalizedHref}/`))
  const [open, setOpen] = useState(() => containsActive && hasChildren)
  const label = item.label ?? (item.labelKey ? t(item.labelKey) : item.href)

  // Auto-expand the section that contains the active route (e.g. on client navigation).
  // Tracking the last-seen `containsActive` and adjusting state during render (instead of in
  // an effect) avoids the extra commit/paint cycle that `set-state-in-effect` warns about.
  const [wasActive, setWasActive] = useState(containsActive)
  if (containsActive !== wasActive) {
    setWasActive(containsActive)
    if (containsActive && hasChildren) setOpen(true)
  }

  const itemClass = cn(
    "flex w-full items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
    isActive
      ? "sidebar-active-item bg-sidebar-primary font-medium text-sidebar-primary-foreground"
      : containsActive
        ? "font-medium text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
        : "text-sidebar-foreground/90 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
  )
  const itemContent = (
    <>
      {item.icon && (
        <span
          className={cn(
            "shrink-0",
            isActive
              ? "text-current"
              : containsActive
                ? "text-sidebar-foreground"
                : "text-sidebar-foreground/90"
          )}
          aria-hidden="true"
        >
          {item.icon}
        </span>
      )}
      <span className="truncate">{label}</span>
      {hasChildren && (
        <ChevronRight
          className={cn(
            "ms-auto size-4 shrink-0 transition-transform",
            open && "rotate-90"
          )}
          aria-hidden="true"
        />
      )}
    </>
  )

  return (
    <div className={cn("ms-0", depth > 0 && "ms-3 border-s ps-2")}>
      {hasChildren ? (
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          className={itemClass}
        >
          {itemContent}
        </button>
      ) : (
        <Link
          href={item.href}
          aria-current={isActive ? "page" : undefined}
          className={itemClass}
        >
          {itemContent}
        </Link>
      )}
      {hasChildren && open && (
        <div className="mt-1 space-y-1">
          {item.children?.map((child) => (
            <SidebarItemRender
              key={child.href}
              item={child}
              activeHref={activeHref}
              depth={depth + 1}
            />
          ))}
        </div>
      )}
    </div>
  )
}

export function Sidebar({ groups = [], className, canAccess }: SidebarProps) {
  const t = useTranslations("navigation")
  const pathname = usePathname()
  const visibleGroups = canAccess
    ? groups
        .map((group) => ({
          ...group,
          items: filterItems(group.items, canAccess),
        }))
        .filter((group) => group.items.length > 0)
    : groups
  const activeHref = computeActiveHref(visibleGroups, normalizePath(pathname))

  return (
    <aside
      className={cn(
        "hidden w-60 shrink-0 overflow-y-auto border-e border-sidebar-border bg-sidebar p-3 md:block",
        className
      )}
    >
      <div className="space-y-4">
        {visibleGroups.map((group, idx) => (
          <div key={idx}>
            {(group.title || group.titleKey) && (
              <h3 className="mb-2 px-3 text-xs font-semibold text-sidebar-foreground/60 uppercase">
                {group.titleKey ? t(group.titleKey) : group.title}
              </h3>
            )}
            <div className="space-y-1">
              {group.items.map((item) => (
                <SidebarItemRender
                  key={item.href}
                  item={item}
                  activeHref={activeHref}
                />
              ))}
            </div>
          </div>
        ))}
      </div>
    </aside>
  )
}
