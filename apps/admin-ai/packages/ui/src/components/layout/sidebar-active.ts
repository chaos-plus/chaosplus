/**
 * Pure sidebar active-item resolution, extracted so it can be unit-tested without React.
 * The single active item = the LONGEST item href the current path matches (exactly or as a
 * path prefix), so a parent and its child (or prefix-overlapping siblings like /goods and
 * /goods/categories) never all highlight at once.
 */

export interface ActiveNavItem {
  href: string
  children?: ActiveNavItem[]
}

/** Strip a leading locale segment (only a full segment) and ensure a leading slash. */
export function normalizePath(p: string): string {
  const stripped = p.replace(/^\/[a-z]{2}(-[A-Z]{2})?(?=\/|$)/, "") || "/"
  return stripped.startsWith("/") ? stripped : `/${stripped}`
}

export function flattenHrefs(items: ActiveNavItem[]): string[] {
  return items.flatMap((it) => [
    it.href,
    ...(it.children ? flattenHrefs(it.children) : []),
  ])
}

/** Returns the normalized href of the single active item for `path`, or null. */
export function computeActiveHref(
  groups: { items: ActiveNavItem[] }[],
  path: string
): string | null {
  let best: string | null = null
  for (const group of groups) {
    for (const href of flattenHrefs(group.items)) {
      const h = normalizePath(href)
      if (path === h || path.startsWith(`${h}/`)) {
        if (!best || h.length > best.length) best = h
      }
    }
  }
  return best
}
