import { describe, expect, it } from "bun:test"
import {
  membershipState,
  membershipWindow,
  toDateTimeLocal,
  toISOString,
} from "./membership-presentation"

describe("directory membership presentation", () => {
  const now = Date.parse("2026-08-02T12:00:00Z")

  it("classifies scheduled membership windows", () => {
    expect(membershipState({}, now)).toBe("有效")
    expect(membershipState({ starts_at: "2026-08-03T00:00:00Z" }, now)).toBe(
      "待生效"
    )
    expect(membershipState({ ends_at: "2026-08-02T12:00:00Z" }, now)).toBe(
      "已结束"
    )
    expect(
      membershipState(
        { starts_at: "2026-08-01T00:00:00Z", ends_at: "2026-08-03T00:00:00Z" },
        now
      )
    ).toBe("有效")
  })

  it("converts timestamps for controls and API requests", () => {
    expect(toDateTimeLocal()).toBe("")
    expect(toDateTimeLocal("2026-08-02T12:34:00Z")).toMatch(
      /^2026-08-02T\d{2}:34$/
    )
    expect(toISOString("")).toBeUndefined()
    expect(toISOString("not-a-date")).toBeUndefined()
    expect(toISOString("2026-08-02T12:34")).toMatch(
      /^2026-08-02T\d{2}:34:00\.000Z$/
    )
  })

  it("formats open and bounded membership windows", () => {
    expect(membershipWindow({})).toBe("不限 - 不限")
    const bounded = membershipWindow({
      starts_at: "2026-08-02T00:00:00Z",
      ends_at: "2026-08-03T00:00:00Z",
    })
    expect(bounded).not.toContain("Invalid Date")
    expect(bounded).not.toContain("不限")
  })
})
