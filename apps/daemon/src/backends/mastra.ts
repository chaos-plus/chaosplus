import { Agent } from "@mastra/core/agent";
import type { AgentEvent, AgentTask } from "../types";

/**
 * Mastra executor — uses Mastra's Agent.generate() for LLM API access.
 * Does NOT require a local CLI binary; uses API key from task.
 *
 * This is the "LLM direct" executor path — it calls the model API through
 * Mastra's provider abstraction. The SDK/CLI executors (claude, codex) are
 * the alternative "external toolchain" path.
 */
export async function* runMastra(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };

  try {
    const agent = new Agent({
      name: "executor",
      instructions: task.systemPrompt ?? "Complete the task. Your final output must be valid JSON.",
      model: task.model ?? "anthropic/claude-fable-5",
    });

    const result = await agent.generate(task.prompt, {
      maxSteps: task.maxTurns ?? 25,
    });

    // Stream result text as message events
    if (result.text) {
      for (const line of result.text.split("\n")) {
        const t = line.trim();
        if (t) yield { type: "message", text: t };
      }
    }

    // Report tool usage if available
    const steps = (result as { steps?: Array<{ text?: string; toolCalls?: Array<{ toolName: string; args: unknown }> }> }).steps;
    if (steps) {
      for (const step of steps) {
        if (step.toolCalls) {
          for (const tc of step.toolCalls) {
            yield { type: "tool", name: tc.toolName, input: tc.args };
          }
        }
      }
    }

    // Mastra reports cost on the result object
    const costUsd = typeof (result as { experimental_totalCostUsd?: number }).experimental_totalCostUsd === "number"
      ? (result as { experimental_totalCostUsd: number }).experimental_totalCostUsd
      : undefined;

    yield { type: "done", ok: true, exitCode: 0, costUsd };
  } catch (e) {
    yield { type: "error", message: (e as Error).message };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}
