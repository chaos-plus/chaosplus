import { Agent } from "@mastra/core/agent";
import { createTool } from "@mastra/core/tools";
import { anthropic } from "@ai-sdk/anthropic";
import { z } from "zod";
import { pickBackend } from "../backends";

/**
 * Mastra integration (agent basics): a Mastra Agent whose only tool dispatches
 * to the runner's backends. This is the thin bridge between the Mastra agent
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
  /** Model ID string (e.g. "claude-sonnet-4-5"), or a fully-constructed model.
   * Required unless DAEMON_MODEL is set — no hardcoded model fallback. */
  model?: string | ReturnType<typeof anthropic>;
  /** Initial system prompt / role constitution. */
  instructions?: string | string[];
}

export function createDaemonAgent(opts: DaemonAgentOptions = {}) {
  // Config priority (user rule: remote → env/local config → local CLI config):
  //   1. opts.model   — remote/explicit
  //   2. DAEMON_MODEL — env / local config
  // Mastra is an SDK that calls the API directly (no CLI underneath), so the
  // third tier (cc-switch / local cli config) does not apply here — claude/codex
  // backends, which DO run a local CLI, get that tier via CLAUDE_BINARY etc.
  // No tier configured → fail fast rather than silently pick a model.
  const model =
    typeof opts.model === "string"
      ? anthropic(opts.model)
      : typeof opts.model === "object" && opts.model !== null
        ? opts.model
        : (() => {
            const id = process.env.DAEMON_MODEL;
            if (!id) {
              throw new Error("createDaemonAgent: model not configured — set DAEMON_MODEL or pass opts.model");
            }
            return anthropic(id);
          })();
  return new Agent({
    id: "chaosplus-runner",
    name: "chaos.plus runner",
    instructions:
      opts.instructions ??
      "You are the chaos.plus execution runner. Use the run-executor tool to run coding-agent tasks on claude, codex, or mock backends.",
    model,
    tools: { runExecutor },
  });
}
