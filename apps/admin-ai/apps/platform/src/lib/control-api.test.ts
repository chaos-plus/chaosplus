import { describe, expect, it } from "bun:test";

import { controlApi } from "./control-api";

describe("control API routing", () => {
  it("keeps attachment content on the same-origin server-ai proxy", () => {
    expect(controlApi.attachmentContentUrl("attachment/a")).toBe(
      "/control/api/attachments/attachment%2Fa/content",
    );
  });
});
