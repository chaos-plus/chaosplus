import { randomUUID } from "node:crypto";
import { pickBackend } from "../backends";
import type { AgentKind, ExecutorType } from "../types";
import { AgentSession } from "./session";

export interface CreateAgentInput {
  name?: string;
  kind?: AgentKind;
  runtime: ExecutorType;
  cwd?: string;
  systemPrompt?: string;
  model?: string;
  provider?: string;
  apiKey?: string;
  env?: Record<string, string>;
  allowedTools?: string[];
  maxTurns?: number;
  permissionMode?: "default" | "acceptEdits" | "bypassPermissions" | "plan";
  sandboxMode?: "read-only" | "workspace-write" | "danger-full-access";
}

/** Registry of hosted agents — the runner's core service state. */
export class AgentManager {
  private sessions = new Map<string, AgentSession>();

  list(): AgentSession[] {
    return [...this.sessions.values()];
  }

  get(id: string): AgentSession | undefined {
    return this.sessions.get(id);
  }

  create(input: CreateAgentInput): AgentSession {
    const runtime = input.runtime;
    pickBackend(runtime); // validate at the boundary, not at first run
    const session = new AgentSession({
      id: randomUUID(),
      name: input.name,
      kind: input.kind ?? "executor",
      runtime,
      cwd: input.cwd ?? process.cwd(),
      systemPrompt: input.systemPrompt,
      model: input.model,
      provider: input.provider,
      apiKey: input.apiKey,
      env: input.env,
      allowedTools: input.allowedTools,
      maxTurns: input.maxTurns,
      permissionMode: input.permissionMode,
      sandboxMode: input.sandboxMode,
    });
    this.sessions.set(session.spec.id, session);
    return session;
  }

  remove(id: string): boolean {
    const s = this.sessions.get(id);
    if (!s) return false;
    s.stop();
    this.sessions.delete(id);
    return true;
  }

  /** cc switch: update provider (and optional key), stop the running session. */
  switchProvider(id: string, provider: string, apiKey?: string): AgentSession {
    const s = this.sessions.get(id);
    if (!s) throw new Error(`agent not found: ${id}`);
    s.spec = { ...s.spec, provider, ...(apiKey !== undefined ? { apiKey } : {}) };
    s.stop();
    return s;
  }
}
