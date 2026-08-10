import type { AgentEvent, AgentTask } from "../types";
import { runClaude } from "./claude";
import { runCodex } from "./codex";
import { runHttp } from "./http";
import { runMastra } from "./mastra";
import { runMock } from "./mock";
import { runScript } from "./script";

/** A backend is a function that runs one agent task and streams normalized events. */
export type AgentBackend = (task: AgentTask) => AsyncGenerator<AgentEvent>;

/** Registry of available executors (PRD §18 ExecutorType). */
export const BACKENDS: Record<string, AgentBackend> = {
  claude: runClaude,
  codex: runCodex,
  http: runHttp,
  mastra: runMastra,
  mock: runMock,
  script: runScript,
};

export function pickBackend(name: string): AgentBackend {
  const backend = BACKENDS[name];
  if (!backend)
    throw new Error(
      `unknown executor: ${name} (have: ${Object.keys(BACKENDS).join(", ")})`,
    );
  return backend;
}
