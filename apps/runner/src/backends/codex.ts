import { Codex } from "@openai/codex-sdk";
import type { AgentEvent, AgentTask } from "../types";

/**
 * Map one codex-sdk stream event to AgentEvents. `end` marks a terminal event
 * (turn completed/failed) so the caller stops consuming. Pure and testable.
 */
export function codexEvents(event: {
  type: string;
  item?: { type: string; text?: string; command?: unknown };
  error?: { message: string };
  message?: string;
}): { events: AgentEvent[]; end: boolean } {
  switch (event.type) {
    case "item.completed":
      if (event.item?.type === "agent_message" && event.item.text) {
        return { events: [{ type: "message", text: event.item.text }], end: false };
      }
      if (event.item?.type === "command_execution") {
        return {
          events: [{ type: "tool", name: "command_execution", result: event.item.command }],
          end: false,
        };
      }
      return { events: [], end: false };
    case "turn.completed":
      return { events: [{ type: "done", ok: true, exitCode: 0 }], end: true };
    case "turn.failed":
      return {
        events: [
          { type: "error", message: event.error?.message ?? "turn failed" },
          { type: "done", ok: false, exitCode: 1 },
        ],
        end: true,
      };
    case "error":
      return {
        events: [
          { type: "error", message: event.message ?? "error" },
          { type: "done", ok: false, exitCode: 1 },
        ],
        end: true,
      };
    default:
      return { events: [], end: false };
  }
}

/**
 * codex backend: runs one turn via @openai/codex-sdk `Thread.runStreamed()`.
 * Streams agent messages + command executions and maps turn completion to `done`.
 * Auth/key come per-task; provider = 'openai' (default) or an OpenAI-compatible
 * baseUrl (local proxy). TS SDK has no human-in-the-loop API — approvalPolicy is
 * the knob.
 */
export async function* runCodex(task: AgentTask): AsyncGenerator<AgentEvent> {
  const codex = new Codex({
    apiKey: task.apiKey ?? task.env?.CODEX_API_KEY ?? process.env.CODEX_API_KEY,
    baseUrl:
      task.provider && task.provider !== "openai" ? task.provider : undefined,
  });
  const thread = codex.startThread({
    workingDirectory: task.cwd,
    model: task.model,
    sandboxMode: task.sandboxMode ?? "workspace-write",
    approvalPolicy: "never",
  });

  const { events } = await thread.runStreamed(task.prompt, {
    signal: task.signal,
  });
  for await (const event of events) {
    const { events: mapped, end } = codexEvents(event as Parameters<typeof codexEvents>[0]);
    for (const ev of mapped) yield ev;
    if (end) return;
  }
}
