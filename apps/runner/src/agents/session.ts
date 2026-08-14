import { pickBackend } from "../backends";
import type { AgentEvent, AgentSpec, SessionStatus } from "../types";

/** One hosted agent: spec + live run state. Multiple agents run concurrently. */
export class AgentSession {
  spec: AgentSpec;
  status: SessionStatus = "idle";
  /** Retained event log for replay / auditing. */
  events: AgentEvent[] = [];
  private listeners = new Set<(e: AgentEvent) => void>();
  private abort?: AbortController;

  constructor(spec: AgentSpec) {
    this.spec = spec;
  }

  subscribe(fn: (e: AgentEvent) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  private emit(e: AgentEvent) {
    this.events.push(e);
    for (const fn of this.listeners) fn(e);
  }

  /** Run one task on this agent's bound runtime. Rejects if already running. */
  async run(prompt: string): Promise<void> {
    if (this.status === "running") throw new Error(`agent ${this.spec.id} is already running`);
    this.abort = new AbortController();
    this.setStatus("running");
    try {
      const { runtime } = this.spec;
      for await (const e of pickBackend(runtime)({ ...this.spec, prompt, signal: this.abort.signal })) {
        if (this.abort.signal.aborted) break;
        this.emit(e);
      }
      this.setStatus(this.abort.signal.aborted ? "stopped" : "completed");
    } catch (err) {
      this.emit({ type: "error", message: (err as Error).message });
      this.setStatus("failed");
    }
  }

  /** Stop the current run. Provider switch = update spec + stop + re-run. */
  stop(): void {
    this.abort?.abort();
    if (this.status === "running") this.setStatus("stopped");
  }

  private setStatus(status: SessionStatus) {
    this.status = status;
    this.emit({ type: "session", status });
  }

  snapshot() {
    const { apiKey: _apiKey, env: _env, id, kind, runtime, ...rest } = this.spec;
    return { id, kind, runtime, status: this.status, ...rest };
  }
}
