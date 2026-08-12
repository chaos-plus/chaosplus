import { expect, test } from "bun:test";
import { BACKENDS, pickBackend } from "./backends";
import { runMock } from "./backends/mock";
import { AgentManager } from "./agents/manager";
import type { AgentEvent, AgentTask } from "./types";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

async function mockCwd(): Promise<string> {
  return mkdtemp(join(tmpdir(), "chaosplus-runner-mock-"));
}

// Test-only backend that stays running until aborted, so we can exercise stop().
async function* runSlow(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "message", text: "[slow] started" };
  await new Promise((resolve) => {
    task.signal?.addEventListener("abort", resolve, { once: true });
  });
  yield { type: "done", ok: false, exitCode: 1 };
}
BACKENDS.slow = runSlow;

// ponytail: one runnable check — the mock backend must stream message→tool→done.

test("mock backend streams message, tool, then done:ok", async () => {
  const events = [];
  const cwd = await mockCwd();
  for await (const event of runMock({ prompt: "hello", cwd })) {
    events.push(event);
  }
  expect(events.map((e) => e.type)).toEqual([
    "message",
    "tool",
    "message",
    "done",
  ]);
  const done = events.at(-1);
  expect(done).toMatchObject({ type: "done", ok: true, exitCode: 0 });
  expect(JSON.parse(await readFile(join(cwd, "output.json"), "utf8"))).toEqual({
    ok: true,
    summary: "mock task completed",
  });
});

test("pickBackend resolves registered runtimes and rejects unknown", () => {
  for (const name of ["claude", "codex", "mock"]) {
    expect(typeof pickBackend(name)).toBe("function");
  }
  expect(() => pickBackend("nope")).toThrow(/unknown executor/);
});

// Service layer: multiple agents run concurrently; run/stop/switch work.

test("manager runs an executor agent to completion and records events", async () => {
  const manager = new AgentManager();
  const a = manager.create({
    kind: "executor",
    runtime: "mock",
    cwd: await mockCwd(),
  });
  await a.run("hello");
  expect(a.status).toBe("completed");
  expect(a.events.some((e) => e.type === "done" && e.ok)).toBe(true);
});

test("manager hosts multiple agents concurrently", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "mock", cwd: await mockCwd() });
  const b = manager.create({ runtime: "mock", cwd: await mockCwd() });
  await Promise.all([a.run("one"), b.run("two")]);
  expect(manager.list()).toHaveLength(2);
  expect(manager.get(a.spec.id)).toBe(a);
});

test("switchProvider updates provider, stops running session, keeps apiKey out of snapshot", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "slow", provider: "anthropic" });
  const run = a.run("start"); // stays running until aborted
  await new Promise((r) => setTimeout(r, 0));
  expect(a.status).toBe("running");

  const switched = manager.switchProvider(a.spec.id, "bedrock", "sk-test");
  expect(switched.spec.provider).toBe("bedrock");
  expect(switched.spec.apiKey).toBe("sk-test");
  await run; // slow backend resolves on abort → run completes as stopped
  expect(a.status).toBe("stopped");
  expect(JSON.stringify(a.snapshot())).not.toContain("sk-test");
});

test("manager remove stops the session", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "mock", cwd: await mockCwd() });
  a.run("x");
  expect(manager.remove(a.spec.id)).toBe(true);
  expect(manager.get(a.spec.id)).toBeUndefined();
  expect(manager.remove(a.spec.id)).toBe(false);
});
