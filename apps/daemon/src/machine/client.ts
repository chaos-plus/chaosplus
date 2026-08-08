import type { DaemonTransport, RunnerCommand, RunnerEvent } from "../nats/transport";

export type WsReply = (ok: boolean, data?: unknown) => void;

/**
 * WS daemon transport (PRD §5.3.1): the daemon holds one authenticated
 * WebSocket to the control-plane (`/api/machines/ws?token=..&name=..`), receives
 * commands, replies request/reply style, and pushes events + heartbeats.
 *
 * Protocol (JSON frames):
 *   control→daemon: {type:"cmd", reqId, cmd:<RunnerCommand>}
 *   daemon→control: {type:"reply", reqId, ok, data?}
 *   daemon→control: {type:"event", event:<RunnerEvent>}
 *   control→daemon: {type:"ping"}  →  {type:"pong"}
 */
export class WsDaemonTransport implements DaemonTransport {
  private ws?: WebSocket;

  constructor(
    private host: string,
    private token: string,
    private name: string,
  ) {}

  async connect(
    handler: (cmd: RunnerCommand, reply: WsReply) => Promise<void> | void,
  ): Promise<void> {
    const url =
      this.host.replace(/^http/, "ws") +
      `/api/machines/ws?token=${encodeURIComponent(this.token)}&name=${encodeURIComponent(this.name)}`;
    this.ws = new WebSocket(url);
    await new Promise<void>((res, rej) => {
      const t = setTimeout(() => rej(new Error("ws connect timeout")), 5000);
      this.ws!.onopen = () => {
        clearTimeout(t);
        res();
      };
      this.ws!.onerror = () => {
        clearTimeout(t);
        rej(new Error("ws connect error"));
      };
    });
    this.ws.onmessage = (ev) => void this.dispatch(ev.data, handler);
  }

  async register(meta: Record<string, string>): Promise<void> {
    this.send({ type: "register", meta });
  }

  publish(event: RunnerEvent): void {
    this.send({ type: "event", event });
  }

  close(): void {
    this.ws?.close();
  }

  private send(m: unknown): void {
    if (this.ws?.readyState === WebSocket.OPEN) this.ws.send(JSON.stringify(m));
  }

  private async dispatch(
    raw: unknown,
    handler: (cmd: RunnerCommand, reply: WsReply) => Promise<void> | void,
  ): Promise<void> {
    const msg = JSON.parse(String(raw)) as {
      type: string;
      reqId?: number;
      cmd?: RunnerCommand;
    };
    if (msg.type === "cmd" && msg.cmd) {
      const reply: WsReply = (ok, data) => this.send({ type: "reply", reqId: msg.reqId, ok, data });
      try {
        await handler(msg.cmd, reply);
      } catch (e) {
        reply(false, { error: (e as Error).message });
      }
    } else if (msg.type === "ping") {
      this.send({ type: "pong" });
    }
  }
}
