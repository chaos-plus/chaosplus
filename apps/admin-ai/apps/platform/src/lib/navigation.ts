import type { EffectiveMenu } from "./iam-api"

export function flattenEffectiveMenus(menus: EffectiveMenu[]): EffectiveMenu[] {
  return menus.flatMap((menu) => [
    menu,
    ...flattenEffectiveMenus(menu.children ?? []),
  ])
}

export function effectiveMenuPaths(menus: EffectiveMenu[]): Set<string> {
  return new Set(
    flattenEffectiveMenus(menus)
      .map((menu) => menu.path)
      .filter((path): path is string => Boolean(path))
  )
}
