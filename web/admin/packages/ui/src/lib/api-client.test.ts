import { describe, expect, it } from "bun:test"

import { adaptList, buildListQuery, type ApiEnvelope } from "./api-client"

describe("buildListQuery (simple dialect)", () => {
  it("returns an empty string when nothing is set", () => {
    expect(buildListQuery({})).toBe("")
  })

  it("serializes offset and limit", () => {
    expect(buildListQuery({ offset: 20, limit: 10 })).toBe("offset=20&limit=10")
  })

  it("emits offset/limit even when zero", () => {
    expect(buildListQuery({ offset: 0, limit: 0 })).toBe("offset=0&limit=0")
  })

  it("defaults a bare filter value to eq", () => {
    expect(buildListQuery({ filter: { status: "active" } })).toBe(
      "filter[status]=eq%3Aactive"
    )
  })

  it("serializes an explicit eq filter", () => {
    expect(
      buildListQuery({ filter: { name: { op: "eq", value: "foo" } } })
    ).toBe("filter[name]=eq%3Afoo")
  })

  it("wraps a like value in %…% when no wildcard is present", () => {
    expect(
      buildListQuery({ filter: { name: { op: "like", value: "foo" } } })
    ).toBe("filter[name]=like%3A%25foo%25")
  })

  it("leaves a like value untouched when the caller supplies a wildcard", () => {
    expect(
      buildListQuery({ filter: { name: { op: "like", value: "fo%" } } })
    ).toBe("filter[name]=like%3Afo%25")
  })

  it("wraps notlike values the same way as like", () => {
    expect(
      buildListQuery({ filter: { name: { op: "notlike", value: "bar" } } })
    ).toBe("filter[name]=notlike%3A%25bar%25")
  })

  it("serializes ordering ops (gt) verbatim", () => {
    expect(
      buildListQuery({ filter: { price: { op: "gt", value: 100 } } })
    ).toBe("filter[price]=gt%3A100")
  })

  it("joins an in-array with commas", () => {
    expect(
      buildListQuery({ filter: { id: { op: "in", value: [1, 2, 3] } } })
    ).toBe("filter[id]=in%3A1%2C2%2C3")
  })

  it("joins a between-array with commas", () => {
    expect(
      buildListQuery({ filter: { price: { op: "between", value: [10, 20] } } })
    ).toBe("filter[price]=between%3A10%2C20")
  })

  it("skips empty filter values", () => {
    expect(
      buildListQuery({
        filter: {
          a: { op: "eq", value: "" },
          b: { op: "in", value: [] },
          c: { op: "eq", value: null },
          d: { op: "eq", value: undefined },
        },
      })
    ).toBe("")
  })

  it("serializes sort directions", () => {
    expect(buildListQuery({ order: { createdAt: "DESC", name: "ASC" } })).toBe(
      "sort[createdAt]=DESC&sort[name]=ASC"
    )
  })

  it("combines pagination, filters, and sort in order", () => {
    expect(
      buildListQuery({
        offset: 0,
        limit: 25,
        filter: { name: { op: "like", value: "foo" } },
        order: { id: "ASC" },
      })
    ).toBe("offset=0&limit=25&filter[name]=like%3A%25foo%25&sort[id]=ASC")
  })
})

describe("buildListQuery (rsql dialect)", () => {
  it("serializes filter state to a single filter=<expr>", () => {
    expect(
      buildListQuery({
        syntax: "rsql",
        filter: {
          name: { op: "like", value: "foo" },
          price: { op: "gt", value: 100 },
        },
      })
    ).toBe("filter=name%3Dlike%3D%22%25foo%25%22%3Bprice%3Dgt%3D100")
  })

  it("sends a raw rsql expression verbatim, overriding filter", () => {
    expect(
      buildListQuery({
        syntax: "rsql",
        rsql: "id=in=(1,2,3)",
        filter: { name: "ignored" },
      })
    ).toBe("filter=id%3Din%3D(1%2C2%2C3)")
  })

  it("emits sort=field:DIR pairs joined by commas", () => {
    expect(
      buildListQuery({
        syntax: "rsql",
        order: { createdAt: "DESC", name: "ASC" },
      })
    ).toBe("sort=createdAt%3ADESC%2Cname%3AASC")
  })

  it("omits filter when the expression is empty", () => {
    expect(buildListQuery({ syntax: "rsql", offset: 5 })).toBe("offset=5")
  })
})

describe("adaptList", () => {
  const envelope = <T>(
    data: T,
    meta?: ApiEnvelope["meta"]
  ): ApiEnvelope<T> => ({
    code: 0,
    message: "",
    data,
    meta,
  })

  it("normalizes the native {data:[],meta} shape", () => {
    const out = adaptList<number>(envelope([1, 2, 3], { total: 3, offset: 10 }))
    expect(out).toEqual({ list: [1, 2, 3], total: 3, offset: 10 })
  })

  it("falls back to list length when meta.total is missing", () => {
    const out = adaptList<number>(envelope([1, 2]))
    expect(out).toEqual({ list: [1, 2], total: 2, offset: 0 })
  })

  it("normalizes the legacy {data:{list,total,offset}} shape", () => {
    const out = adaptList<string>(
      envelope({ list: ["a", "b"], total: 50, offset: 20 })
    )
    expect(out).toEqual({ list: ["a", "b"], total: 50, offset: 20 })
  })

  it("defaults legacy missing fields to empty/zero", () => {
    const out = adaptList<string>(envelope({ list: undefined } as unknown))
    expect(out).toEqual({ list: [], total: 0, offset: 0 })
  })

  it("returns an empty result when data is null", () => {
    const out = adaptList<number>(envelope(null))
    expect(out).toEqual({ list: [], total: 0, offset: 0 })
  })

  it("returns an empty result when data is a non-list object without `list`", () => {
    const out = adaptList<number>(envelope({ foo: "bar" }))
    expect(out).toEqual({ list: [], total: 0, offset: 0 })
  })
})
