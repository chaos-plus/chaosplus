import type { AgentEvent, AgentTask } from "../types";

/**
 * Deterministic backend with no API key. Doubles as the runner's runnable
 * self-check and PRD §18 `mock` ExecutorType.
 */
export async function* runMock(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield {
    type: "message",
    text: `[mock] planning: ${task.prompt.slice(0, 80)}`,
  };
  yield {
    type: "tool",
    name: "read_file",
    input: { path: "src/index.ts" },
    result: "// mock content",
  };
  yield { type: "message", text: "[mock] task complete (no real work done)" };
  yield { type: "done", ok: true, exitCode: 0 };
}
