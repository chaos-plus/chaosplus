import { describe, expect, it } from "bun:test"

import {
  computeActiveHref,
  normalizePath,
  type ActiveNavItem,
} from "./sidebar-active"

describe("normalizePath", () => {
  it("strips a leading locale segment", () => {
    expect(normalizePath("/en-US/goods")).toBe("/goods")
    expect(normalizePath("/zh/goods")).toBe("/goods")
    expect(normalizePath("/en-US")).toBe("/")
  })

  it("does NOT strip a real first segment that merely looks short", () => {
    // `/go` is a route segment, not a locale — the (?=\/|$) anchor must protect it.
    expect(normalizePath("/goods")).toBe("/goods")
    expect(normalizePath("/users/roles")).toBe("/users/roles")
  })

  it("keeps the root path", () => {
    expect(normalizePath("/")).toBe("/")
  })
})

const groups: { items: ActiveNavItem[] }[] = [
  {
    items: [
      { href: "/dashboard" },
      { href: "/goods", children: [{ href: "/goods/categories" }] },
      {
        href: "/users",
        children: [{ href: "/users/roles" }, { href: "/users/permissions" }],
      },
    ],
  },
]

describe("computeActiveHref", () => {
  it("matches an exact leaf href", () => {
    expect(computeActiveHref(groups, "/dashboard")).toBe("/dashboard")
  })

  it("returns a SINGLE winner — the longest matching href — for nested paths", () => {
    // Both /goods and /goods/categories prefix-match; only the longest wins (no double-highlight).
    expect(computeActiveHref(groups, "/goods/categories")).toBe(
      "/goods/categories"
    )
  })

  it("matches a parent when on the parent path itself", () => {
    expect(computeActiveHref(groups, "/goods")).toBe("/goods")
  })

  it("treats deeper sub-paths as belonging to the longest registered ancestor", () => {
    expect(computeActiveHref(groups, "/goods/categories/123")).toBe(
      "/goods/categories"
    )
    expect(computeActiveHref(groups, "/users/roles/5/edit")).toBe(
      "/users/roles"
    )
  })

  it("works after locale normalization", () => {
    expect(
      computeActiveHref(groups, normalizePath("/en-US/users/permissions"))
    ).toBe("/users/permissions")
  })

  it("returns null when nothing matches", () => {
    expect(computeActiveHref(groups, "/unknown")).toBeNull()
  })

  it("does not let a sibling prefix bleed across boundaries", () => {
    // `/users` must not match `/users-archive` (only exact or `/users/` prefix).
    expect(computeActiveHref(groups, "/users-archive")).toBeNull()
  })
})
