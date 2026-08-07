import { z } from "zod"

export const autoFormRuleMessageKeys = {
  safeInteger: "form.validation.safeInteger",
  positiveInteger: "form.validation.positiveInteger",
  priceRmb: "form.validation.priceRmb",
  mobile: "form.validation.mobile",
  email: "form.validation.email",
  required: "form.required",
  requiredField: "form.validation.requiredField",
  number: "form.validation.number",
  numberField: "form.validation.numberField",
  percent: "form.validation.percent",
} as const

type AutoFormRuleMessages = Partial<
  Record<keyof typeof autoFormRuleMessageKeys, string>
>

export function createAutoFormRules(messages: AutoFormRuleMessages = {}) {
  return {
    safeInteger: (v: unknown): string | undefined =>
      Number.isSafeInteger(Number(v)) && String(v) !== ""
        ? undefined
        : (messages.safeInteger ?? "Enter a valid integer"),

    positiveInteger: (v: unknown): string | undefined => {
      const n = Number(v)
      return Number.isInteger(n) && n > 0
        ? undefined
        : (messages.positiveInteger ?? "Enter a positive integer")
    },

    priceRmb: (v: unknown): string | undefined =>
      /^([1-9]\d*|0)$/.test(String(v))
        ? undefined
        : (messages.priceRmb ?? "Enter a valid amount in cents"),

    mobile: (v: unknown): string | undefined =>
      /^1[3-9]\d{9}$/.test(String(v))
        ? undefined
        : (messages.mobile ?? "Enter a valid mobile number"),

    email: (v: unknown): string | undefined =>
      /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(String(v))
        ? undefined
        : (messages.email ?? "Enter a valid email address"),

    // --- parity with legacy FORM_ITEM_RULES ---
    number: (v: unknown): string | undefined =>
      v === "" || v == null || !Number.isNaN(Number(v))
        ? undefined
        : (messages.number ?? "Must be a number"),

    integer: (v: unknown): string | undefined =>
      /^[0-9]{0,10}$/.test(String(v ?? ""))
        ? undefined
        : (messages.positiveInteger ?? "Must be a positive integer"),

    percent: (v: unknown): string | undefined => {
      const s = String(v ?? "")
      const n = Number(s)
      if (Number.isNaN(n)) return messages.number ?? "Must be a number"
      if (!/^[0-9]{0,10}$/.test(s) || n < 0 || n > 100)
        return messages.percent ?? "Must be an integer between 0 and 100"
      return undefined
    },

    /** RMB amount in yuan: non-negative, at most 2 decimal places. */
    priceYuan: (v: unknown): string | undefined => {
      const s = String(v ?? "")
      if (s === "") return undefined
      if (Number.isNaN(Number(s)))
        return messages.priceRmb ?? "Must be a number"
      if (s.indexOf("-") >= 0)
        return messages.priceRmb ?? "Must be non-negative"
      const dot = s.indexOf(".")
      if (dot >= 0 && s.length - dot - 1 > 2)
        return messages.priceRmb ?? "At most 2 decimal places"
      return undefined
    },
  }
}

export const autoFormRules = {
  ...createAutoFormRules(),
}

/**
 * Rule presets bundled as arrays, mirroring the legacy `FORM_ITEM_RULES.*` so factory
 * configs read the same: `config: { rules: FORM_RULES.PERCENT }`.
 */
type FormRule = (value: unknown) => string | undefined

export const FORM_RULES: Record<string, FormRule[]> = {
  NUMBER: [autoFormRules.number],
  INTEGER: [autoFormRules.integer],
  SAFE_INTEGER: [autoFormRules.safeInteger],
  PERCENT: [autoFormRules.percent],
  PRICE_RMB: [autoFormRules.priceYuan],
}

export function buildRequiredString(
  name?: string,
  messages: AutoFormRuleMessages = {}
) {
  return z
    .string()
    .min(
      1,
      name
        ? (messages.requiredField ?? `${name} is required`)
        : (messages.required ?? "Required")
    )
}

export function buildOptionalString() {
  return z.string().optional()
}

export function buildRequiredNumber(
  name?: string,
  messages: AutoFormRuleMessages = {}
) {
  return z.coerce.number({
    message: name
      ? (messages.numberField ?? `${name} must be a number`)
      : (messages.number ?? "Must be a number"),
  })
}
