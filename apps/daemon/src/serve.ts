import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { resolve, sep } from "node:path";
import { AgentManager } from "./agents/manager";
import { NatsDaemonTransport, type RunnerCommand, type RunnerEvent } from "./nats/transport";
import { startWeb } from "./web";

/**
 * Daemon entry: connect to NATS, register with the control-plane, and service
 * spawn/kill/switch-provider commands by driving AgentManager sessions. Events
 * stream back to the control-plane on the runner's event subject.
 *
 * Usage: RUNNER_ID=myhost DAEMON_NATS_URL=nats://127.0.0.1:4222 bun run src/serve.ts
 */

const RUNNER_ID = process.env.RUNNER_ID ?? require("node:os").hostname();
const NATS_URL = process.env.DAEMON_NATS_URL ?? "nats://127.0.0.1:4222";

const manager = new AgentManager();
const sessions = new Map<string, string>(); // spawnId -> agent session id
const spawnCwd = new Map<string, string>(); // spawnId -> workspace cwd (for read-file)
const transport = new NatsDaemonTransport(RUNNER_ID, NATS_URL);

async function onCommand(cmd: RunnerCommand, reply: (ok: boolean, data?: unknown) => void): Promise<void> {
  switch (cmd.type) {
    case "spawn": {
      const agent = manager.create({
        kind: "executor",
        runtime: cmd.spawn.executorType,
        cwd: cmd.spawn.cwd,
        systemPrompt: cmd.spawn.systemPrompt,
        model: cmd.spawn.model,
        provider: cmd.spawn.provider,
        apiKey: cmd.spawn.apiKey,
      });
      sessions.set(cmd.spawn.spawnId, agent.spec.id);
      spawnCwd.set(cmd.spawn.spawnId, cmd.spawn.cwd);
      transport.publish({ type: "spawn-started", spawnId: cmd.spawn.spawnId });
      reply(true, { agentId: agent.spec.id });
      // Forward message/tool output as spawn-event so the control-plane can
      // reset its idle timeout on live activity (activity-based spawn timeout).
      agent.subscribe((e) => {
        if (e.type === "message" || e.type === "tool") {
          transport.publish({ type: "spawn-event", spawnId: cmd.spawn.spawnId, event: e });
        }
      });
      void runAndReport(agent.spec.id, cmd.spawn.spawnId, cmd.spawn.prompt);
      return;
    }
    case "read-file": {
      const cwd = spawnCwd.get(cmd.spawnId);
      if (!cwd) {
        reply(false, { error: `no workspace for spawn ${cmd.spawnId}` });
        return;
      }
      const abs = resolve(cwd, cmd.path);
      if (abs !== cwd && !abs.startsWith(cwd + sep)) {
        reply(false, { error: "path escapes workspace" });
        return;
      }
      try {
        const content = await readFile(abs, "utf8");
        reply(true, { content });
      } catch (e) {
        reply(false, { error: (e as Error).message });
      }
      return;
    }
    case "run-cmd": {
      // PRD F.5 'cmd:' validator: run a command template in the spawn's
      // workspace; non-zero exit = node failed. Output is returned to the
      // control-plane for diagnostics.
      const cwd = spawnCwd.get(cmd.spawnId);
      if (!cwd) {
        reply(false, { error: `no workspace for spawn ${cmd.spawnId}` });
        return;
      }
      try {
        const { stdout, stderr, exitCode } = await runCmd(cmd.cmd, cwd, cmd.timeoutMs || 60000);
        reply(true, { exitCode, stdout, stderr });
      } catch (e) {
        reply(false, { error: (e as Error).message });
      }
      return;
    }
    case "kill": {
      const id = sessions.get(cmd.spawnId);
      if (id) manager.get(id)?.stop();
      reply(true);
      return;
    }
    case "switch-provider": {
      const id = sessions.get(cmd.spawnId);
      if (!id) return reply(false, { error: `no session for spawn ${cmd.spawnId}` });
      manager.switchProvider(id, cmd.provider, cmd.apiKey);
      reply(true);
      return;
    }
  }
}

async function runAndReport(agentId: string, spawnId: string, prompt: string): Promise<void> {
  const agent = manager.get(agentId);
  if (!agent) return;
  try {
    await agent.run(prompt);
  } catch (e) {
    transport.publish({ type: "spawn-error", spawnId, message: (e as Error).message });
    return;
  }
  const done = agent.events.find((ev) => ev.type === "done");
  const err = agent.events.find((ev) => ev.type === "error");
  transport.publish({
    type: "spawn-done",
    spawnId,
    ok: done?.ok ?? false,
    exitCode: done?.exitCode ?? 1,
    ...(done?.ok === false || err ? { error: err && "message" in err ? err.message : "no done event" } : {}),
  });
  sessions.delete(spawnId);
  manager.remove(agentId);
}

async function main(): Promise<void> {
  await transport.connect(onCommand);
  await transport.register({ runtime: "bun", pid: String(process.pid) });
  console.log(`[daemon] ${RUNNER_ID} connected to ${NATS_URL}, awaiting commands`);

  // Local web UI for managing agents + 1v1 chat (dev/test). Independent of NATS.
  startWeb(manager);

  const heartbeat: RunnerEvent = { type: "heartbeat", ts: Date.now() };
  const hb = setInterval(() => transport.publish(heartbeat), 15000);

  const shutdown = () => {
    clearInterval(hb);
    transport.close();
    process.exit(0);
  };
  process.on("SIGINT", shutdown);
  process.on("SIGTERM", shutdown);
}

/** Run a validator command template in a workspace, killing it on timeout. */
function runCmd(cmd: string, cwd: string, timeoutMs: number): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolvePromise, reject) => {
    const proc = execFile(cmd, { cwd, shell: true, windowsHide: true }, (err, stdout, stderr) => {
      const code = err && typeof (err as { code?: unknown }).code === "number" ? (err as { code: number }).code : 0;
      resolvePromise({ stdout, stderr, exitCode: code });
    });
    const killer = setTimeout(() => proc.kill(), timeoutMs);
    proc.on("close", () => clearTimeout(killer));
  });
}

if (import.meta.main) void main();
