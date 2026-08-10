/** Common types for the runner's agent runtimes. */

export type ExecutorType = "claude" | "codex" | "mock" | "mastra" | "script" | "http" | (string & {});

export type AgentKind = "executor" | "digital_human";
export type SessionStatus = "idle" | "running" | "completed" | "failed" | "stopped";

/** What a caller hands to a backend to run one agent session (PRD §18 executor). */
export interface AgentTask {
  prompt: string;
  cwd: string;
  /** Injected as a preamble / role constitution (claude prepends to prompt). */
  systemPrompt?: string;
  model?: string;
  env?: Record<string, string>;
  /** claude: provider = 'anthropic'|'bedrock'|'vertex'|'foundry'; codex: 'openai'|baseUrl. */
  provider?: string;
  /** Per-agent key — daemon serves it, never reads it from CLI startup env. */
  apiKey?: string;
  /** Abort hook: codex passes to runStreamed, claude interrupts the query. */
  signal?: AbortSignal;
  /** claude: auto-approved tool names. */
  allowedTools?: string[];
  /** claude: max tool-use round trips. */
  maxTurns?: number;
  /** claude: 'default' | 'acceptEdits' | 'bypassPermissions' | 'plan'. */
  permissionMode?: "default" | "acceptEdits" | "bypassPermissions" | "plan";
  /** codex: sandbox isolation level. */
  sandboxMode?: "read-only" | "workspace-write" | "danger-full-access";
}

/** Persistent description of one hosted agent (executor or digital-human). */
export interface AgentSpec {
  id: string;
  name?: string;
  kind: AgentKind;
  runtime: ExecutorType;
  cwd: string;
  systemPrompt?: string;
  model?: string;
  provider?: string;
  apiKey?: string;
  allowedTools?: string[];
  maxTurns?: number;
  permissionMode?: "default" | "acceptEdits" | "bypassPermissions" | "plan";
  sandboxMode?: "read-only" | "workspace-write" | "danger-full-access";
}

/** Normalized streaming events emitted by every backend. */
export type AgentEvent =
  | { type: "message"; text: string }
  | { type: "tool"; name: string; input?: unknown; result?: unknown }
  | { type: "done"; ok: boolean; exitCode: number; costUsd?: number }
  | { type: "error"; message: string }
  | { type: "session"; status: SessionStatus };
