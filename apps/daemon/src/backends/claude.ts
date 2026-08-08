import { query } from "@anthropic-ai/claude-agent-sdk";
import type { AgentEvent, AgentTask } from "../types";
import { detectBinary, missingBinary } from "./detect";

/** Map provider name → the SDK's cloud-switch env flag (Bedrock/Vertex/Foundry). */
const PROVIDER_ENV: Record<string, string> = {
  bedrock: "CLAUDE_CODE_USE_BEDROCK",
  vertex: "CLAUDE_CODE_USE_VERTEX",
  foundry: "CLAUDE_CODE_USE_FOUNDRY",
};

/** SDK fails to spawn on Windows when the exe path has backslashes — normalize. */
function exePath(p?: string): string | undefined {
  return p ? p.replace(/\\/g, "/") : undefined;
}

/**
 * claude backend: runs one session via @anthropic-ai/claude-agent-sdk `query()`.
 * Streams text + tool_use blocks and a final `done` mapped from the SDK result.
 * Auth/key/provider come per-task (apiKey + provider env), not CLI startup env.
 * Uses the installed `claude` CLI so the user's cc-switch auth/profile applies
 * (the SDK's bundled binary doesn't read user config).
 */
export async function* runClaude(task: AgentTask): AsyncGenerator<AgentEvent> {
  const providerEnv = PROVIDER_ENV[task.provider ?? "anthropic"];
  // Auto-detect the installed claude CLI (cc-switch profile applies); a missing
  // binary fails fast with a clear message instead of an obscure SDK spawn error.
  const claudeBin = task.env?.CLAUDE_BINARY ?? detectBinary(["claude"], "CLAUDE_BINARY");
  if (!claudeBin) throw missingBinary("claude", "CLAUDE_BINARY");
  const gen = query({
    prompt: task.systemPrompt
      ? `${task.systemPrompt}\n\n${task.prompt}`
      : task.prompt,
    options: {
      cwd: task.cwd,
      model: task.model,
      allowedTools: task.allowedTools,
      maxTurns: task.maxTurns,
      permissionMode: task.permissionMode ?? "acceptEdits",
      pathToClaudeCodeExecutable: exePath(claudeBin),
      env: {
        ...process.env,
        ...task.env,
        ...(task.apiKey ? { ANTHROPIC_API_KEY: task.apiKey } : {}),
        ...(providerEnv ? { [providerEnv]: "1" } : {}),
      },
    },
  });

  // Abort: interrupt the running query so the subprocess stops, not just the loop.
  // The run may already have ended (transport closed) when stop() fires — that
  // rejects the interrupt promise, so swallow it (else unhandled rejection crashes bun).
  task.signal?.addEventListener(
    "abort",
    () => {
      void gen.interrupt().catch(() => {});
    },
    { once: true },
  );

  for await (const message of gen) {
    if (message.type === "assistant" && message.message?.content) {
      for (const block of message.message.content) {
        switch (block.type) {
          case "text":
            yield { type: "message", text: block.text };
            break;
          case "tool_use":
            yield { type: "tool", name: block.name, input: block.input };
            break;
        }
      }
    } else if (message.type === "result") {
      const ok = message.subtype === "success";
      yield {
        type: "done",
        ok,
        exitCode: ok ? 0 : 1,
        costUsd: message.total_cost_usd,
      };
    }
  }
}
