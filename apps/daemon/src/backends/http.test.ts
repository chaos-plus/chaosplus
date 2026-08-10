import { describe, expect, test } from "bun:test";
import { runHttp } from "./http";
import type { AgentEvent } from "../types";

async function collect(gen: AsyncGenerator<AgentEvent>): Promise<AgentEvent[]> {
  const evs: AgentEvent[] = [];
  for await (const ev of gen) evs.push(ev);
  return evs;
}

describe("runHttp", () => {
  test("rejects non-JSON prompt", async () => {
    const evs = await collect(runHttp({ prompt: "not-json", cwd: "/tmp" }));
    const err = evs.find((e) => e.type === "error");
    expect(err).toBeDefined();
    const done = evs.find((e) => e.type === "done");
    expect(done!.ok).toBe(false);
  });

  test("emits session start event", async () => {
    // ponytail: test against a local listener so CI doesn't need internet.
    const server = Bun.serve({ port: 0, fetch: () => new Response("ok") });
    try {
      const evs = await collect(runHttp({
        prompt: JSON.stringify({ url: server.url.href }),
        cwd: "/tmp",
      }));
      expect(evs[0]).toEqual({ type: "session", status: "running" });
      const done = evs.find((e) => e.type === "done");
      expect(done!.ok).toBe(true);
    } finally {
      server.stop();
    }
  });

  test("reports done with exitCode on success", async () => {
    const server = Bun.serve({ port: 0, fetch: () => new Response("ok") });
    try {
      const evs = await collect(runHttp({
        prompt: JSON.stringify({ url: server.url.href }),
        cwd: "/tmp",
      }));
      const done = evs.find((e) => e.type === "done");
      expect(done!.ok).toBe(true);
      expect(done!.exitCode).toBe(0);
    } finally {
      server.stop();
    }
  });
});
