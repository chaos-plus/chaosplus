import { describe, expect, it } from "bun:test"
import type { EffectiveMenu } from "./iam-api"
import { effectiveMenuPaths, flattenEffectiveMenus } from "./navigation"

const menus: EffectiveMenu[] = [
  {
    id: "iam",
    label: "IAM",
    permission_code: "",
    sort_order: 0,
    children: [
      {
        id: "users",
        label: "Users",
        path: "/iam/users",
        permission_code: "user_view",
        sort_order: 10,
      },
    ],
  },
]

describe("effective navigation", () => {
  it("flattens nested authorized menu paths", () => {
    expect(flattenEffectiveMenus(menus).map((menu) => menu.id)).toEqual([
      "iam",
      "users",
    ])
    expect([...effectiveMenuPaths(menus)]).toEqual(["/iam/users"])
  })

  it("keeps an empty authorization result empty", () => {
    expect(flattenEffectiveMenus([])).toEqual([])
    expect(effectiveMenuPaths([]).size).toBe(0)
  })
})
