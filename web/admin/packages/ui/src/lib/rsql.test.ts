import { describe, expect, it } from "bun:test"

import { filterStateToRsql, rsql, rsqlComparison } from "./rsql"

describe("rsqlComparison", () => {
  it("keeps eq/ne operands as quoted strings (matches the simple path)", () => {
    expect(rsqlComparison("name", "eq", "foo")).toBe('name=="foo"')
    expect(rsqlComparison("name", "ne", "foo")).toBe('name!="foo"')
    // eq is string-semantic even for numbers, so it never numeric-coerces.
    expect(rsqlComparison("id", "eq", 123)).toBe('id=="123"')
  })

  it("emits bare numeric operands for ordering ops when the value is numeric", () => {
    expect(rsqlComparison("price", "gt", 100)).toBe("price=gt=100")
    expect(rsqlComparison("price", "gt", "100")).toBe("price=gt=100")
    expect(rsqlComparison("price", "gte", 100)).toBe("price=ge=100")
    expect(rsqlComparison("price", "lt", 5)).toBe("price=lt=5")
    expect(rsqlComparison("price", "lte", 5)).toBe("price=le=5")
  })

  it("quotes ordering operands that are not numeric", () => {
    expect(rsqlComparison("createdAt", "gt", "2026-01-01")).toBe(
      'createdAt=gt="2026-01-01"'
    )
  })

  it("auto-wraps LIKE in %…% unless a wildcard is already present", () => {
    expect(rsqlComparison("name", "like", "foo")).toBe('name=like="%foo%"')
    expect(rsqlComparison("name", "like", "fo%")).toBe('name=like="fo%"')
    expect(rsqlComparison("name", "notlike", "bar")).toBe(
      'name=notlike="%bar%"'
    )
  })

  it("builds IN / BETWEEN lists, coercing each item independently", () => {
    expect(rsqlComparison("id", "in", [1, 2, 3])).toBe("id=in=(1,2,3)")
    expect(rsqlComparison("tag", "in", ["a", "b"])).toBe('tag=in=("a","b")')
    expect(rsqlComparison("id", "in", [1, "a"])).toBe('id=in=(1,"a")')
    expect(rsqlComparison("price", "between", [10, 20])).toBe(
      "price=between=(10,20)"
    )
  })

  it("preserves a caller-supplied wildcard for notlike", () => {
    expect(rsqlComparison("name", "notlike", "ba%")).toBe('name=notlike="ba%"')
  })

  it("coerces a string IN from a comma-separated value", () => {
    expect(rsqlComparison("id", "in", "1,2,3")).toBe("id=in=(1,2,3)")
  })

  it("builds a mixed-type BETWEEN (numeric + string operands)", () => {
    expect(
      rsqlComparison("createdAt", "between", ["2026-01-01", "2026-12-31"])
    ).toBe('createdAt=between=("2026-01-01","2026-12-31")')
  })
})

describe("filterStateToRsql", () => {
  it("AND-joins terms with `;` in insertion order", () => {
    const out = filterStateToRsql({
      name: { op: "like", value: "foo" },
      price: { op: "gt", value: 100 },
    })
    expect(out).toBe('name=like="%foo%";price=gt=100')
  })

  it("defaults a raw (non-FilterValue) entry to eq", () => {
    expect(filterStateToRsql({ name: "foo" })).toBe('name=="foo"')
  })

  it("drops empty values", () => {
    expect(filterStateToRsql({ name: { op: "eq", value: "" } })).toBe("")
    expect(filterStateToRsql({ tags: { op: "in", value: [] } })).toBe("")
    expect(filterStateToRsql({ name: { op: "eq", value: null } })).toBe("")
  })

  it("returns empty string for empty / undefined input", () => {
    expect(filterStateToRsql({})).toBe("")
    expect(filterStateToRsql(undefined)).toBe("")
  })
})

describe("rsql builder", () => {
  it("composes the documented AND/OR/group example", () => {
    const out = rsql.and(
      rsql.eq("bizType", "WASH"),
      rsql.group(rsql.or(rsql.gt("priceNow", 100), rsql.in("id", [1, 2, 3])))
    )
    expect(out).toBe('bizType=="WASH";(priceNow=gt=100,id=in=(1,2,3))')
  })

  it("drops falsy nodes from and()/or()", () => {
    expect(rsql.and("a", false, "b", null, undefined)).toBe("a;b")
    expect(rsql.or("a", "", "b")).toBe("a,b")
  })

  it("group() parenthesizes and no-ops on empty", () => {
    expect(rsql.group("x")).toBe("(x)")
    expect(rsql.group("")).toBe("")
  })

  it("maps ge/le aliases to the right operators", () => {
    expect(rsql.ge("p", 1)).toBe("p=ge=1")
    expect(rsql.le("p", 1)).toBe("p=le=1")
  })
})
