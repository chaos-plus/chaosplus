import { describe, expect, it } from "bun:test"

import { createAutoFormRules, FORM_RULES } from "./validation"

const rules = createAutoFormRules()

describe("createAutoFormRules: safeInteger", () => {
  it("accepts safe integers", () => {
    expect(rules.safeInteger("42")).toBeUndefined()
    expect(rules.safeInteger(0)).toBeUndefined()
  })

  it("rejects empty input and non-integers", () => {
    expect(rules.safeInteger("")).toBeDefined()
    expect(rules.safeInteger("abc")).toBeDefined()
    expect(rules.safeInteger(Number.MAX_SAFE_INTEGER + 1)).toBeDefined()
  })

  it("uses a custom message when provided", () => {
    const r = createAutoFormRules({ safeInteger: "bad int" })
    expect(r.safeInteger("")).toBe("bad int")
  })
})

describe("createAutoFormRules: positiveInteger", () => {
  it("accepts positive integers", () => {
    expect(rules.positiveInteger("5")).toBeUndefined()
    expect(rules.positiveInteger(1)).toBeUndefined()
  })

  it("rejects zero, negatives, and non-integers", () => {
    expect(rules.positiveInteger(0)).toBeDefined()
    expect(rules.positiveInteger(-3)).toBeDefined()
    expect(rules.positiveInteger("1.5")).toBeDefined()
    expect(rules.positiveInteger("x")).toBeDefined()
  })
})

describe("createAutoFormRules: priceRmb", () => {
  it("accepts a non-negative integer amount in cents", () => {
    expect(rules.priceRmb("0")).toBeUndefined()
    expect(rules.priceRmb("1990")).toBeUndefined()
  })

  it("rejects leading zeros, decimals, and negatives", () => {
    expect(rules.priceRmb("007")).toBeDefined()
    expect(rules.priceRmb("19.9")).toBeDefined()
    expect(rules.priceRmb("-5")).toBeDefined()
    expect(rules.priceRmb("abc")).toBeDefined()
  })
})

describe("createAutoFormRules: mobile", () => {
  it("accepts a valid mainland mobile number", () => {
    expect(rules.mobile("13800138000")).toBeUndefined()
    expect(rules.mobile("19912345678")).toBeUndefined()
  })

  it("rejects malformed numbers", () => {
    expect(rules.mobile("12345678901")).toBeDefined() // second digit must be 3-9
    expect(rules.mobile("1380013800")).toBeDefined() // too short
    expect(rules.mobile("138001380000")).toBeDefined() // too long
    expect(rules.mobile("abcdefghijk")).toBeDefined()
  })
})

describe("createAutoFormRules: email", () => {
  it("accepts a well-formed address", () => {
    expect(rules.email("user@example.com")).toBeUndefined()
    expect(rules.email("a.b+c@sub.domain.co")).toBeUndefined()
  })

  it("rejects malformed addresses", () => {
    expect(rules.email("no-at-sign")).toBeDefined()
    expect(rules.email("missing@domain")).toBeDefined()
    expect(rules.email("spaces in@example.com")).toBeDefined()
    expect(rules.email("@example.com")).toBeDefined()
  })
})

describe("createAutoFormRules: number", () => {
  it("treats empty/nullish as valid (optional)", () => {
    expect(rules.number("")).toBeUndefined()
    expect(rules.number(null)).toBeUndefined()
  })

  it("accepts numeric strings and rejects non-numeric", () => {
    expect(rules.number("3.14")).toBeUndefined()
    expect(rules.number("abc")).toBeDefined()
  })
})

describe("createAutoFormRules: percent", () => {
  it("accepts integers between 0 and 100", () => {
    expect(rules.percent("0")).toBeUndefined()
    expect(rules.percent("50")).toBeUndefined()
    expect(rules.percent("100")).toBeUndefined()
  })

  it("rejects values outside the range and non-numbers", () => {
    expect(rules.percent("101")).toBeDefined()
    expect(rules.percent("-1")).toBeDefined()
    expect(rules.percent("abc")).toBeDefined()
  })
})

describe("createAutoFormRules: priceYuan", () => {
  it("accepts empty and well-formed amounts", () => {
    expect(rules.priceYuan("")).toBeUndefined()
    expect(rules.priceYuan("19.99")).toBeUndefined()
    expect(rules.priceYuan("100")).toBeUndefined()
  })

  it("rejects negatives, >2 decimals, and non-numbers", () => {
    expect(rules.priceYuan("-1")).toBeDefined()
    expect(rules.priceYuan("1.999")).toBeDefined()
    expect(rules.priceYuan("abc")).toBeDefined()
  })
})

describe("FORM_RULES presets", () => {
  it("exposes each preset as an array of validator functions", () => {
    for (const key of [
      "NUMBER",
      "INTEGER",
      "SAFE_INTEGER",
      "PERCENT",
      "PRICE_RMB",
    ]) {
      const preset = FORM_RULES[key]
      expect(Array.isArray(preset)).toBe(true)
      expect(preset?.length).toBeGreaterThan(0)
      expect(typeof preset?.[0]).toBe("function")
    }
  })

  it("PERCENT preset validates the 0-100 range", () => {
    const validate = FORM_RULES.PERCENT?.[0]
    expect(validate?.("50")).toBeUndefined()
    expect(validate?.("200")).toBeDefined()
  })

  it("PRICE_RMB preset validates yuan amounts", () => {
    const validate = FORM_RULES.PRICE_RMB?.[0]
    expect(validate?.("19.99")).toBeUndefined()
    expect(validate?.("1.999")).toBeDefined()
  })
})
