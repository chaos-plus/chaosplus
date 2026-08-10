import { describe, expect, test } from "bun:test";
import { claudeEvents } from "./claude";
import { codexEvents } from "./codex";

describe("claudeEvents", () => {
  test("maps assistant text blocks to message events", () => {
    const out = claudeEvents({
      type: "assistant",
      message: { content: [{ type: "text", text: "hello" }] },
    });
    expect(out).toEqual([{ type: "message", text: "hello" }]);
  });

  test("maps tool_use blocks to tool events with input", () => {
    const out = claudeEvents({
      type: "assistant",
      message: {
        content: [{ type: "tool_use", name: "Bash", input: { command: "ls" } }],
      },
    });
    expect(out).toEqual([{ type: "tool", name: "Bash", input: { command: "ls" } }]);
  });

  test("maps multiple blocks in order and skips unknown block types", () => {
    const out = claudeEvents({
      type: "assistant",
      message: {
        content: [
          { type: "text", text: "first" },
          { type: "tool_use", name: "Write", input: { path: "a.py" } },
          { type: "thinking", thinking: "ignored" },
        ],
      },
    });
    expect(out.map((e) => e.type)).toEqual(["message", "tool"]);
  });

  test("success result becomes done ok with exit 0 and cost", () => {
    const out = claudeEvents({ type: "result", subtype: "success", total_cost_usd: 0.42 });
    expect(out).toEqual([{ type: "done", ok: true, exitCode: 0, costUsd: 0.42 }]);
  });

  test("non-success result becomes done failed with exit 1", () => {
    const out = claudeEvents({ type: "result", subtype: "error_max_turns" });
    expect(out[0]).toMatchObject({ type: "done", ok: false, exitCode: 1 });
  });

  test("unrelated message types produce nothing", () => {
    expect(claudeEvents({ type: "system" })).toEqual([]);
    expect(claudeEvents({ type: "assistant" })).toEqual([]);
  });
});

describe("codexEvents", () => {
  test("agent_message item becomes a message event", () => {
    const r = codexEvents({ type: "item.completed", item: { type: "agent_message", text: "hi" } });
    expect(r).toEqual({ events: [{ type: "message", text: "hi" }], end: false });
  });

  test("command_execution item becomes a tool event carrying the command", () => {
    const r = codexEvents({
      type: "item.completed",
      item: { type: "command_execution", command: "pytest -q" },
    });
    expect(r.events).toEqual([{ type: "tool", name: "command_execution", result: "pytest -q" }]);
    expect(r.end).toBe(false);
  });

  test("empty agent_message text yields nothing", () => {
    const r = codexEvents({ type: "item.completed", item: { type: "agent_message", text: "" } });
    expect(r.events).toEqual([]);
  });

  test("turn.completed ends the stream with a successful done", () => {
    const r = codexEvents({ type: "turn.completed" });
    expect(r).toEqual({ events: [{ type: "done", ok: true, exitCode: 0 }], end: true });
  });

  test("turn.failed emits error then failed done and ends", () => {
    const r = codexEvents({ type: "turn.failed", error: { message: "boom" } });
    expect(r.events).toEqual([
      { type: "error", message: "boom" },
      { type: "done", ok: false, exitCode: 1 },
    ]);
    expect(r.end).toBe(true);
  });

  test("error event emits error then failed done and ends", () => {
    const r = codexEvents({ type: "error", message: "network down" });
    expect(r.events[0]).toEqual({ type: "error", message: "network down" });
    expect(r.end).toBe(true);
  });

  test("unknown event types are ignored without ending the stream", () => {
    expect(codexEvents({ type: "item.started" })).toEqual({ events: [], end: false });
  });
});
