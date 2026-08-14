import type { AgentTask } from "../types";

export interface SpawnCommand extends AgentTask {
  runId: string;
  nodeId: string;
  attempt: number;
  spawnId: string;
  executorType: string;
}

export type RunnerCommand =
  | { type: "spawn"; spawn: SpawnCommand }
  | { type: "kill"; spawnId: string }
  | {
      type: "switch-provider";
      spawnId: string;
      provider: string;
      apiKey?: string;
    }
  | { type: "read-file"; spawnId: string; path: string }
  | { type: "run-cmd"; spawnId: string; cmd: string; timeoutMs: number };

export type RunnerEvent =
  | { type: "heartbeat"; ts: number }
  | { type: "spawn-started"; spawnId: string }
  | { type: "spawn-event"; spawnId: string; event: unknown }
  | {
      type: "spawn-done";
      spawnId: string;
      ok: boolean;
      exitCode: number;
      costUsd?: number;
      error?: string;
      preview?: { type: string; content: string };
    }
  | { type: "spawn-error"; spawnId: string; message: string };

export interface DaemonTransport {
  connect(
    handler: (
      cmd: RunnerCommand,
      reply: (ok: boolean, data?: unknown) => void,
    ) => Promise<void> | void,
  ): Promise<void>;
  register(meta: Record<string, string>): Promise<void>;
  publish(event: RunnerEvent): void;
  close(): void;
}
