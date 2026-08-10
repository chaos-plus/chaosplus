import { describe, expect, it } from "bun:test"
import {
  trustedContextCondition,
  trustedContextConditionDraft,
  trustedContextConditionSummary,
} from "./policy-condition"

describe("trusted context conditions", () => {
  it("builds, summarizes and restores the structured operator fields", () => {
    const condition = trustedContextCondition(
      "2",
      "console",
      "corp",
      "08:00",
      "18:00",
      "Asia/Shanghai"
    )

    expect(condition).toEqual({
      version: 1,
      all: [
        { gte: [{ context: "auth.acr" }, { value: 2 }] },
        { eq: [{ context: "client.id" }, { value: "console" }] },
        { eq: [{ context: "network.zone" }, { value: "corp" }] },
        { between_time: ["08:00", "18:00", "Asia/Shanghai"] },
      ],
    })
    expect(trustedContextConditionSummary(condition)).toBe(
      "ACR >= 2 AND client.id = console AND network.zone = corp AND 08:00-18:00 Asia/Shanghai"
    )
    expect(trustedContextConditionDraft(condition, "UTC")).toEqual({
      minimumAcr: "2",
      clientID: "console",
      networkZone: "corp",
      timeStart: "08:00",
      timeEnd: "18:00",
      timezone: "Asia/Shanghai",
    })
  })

  it("omits an unrestricted condition", () => {
    expect(trustedContextCondition("", "", "", "", "", "UTC")).toBeUndefined()
    expect(trustedContextConditionSummary(undefined)).toBe("不限")
  })
})
