import { expect, test } from "bun:test";
import { pickBackend } from "./backends";
import { AgentManager } from "./agents/manager";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

async function testCwd(): Promise<string> {
  return mkdtemp(join(tmpdir(), "chaosplus-runner-test-"));
}

test("script backend runs a real process and writes its output", async () => {
  const events = [];
  const cwd = await testCwd();
  const script = `printf '{"ok":true,"summary":"script task completed"}' > output.json\nprintf 'done\\n'`;
  for await (const event of pickBackend("script")({ prompt: script, cwd })) {
    events.push(event);
  }
  expect(events.map((e) => e.type)).toEqual([
    "session",
    "message",
    "done",
  ]);
  const done = events.at(-1);
  expect(done).toMatchObject({ type: "done", ok: true, exitCode: 0 });
  expect(JSON.parse(await readFile(join(cwd, "output.json"), "utf8"))).toEqual({
    ok: true,
    summary: "script task completed",
  });
});

test("pickBackend resolves registered runtimes and rejects unknown", () => {
  for (const name of ["claude", "codex", "script"]) {
    expect(typeof pickBackend(name)).toBe("function");
  }
  expect(() => pickBackend("nope")).toThrow(/unknown executor/);
});

// Service layer: multiple agents run concurrently; run/stop/switch work.

test("manager runs an executor agent to completion and records events", async () => {
  const manager = new AgentManager();
  const a = manager.create({
    kind: "executor",
    runtime: "script",
    cwd: await testCwd(),
  });
  await a.run("printf 'done\\n'");
  expect(a.status).toBe("completed");
  expect(a.events.some((e) => e.type === "done" && e.ok)).toBe(true);
});

test("manager hosts multiple agents concurrently", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "script", cwd: await testCwd() });
  const b = manager.create({ runtime: "script", cwd: await testCwd() });
  await Promise.all([a.run("printf 'one\\n'"), b.run("printf 'two\\n'")]);
  expect(manager.list()).toHaveLength(2);
  expect(manager.get(a.spec.id)).toBe(a);
});

test("switchProvider updates provider, stops running session, keeps apiKey out of snapshot", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "script", provider: "anthropic", cwd: await testCwd() });
  const run = a.run("sleep 10");
  await new Promise((r) => setTimeout(r, 0));
  expect(a.status).toBe("running");

  const switched = manager.switchProvider(a.spec.id, "bedrock", "sk-test");
  expect(switched.spec.provider).toBe("bedrock");
  expect(switched.spec.apiKey).toBe("sk-test");
  await run;
  expect(a.status).toBe("stopped");
  expect(JSON.stringify(a.snapshot())).not.toContain("sk-test");
});

test("manager remove stops the session", async () => {
  const manager = new AgentManager();
  const a = manager.create({ runtime: "script", cwd: await testCwd() });
  const run = a.run("sleep 10");
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(manager.remove(a.spec.id)).toBe(true);
  await run;
  expect(manager.get(a.spec.id)).toBeUndefined();
  expect(manager.remove(a.spec.id)).toBe(false);
});
