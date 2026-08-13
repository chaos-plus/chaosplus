import { afterEach, describe, expect, it } from "bun:test";

import { controlApi } from "./control-api";

const originalFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = originalFetch;
});

describe("control API routing", () => {
  it("routes workspace requests through the server-ai proxy", async () => {
    let requested = "";
    globalThis.fetch = Object.assign(
      async (input: RequestInfo | URL) => {
        requested = String(input);
        return new Response(JSON.stringify({ data: [] }), {
          headers: { "Content-Type": "application/json" },
        });
      },
      { preconnect: originalFetch.preconnect },
    );

    await controlApi.testCases();

    expect(requested).toBe("/control/api/test-cases");
  });

  it("uses server-ai for attachment content and versioned deletion", async () => {
    let requested = "";
    let method = "";
    globalThis.fetch = Object.assign(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        requested = String(input);
        method = init?.method ?? "GET";
        return new Response(JSON.stringify({ ok: true }), {
          headers: { "Content-Type": "application/json" },
        });
      },
      { preconnect: originalFetch.preconnect },
    );

    expect(controlApi.attachmentContentUrl("attachment/a")).toBe(
      "/control/api/attachments/attachment%2Fa/content",
    );
    await controlApi.deleteAttachment("attachment/a", 3);

    expect(requested).toBe("/control/api/attachments/attachment%2Fa?version=3");
    expect(method).toBe("DELETE");
  });
});
