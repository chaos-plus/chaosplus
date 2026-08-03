import { afterEach, describe, expect, it } from "bun:test"

import { createApiClient } from "./api-client"

describe("api-client 401 auto-refresh", () => {
  let server: ReturnType<typeof Bun.serve> | undefined
  afterEach(() => server?.stop(true))

  it("renews and replays the request once when onUnauthorized succeeds", async () => {
    let requests = 0
    server = Bun.serve({
      port: 0,
      fetch() {
        requests++
        if (requests === 1)
          return Response.json(
            { code: 401, message: "Unauthorized", data: null },
            { status: 401 }
          )
        return Response.json({ code: 0, message: "", data: { value: 1 } })
      },
    })
    let refreshed = 0
    const client = createApiClient({
      baseUrl: `http://127.0.0.1:${server.port}`,
      getToken: () => "token",
      onUnauthorized: async () => {
        refreshed++
        return true
      },
    })
    const response = await client.get<{ value: number }>("/thing")
    expect(response.data).toEqual({ value: 1 })
    expect(refreshed).toBe(1)
    expect(requests).toBe(2)
  })

  it("propagates 401 when renewal fails", async () => {
    server = Bun.serve({
      port: 0,
      fetch: () =>
        Response.json(
          { code: 401, message: "Unauthorized", data: null },
          { status: 401 }
        ),
    })
    const client = createApiClient({
      baseUrl: `http://127.0.0.1:${server.port}`,
      getToken: () => "token",
      onUnauthorized: async () => false,
    })
    await expect(client.get("/thing")).rejects.toThrow()
  })

  it("does not retry without a renewal hook", async () => {
    let requests = 0
    server = Bun.serve({
      port: 0,
      fetch() {
        requests++
        return Response.json(
          { code: 401, message: "Unauthorized", data: null },
          { status: 401 }
        )
      },
    })
    const client = createApiClient({
      baseUrl: `http://127.0.0.1:${server.port}`,
      getToken: () => "token",
    })
    await expect(client.get("/thing")).rejects.toThrow()
    expect(requests).toBe(1)
  })
})
