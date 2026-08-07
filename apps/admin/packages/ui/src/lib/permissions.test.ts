import { describe, expect, it } from "bun:test"

import { hasAny, matchPermission } from "./permissions"

describe("matchPermission", () => {
  it("grants everything for the `*` wildcard", () => {
    expect(matchPermission(["*"], "users:read")).toBe(true)
    expect(matchPermission(["*"], "anything:at:all")).toBe(true)
  })

  it("grants on exact match", () => {
    expect(matchPermission(["users:read"], "users:read")).toBe(true)
  })

  it("grants on a resource wildcard `resource:*`", () => {
    expect(matchPermission(["users:*"], "users:read")).toBe(true)
    expect(matchPermission(["users:*"], "users:write")).toBe(true)
  })

  it("does not let a resource wildcard leak to other resources", () => {
    expect(matchPermission(["users:*"], "roles:read")).toBe(false)
  })

  it("denies a different action under the same resource (no wildcard)", () => {
    expect(matchPermission(["users:read"], "users:write")).toBe(false)
  })

  it("denies when nothing is granted", () => {
    expect(matchPermission([], "users:read")).toBe(false)
  })

  it("treats an empty required string as allow (no gate)", () => {
    expect(matchPermission([], "")).toBe(true)
  })

  it("scans the whole granted set, not just the first entry", () => {
    expect(matchPermission(["goods:read", "users:read"], "users:read")).toBe(
      true
    )
  })
})

describe("hasAny", () => {
  it("allows when the required list is empty", () => {
    expect(hasAny(["users:read"], [])).toBe(true)
    expect(hasAny([], [])).toBe(true)
  })

  it("passes if ANY required permission is satisfied", () => {
    expect(hasAny(["users:read"], ["users:write", "users:read"])).toBe(true)
  })

  it("fails when none are satisfied", () => {
    expect(hasAny(["goods:read"], ["users:write", "roles:read"])).toBe(false)
  })

  it("honors wildcards through to matchPermission", () => {
    expect(hasAny(["users:*"], ["users:delete"])).toBe(true)
  })
})
