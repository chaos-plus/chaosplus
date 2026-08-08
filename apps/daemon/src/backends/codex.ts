import { Codex } from "@openai/codex-sdk";
import type { AgentEvent, AgentTask } from "../types";

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
    switch (event.type) {
      case "item.completed":
        if (event.item.type === "agent_message" && event.item.text) {
          yield { type: "message", text: event.item.text };
        } else if (event.item.type === "command_execution") {
          yield {
            type: "tool",
            name: "command_execution",
            result: event.item.command,
          };
        }
        break;
      case "turn.completed":
        yield { type: "done", ok: true, exitCode: 0 };
        return;
      case "turn.failed":
        yield { type: "error", message: event.error.message };
        yield { type: "done", ok: false, exitCode: 1 };
        return;
      case "error":
        yield { type: "error", message: event.message };
        yield { type: "done", ok: false, exitCode: 1 };
        return;
    }
  }
}
