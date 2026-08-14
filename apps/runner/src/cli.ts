import { parseArgs } from "node:util";
import { pickBackend } from "./backends";
import type { AgentTask } from "./types";

/**
 * CLI entry: `bun run src/cli.ts --runtime claude --prompt "..." [--cwd path]`.
 * Streams normalized agent events to stdout; exits 0 on done/ok, 1 otherwise.
 */
const { values } = parseArgs({
  args: process.argv.slice(2),
  options: {
    runtime: { type: "string" },
    prompt: { type: "string" },
    cwd: { type: "string" },
    model: { type: "string" },
  },
});

if (!values.runtime) {
  console.error("usage: bun run src/cli.ts --runtime <executor> [--prompt <prompt>] [--cwd <path>] [--model <model>]");
  process.exit(2);
}

const task: AgentTask = {
  prompt: values.prompt ?? "Summarize the current directory.",
  cwd: values.cwd ?? process.cwd(),
  model: values.model,
};

let ok = false;
try {
  for await (const event of pickBackend(values.runtime)(task)) {
    switch (event.type) {
      case "message":
        console.log(event.text);
        break;
      case "tool":
        console.log(`  [tool] ${event.name}`);
        break;
      case "done":
        ok = event.ok;
        console.log(
          `  [done] exit=${event.exitCode}${event.costUsd != null ? ` cost=$ ${event.costUsd.toFixed(4)}` : ""}`,
        );
        break;
      case "error":
        console.error(`  [error] ${event.message}`);
        break;
    }
  }
} catch (err) {
  console.error(`[fatal] ${(err as Error).message}`);
  process.exit(1);
}

process.exit(ok ? 0 : 1);
