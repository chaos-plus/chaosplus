import { describe, test, expect } from "bun:test";
import type { ServerWebSocket } from "bun";
import { WsDaemonTransport } from "./client";
import type { RunnerCommand } from "../nats/transport";

/** 伪控制面:Bun.serve 起一个 WS server,记录收到的消息,可主动下发 cmd。 */
interface FakeServer {
  server: ReturnType<typeof Bun.serve>;
  received: any[];
  waitOpen(timeout?: number): Promise<void>;
  waitFor(pred: (m: any) => boolean, timeout?: number): Promise<any>;
  send(m: unknown): void;
}

function startFakeServer(): FakeServer {
  const received: any[] = [];
  let ws: ServerWebSocket | null = null;
  const waiters: Array<() => void> = [];
  const server = Bun.serve({
    port: 0,
    fetch(req, srv) {
      if (srv.upgrade(req)) return undefined;
      return new Response("upgrade required", { status: 426 });
    },
    websocket: {
      open(w) {
        ws = w;
        for (const r of waiters.splice(0)) r();
      },
      message(_w, raw) {
        received.push(JSON.parse(String(raw)));
      },
      close() {},
    },
  });
  return {
    server,
    received,
    waitOpen: (timeout = 2000) =>
      new Promise((res, rej) => {
        if (ws) return res();
        const t = setTimeout(() => rej(new Error("server open timeout")), timeout);
        waiters.push(() => {
          clearTimeout(t);
          res();
        });
      }),
    send: (m) => {
      if (!ws) throw new Error("no ws yet");
      ws.send(JSON.stringify(m));
    },
    waitFor: (pred, timeout = 2000) =>
      new Promise((res, rej) => {
        const start = Date.now();
        const check = () => {
          const hit = received.find(pred);
          if (hit) return res(hit);
          if (Date.now() > start + timeout) return rej(new Error("timeout waiting for message"));
          setTimeout(check, 10);
        };
        check();
      }),
  };
}

const spawnCmd: RunnerCommand = {
  type: "spawn",
  spawn: { runId: "r", nodeId: "n", attempt: 1, spawnId: "s1", executorType: "mock", prompt: "hi", cwd: "/tmp" },
};

describe("WsDaemonTransport", () => {
  test("connect: 收到 spawn cmd 调 handler,reply 带回 reqId", async () => {
    const fake = startFakeServer();
    const t = new WsDaemonTransport(`http://127.0.0.1:${fake.server.port}`, "tok-1", "box-1");
    const gotCmd = new Promise<RunnerCommand>((res) => {
      void t.connect((cmd, reply) => {
        res(cmd);
        reply(true, { agentId: "a1" });
      });
    });
    await fake.waitOpen();
    fake.send({ type: "cmd", reqId: 7, cmd: spawnCmd });
    const cmd = await gotCmd;
    expect(cmd.type).toBe("spawn");
    const reply = await fake.waitFor((m) => m.type === "reply" && m.reqId === 7);
    expect(reply.ok).toBe(true);
    expect(reply.data.agentId).toBe("a1");
  });

  test("publish: 发送 {type:event, event}", async () => {
    const fake = startFakeServer();
    const t = new WsDaemonTransport(`http://127.0.0.1:${fake.server.port}`, "tok-1", "box-1");
    await t.connect(() => {});
    await fake.waitOpen();
    t.publish({ type: "heartbeat", ts: 123 });
    const ev = await fake.waitFor((m) => m.type === "event");
    expect(ev.event).toEqual({ type: "heartbeat", ts: 123 });
  });

  test("register: 发送 {type:register, meta}", async () => {
    const fake = startFakeServer();
    const t = new WsDaemonTransport(`http://127.0.0.1:${fake.server.port}`, "tok-1", "box-1");
    await t.connect(() => {});
    await fake.waitOpen();
    await t.register({ runtime: "bun", pid: "1" });
    const m = await fake.waitFor((x) => x.type === "register");
    expect(m.meta.runtime).toBe("bun");
  });

  test("ping: 回复 pong", async () => {
    const fake = startFakeServer();
    const t = new WsDaemonTransport(`http://127.0.0.1:${fake.server.port}`, "tok-1", "box-1");
    await t.connect(() => {});
    await fake.waitOpen();
    fake.send({ type: "ping" });
    const m = await fake.waitFor((x) => x.type === "pong");
    expect(m.type).toBe("pong");
  });
});
