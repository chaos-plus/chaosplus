/**
 * RSQL serialization for Chaosplus-compatible query endpoints.
 *
 * The backend's list endpoints accept TWO filter dialects: the "simple" syntax
 * (`filter[field]=op:value`) and full RSQL (`filter=<expr>`). This module produces the
 * latter, so the web client can express things the simple syntax cannot — OR, grouping,
 * IN/BETWEEN with mixed terms.
 *
 * Value coercion is deliberately kept 1:1 with the simple path (`pkg/rsql/simple.go`) so the
 * SAME filter inputs behave identically in either dialect:
 *   - eq / ne / like / notlike → string operands (quoted, so the RSQL parser keeps them strings)
 *   - gt / ge / lt / le        → numeric operands when numeric (parser int/float-parses bare tokens)
 *   - in / between             → list `( … )`, each item numeric-if-numeric else string (= parseList)
 * LIKE values are wrapped in `%…%` unless the caller already supplied a wildcard, matching the
 * simple path (the backend's RSQL LIKE does not auto-wrap).
 */

import type { FilterOp, FilterValue } from "./api-client"

export function isFilterValue(v: unknown): v is FilterValue {
  return typeof v === "object" && v !== null && "value" in v
}

export function isEmptyFilterValue(v: unknown): boolean {
  return (
    v === undefined ||
    v === null ||
    v === "" ||
    (Array.isArray(v) && v.length === 0)
  )
}

const RSQL_OPERATORS: Record<FilterOp, string> = {
  eq: "==",
  ne: "!=",
  like: "=like=",
  notlike: "=notlike=",
  gt: "=gt=",
  gte: "=ge=",
  lt: "=lt=",
  lte: "=le=",
  in: "=in=",
  between: "=between=",
}

const NUMERIC_RE = /^-?\d+(\.\d+)?$/

/** Quote a value so the RSQL parser reads it as a string, escaping the `; , ( )` delimiters. */
function quote(s: string): string {
  // The parser supports no escape sequences, so pick a quote char the value does not contain.
  const q = s.includes('"') && !s.includes("'") ? "'" : '"'
  return `${q}${s.split(q).join("")}${q}`
}

/** A bare numeric/boolean token, or a quoted string — mirrors `parseNumber` in the simple path. */
function numericOrString(v: unknown): string {
  if (typeof v === "number" && Number.isFinite(v)) return String(v)
  if (typeof v === "boolean") return v ? "true" : "false"
  const s = String(v ?? "")
  if (NUMERIC_RE.test(s) || s === "true" || s === "false") return s
  return quote(s)
}

function toList(v: unknown): string[] {
  const arr = Array.isArray(v) ? v : String(v ?? "").split(",")
  return arr.map((x) => String(x).trim()).filter((x) => x !== "")
}

/** Serialize a single `field op value` comparison to its RSQL form. */
export function rsqlComparison(
  field: string,
  op: FilterOp,
  value: unknown
): string {
  const operator = RSQL_OPERATORS[op]
  if (op === "in" || op === "between") {
    return `${field}${operator}(${toList(value).map(numericOrString).join(",")})`
  }
  if (op === "like" || op === "notlike") {
    let s = String(value)
    if (!s.includes("%")) s = `%${s}%`
    return `${field}${operator}${quote(s)}`
  }
  if (op === "gt" || op === "gte" || op === "lt" || op === "lte") {
    return `${field}${operator}${numericOrString(value)}`
  }
  // eq / ne → string semantics (matches the simple path keeping EQ/NE values as strings)
  return `${field}${operator}${quote(String(value))}`
}

/**
 * Convert a filter-bar state (field → {op,value}) into an AND-joined RSQL expression.
 * Empty values are dropped. Returns '' when nothing is set.
 */
export function filterStateToRsql(
  filter?: Record<string, FilterValue | unknown>
): string {
  const terms: string[] = []
  for (const [field, raw] of Object.entries(filter ?? {})) {
    const fv: FilterValue = isFilterValue(raw) ? raw : { value: raw }
    if (isEmptyFilterValue(fv.value)) continue
    terms.push(rsqlComparison(field, fv.op ?? "eq", fv.value))
  }
  return terms.join(";")
}

/**
 * Low-level RSQL expression builder for advanced filters the simple syntax can't express
 * (OR, grouping). Each method returns a serialized fragment string; combine with and()/or()
 * and wrap with group() to control precedence, then pass the result as `ListQuery.rsql`.
 *
 *   rsql.and(
 *     rsql.eq('bizType', 'WASH'),
 *     rsql.group(rsql.or(rsql.gt('priceNow', 100), rsql.in('id', [1, 2, 3]))),
 *   )
 *   // → bizType=="WASH";(priceNow=gt=100,id=in=(1,2,3))
 */
export const rsql = {
  eq: (field: string, v: unknown) => rsqlComparison(field, "eq", v),
  ne: (field: string, v: unknown) => rsqlComparison(field, "ne", v),
  like: (field: string, v: unknown) => rsqlComparison(field, "like", v),
  notlike: (field: string, v: unknown) => rsqlComparison(field, "notlike", v),
  gt: (field: string, v: unknown) => rsqlComparison(field, "gt", v),
  ge: (field: string, v: unknown) => rsqlComparison(field, "gte", v),
  lt: (field: string, v: unknown) => rsqlComparison(field, "lt", v),
  le: (field: string, v: unknown) => rsqlComparison(field, "lte", v),
  in: (field: string, vs: unknown[]) => rsqlComparison(field, "in", vs),
  between: (field: string, lo: unknown, hi: unknown) =>
    rsqlComparison(field, "between", [lo, hi]),
  /** Join fragments with AND (`;`). */
  and: (...nodes: Array<string | undefined | null | false>) =>
    nodes.filter((n): n is string => Boolean(n)).join(";"),
  /** Join fragments with OR (`,`). */
  or: (...nodes: Array<string | undefined | null | false>) =>
    nodes.filter((n): n is string => Boolean(n)).join(","),
  /** Parenthesize a fragment to control precedence. */
  group: (node: string) => (node ? `(${node})` : ""),
  /** Pass through a pre-built RSQL string unchanged. */
  raw: (expr: string) => expr,
}
