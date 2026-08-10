import { describe, expect, test } from "bun:test";
import { runScript } from "./script";
import type { AgentEvent } from "../types";

async function collect(gen: AsyncGenerator<AgentEvent>): Promise<AgentEvent[]> {
  const evs: AgentEvent[] = [];
  for await (const ev of gen) evs.push(ev);
  return evs;
}

describe("runScript", () => {
  test("runs a simple bash echo", async () => {
    const evs = await collect(runScript({ prompt: "echo hello-world", cwd: "/tmp" }));
    const done = evs.find((e) => e.type === "done");
    expect(done).toBeDefined();
    expect(done!.ok).toBe(true);
    expect(done!.exitCode).toBe(0);
  });

  test("captures exit code on failure", async () => {
    const evs = await collect(runScript({ prompt: "exit 42", cwd: "/tmp" }));
    const done = evs.find((e) => e.type === "done");
    expect(done).toBeDefined();
    expect(done!.ok).toBe(false);
    expect(done!.exitCode).toBe(42);
  });

  test("emits session start event", async () => {
    const evs = await collect(runScript({ prompt: "echo x", cwd: "/tmp" }));
    expect(evs[0]).toEqual({ type: "session", status: "running" });
  });

  test("reports stderr as message when stdout is empty", async () => {
    const evs = await collect(runScript({ prompt: "echo error-msg >&2", cwd: "/tmp" }));
    const msgs = evs.filter((e) => e.type === "message");
    expect(msgs.some((m) => "text" in m && (m as { text: string }).text.includes("error-msg"))).toBe(true);
  });
});
