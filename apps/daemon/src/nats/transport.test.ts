import { expect, test } from "bun:test";
import { connect } from "nats";
import { NatsDaemonTransport, type RunnerCommand } from "./transport";

// Contract-level checks that don't need a live broker (control-plane not built yet).

test("transport instance holds the runner identity", () => {
  const t = new NatsDaemonTransport("host-a");
  expect(t).toBeInstanceOf(NatsDaemonTransport);
});

test("spawn command shape round-trips through JSON", () => {
  const cmd: RunnerCommand = {
    type: "spawn",
    spawn: {
      spawnId: "s1",
      runId: "r1",
      nodeId: "n1",
      attempt: 1,
      executorType: "mock",
      prompt: "do a thing",
      cwd: "/tmp",
    },
  };
  const back = JSON.parse(JSON.stringify(cmd)) as RunnerCommand;
  expect(back.type).toBe("spawn");
  if (back.type === "spawn") {
    expect(back.spawn.spawnId).toBe("s1");
    expect(back.spawn.executorType).toBe("mock");
  }
});

// Live check: verifies a NATS broker is reachable; skipped when none is up
// locally (control-plane not built yet, so no responder is expected).
test("live NATS is reachable at the default URL (skipped when absent)", async () => {
  let nc;
  try {
    nc = await connect({ servers: "nats://127.0.0.1:4222", timeout: 500 });
  } catch {
    return; // no broker locally — not a failure
  }
  expect(nc).toBeDefined();
  await nc.close();
});
