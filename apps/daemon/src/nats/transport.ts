import { connect, type Msg, type NatsConnection, type Subscription, type SubscriptionOptions } from "nats";
import type { AgentTask } from "../types";

/**
 * NATS transport for the daemon ↔ Go control-plane link (PRD §17.1/C3, F.1).
 * Subject scheme:
 *   control→runner command (request/reply): chaos.runner.{runnerId}.cmd
 *   runner→control events (publish):        chaos.runner.{runnerId}.evt
 *   register (request/reply):               chaos.runner.register
 * Control-plane is not built yet — this is the client side of the contract.
 */

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
  | { type: "switch-provider"; spawnId: string; provider: string; apiKey?: string }
  | { type: "read-file"; spawnId: string; path: string }
  | { type: "run-cmd"; spawnId: string; cmd: string; timeoutMs: number };

export type RunnerEvent =
  | { type: "heartbeat"; ts: number }
  | { type: "spawn-started"; spawnId: string }
  | { type: "spawn-event"; spawnId: string; event: unknown }
  | { type: "spawn-done"; spawnId: string; ok: boolean; exitCode: number }
  | { type: "spawn-error"; spawnId: string; message: string };

const cmdSubject = (runnerId: string) => `chaos.runner.${runnerId}.cmd`;
const evtSubject = (runnerId: string) => `chaos.runner.${runnerId}.evt`;
const REGISTER_SUBJECT = "chaos.runner.register";

export class NatsDaemonTransport {
  private nc?: NatsConnection;
  private subs: Subscription[] = [];
  private seq = 0;

  constructor(private runnerId: string, private url = "nats://127.0.0.1:4222") {}

  /** Connect and subscribe to the daemon's command subject. */
  async connect(handler: (cmd: RunnerCommand, reply: (ok: boolean, data?: unknown) => void) => Promise<void> | void): Promise<void> {
    this.nc = await connect({ servers: this.url, name: this.runnerId });
    const opts: SubscriptionOptions = { callback: (err, msg) => void this.dispatch(err, msg, handler) };
    this.subs.push(this.nc.subscribe(cmdSubject(this.runnerId), opts));
  }

  /** Announce this runner to the control-plane (request/reply). Non-fatal if absent. */
  async register(meta: Record<string, string>): Promise<void> {
    if (!this.nc) throw new Error("not connected");
    try {
      await this.nc.request(REGISTER_SUBJECT, JSON.stringify({ runnerId: this.runnerId, meta }), { timeout: 2000 });
    } catch {
      // control-plane not up yet — registration retried by reconnect later
    }
  }

  publish(event: RunnerEvent): void {
    if (!this.nc) return;
    this.nc.publish(evtSubject(this.runnerId), JSON.stringify({ seq: ++this.seq, ...event }));
  }

  close(): void {
    for (const s of this.subs) s.unsubscribe();
    void this.nc?.close();
  }

  private async dispatch(
    err: Error | null,
    msg: Msg,
    handler: (cmd: RunnerCommand, reply: (ok: boolean, data?: unknown) => void) => Promise<void> | void,
  ): Promise<void> {
    if (err || !msg.reply) return;
    try {
      const cmd = JSON.parse(msg.data instanceof Uint8Array ? new TextDecoder().decode(msg.data) : msg.data) as RunnerCommand;
      await handler(cmd, (ok, data) => msg.respond(JSON.stringify({ ok, data })));
    } catch (e) {
      msg.respond(JSON.stringify({ ok: false, error: (e as Error).message }));
    }
  }
}
