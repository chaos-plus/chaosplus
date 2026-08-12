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

  // Wire abort signal so kill/stop actually cancels the in-flight LLM call
  // instead of only marking the session stopped while the API call continues.
  const abortController = new AbortController();
  const onAbort = () => abortController.abort();
  task.signal?.addEventListener("abort", onAbort, { once: true });

  try {
    const agent = new Agent({
      id: "executor",
      name: "executor",
      instructions: task.systemPrompt ?? "Complete the task. Your final output must be valid JSON.",
      model: task.model ?? "anthropic/claude-fable-5",
    });

    const result = await agent.generate(task.prompt, {
      maxSteps: task.maxTurns ?? 25,
      abortSignal: abortController.signal,
    });

    // Stream result text as message events
    if (result.text) {
      for (const line of result.text.split("\n")) {
        const t = line.trim();
        if (t) yield { type: "message", text: t };
      }
    }

    // Report tool usage if available
    const steps = (result as unknown as { steps?: Array<{ text?: string; toolCalls?: Array<{ toolName: string; args: unknown }> }> }).steps;
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
    const resultWithCost = result as unknown as { experimental_totalCostUsd?: number };
    const costUsd = typeof resultWithCost.experimental_totalCostUsd === "number"
      ? resultWithCost.experimental_totalCostUsd
      : undefined;

    yield { type: "done", ok: true, exitCode: 0, costUsd };
  } catch (e) {
    if ((e as Error).name === "AbortError") {
      yield { type: "error", message: "aborted" };
    } else {
      yield { type: "error", message: (e as Error).message };
    }
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  } finally {
    task.signal?.removeEventListener("abort", onAbort);
  }
}
