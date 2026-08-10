import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { detectBinary } from "./backends/detect";
import { AgentManager } from "./agents/manager";
import { WsDaemonTransport } from "./machine/client";
import type { RunnerCommand, RunnerEvent } from "./nats/transport";
import { startWeb } from "./web";

/**
 * Daemon entry (PRD §5.3.1): connect to the control-plane over one authenticated
 * WebSocket, register as a machine runner, and service spawn/kill/switch-provider
 * commands by driving AgentManager sessions. Events + heartbeats stream back over
 * the same socket. NATS is not involved.
 *
 * Usage: bun run src/serve.ts --server http://127.0.0.1:8081 --token <token> [--name <name>]
 */

function arg(name: string): string | undefined {
  const i = process.argv.indexOf(name);
  return i >= 0 ? process.argv[i + 1] : undefined;
}

const SERVER = arg("--server") ?? "http://127.0.0.1:8081";
const TOKEN = arg("--token");
const NAME = arg("--name") ?? require("node:os").hostname();
if (!TOKEN) {
  console.error("usage: bun run src/serve.ts --server <url> --token <token> [--name <name>]");
  process.exit(1);
}

const manager = new AgentManager();
const sessions = new Map<string, string>(); // spawnId -> agent session id
const spawnCwd = new Map<string, string>(); // spawnId -> workspace cwd (for read-file)
const transport = new WsDaemonTransport(SERVER, TOKEN, NAME);

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
      // Windows: cwd arrives with forward slashes but resolve() yields backslashes,
      // so a raw comparison would false-positive "escapes". Normalize both sides.
      const norm = (p: string) => p.replace(/\\/g, "/");
      const base = norm(resolve(cwd));
      const abs = norm(resolve(cwd, cmd.path));
      if (abs !== base && !abs.startsWith(base + "/")) {
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
    try {
      await agent.run(prompt);
    } catch (e) {
      transport.publish({ type: "spawn-error", spawnId, message: (e as Error).message });
      return;
    }
    const done = agent.events.find((ev) => ev.type === "done");
    const err = agent.events.find((ev) => ev.type === "error");
    let preview: { type: string; content: string } | undefined;
    const msgs = agent.events.filter((e) => e.type === "message");
    if (msgs.length > 0) {
      const first = (msgs[0] as { text: string }).text.slice(0, 500);
      preview = { type: "text", content: first };
    }

    transport.publish({
      type: "spawn-done",
      spawnId,
      ok: done?.ok ?? false,
      exitCode: done?.exitCode ?? 1,
      ...(done && "costUsd" in done && done.costUsd != null ? { costUsd: done.costUsd } : {}),
      ...(done?.ok === false || err ? { error: err && "message" in err ? err.message : "no done event" } : {}),
      ...(preview ? { preview } : {}),
    });
  } finally {
    sessions.delete(spawnId);
    spawnCwd.delete(spawnId);
    manager.remove(agentId);
  }
}

async function main(): Promise<void> {
  await transport.connect(onCommand);
  // 上报本机检测到的执行器:控制面据此给"新建数字人"的 runtime 下拉(PRD D.4)。
  const runtimes = [
    detectBinary(["claude"], "CLAUDE_BINARY") ? "claude" : "",
    detectBinary(["codex"], "CODEX_BINARY") ? "codex" : "",
    "mock",
    "mastra",  // always available (pure API, no binary)
    "script", // always available (shell)
    "http",   // always available (fetch)
  ].filter(Boolean);
  await transport.register({
    runtime: "bun",
    pid: String(process.pid),
    name: NAME,
    runtimes: runtimes.join(","),
    os: `${process.platform}/${process.arch}`,
  });
  console.log(`[daemon] detected runtimes: ${runtimes.join(", ")}`);
  console.log(`[daemon] ${NAME} connected to ${SERVER}, awaiting commands`);

  // Local web UI is a dev/test surface for 1v1 agent chat — only when explicitly
  // requested (--web), so a machine runner doesn't squat a port / spawn services.
  if (arg("--web")) startWeb(manager);

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

/** Run a validator command template in a workspace, killing it on timeout.
 * Timeout or maxBuffer overflow → exitCode = 1 (not 0), so the validator
 * correctly fails the node instead of silently passing. */
function runCmd(cmd: string, cwd: string, timeoutMs: number): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolvePromise, _reject) => {
    const proc = execFile(cmd, { cwd, shell: true, windowsHide: true, maxBuffer: 10 * 1024 * 1024 }, (err, stdout, stderr) => {
      let code = 0;
      if (err) {
        code = typeof (err as { code?: unknown }).code === "number"
          ? (err as { code: number }).code
          : 1; // killed, maxBuffer overflow, or spawn failure → non-zero
      }
      resolvePromise({ stdout, stderr, exitCode: code });
    });
    const killer = setTimeout(() => proc.kill(), timeoutMs);
    proc.on("close", () => clearTimeout(killer));
  });
}

if (import.meta.main) void main();
