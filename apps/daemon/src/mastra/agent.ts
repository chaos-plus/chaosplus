import { Agent } from "@mastra/core/agent";
import { createTool } from "@mastra/core/tools";
import { anthropic } from "@ai-sdk/anthropic";
import { z } from "zod";
import { pickBackend } from "../backends";

/**
 * Mastra integration (agent basics): a Mastra Agent whose only tool dispatches
 * to the daemon's backends. This is the thin bridge between the Mastra agent
 * framework and the executor runtimes — higher-level agents/tools build on it.
 */

const runExecutor = createTool({
  id: "run-executor",
  description:
    "Run one coding-agent session on a backend (claude/codex/mock) and return its output.",
  inputSchema: z.object({
    runtime: z.enum(["claude", "codex", "mock"]),
    prompt: z.string(),
    cwd: z.string(),
  }),
  execute: async ({ runtime, prompt, cwd }) => {
    const chunks: string[] = [];
    for await (const event of pickBackend(runtime)({ prompt, cwd })) {
      if (event.type === "message") chunks.push(event.text);
    }
    return { output: chunks.join("\n") };
  },
});

export interface DaemonAgentOptions {
  model?: ReturnType<typeof anthropic>;
  /** Initial system prompt / role constitution. */
  instructions?: string | string[];
}

export function createDaemonAgent(opts: DaemonAgentOptions = {}) {
  return new Agent({
    id: "chaosplus-daemon",
    name: "chaos.plus daemon",
    instructions:
      opts.instructions ??
      "You are the chaos.plus execution daemon. Use the run-executor tool to run coding-agent tasks on claude, codex, or mock backends.",
    model: opts.model ?? anthropic("claude-sonnet-4-6"),
    tools: { runExecutor },
  });
}
