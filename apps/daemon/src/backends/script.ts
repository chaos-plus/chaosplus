import { execFile, type ChildProcess } from "node:child_process";
import type { AgentEvent, AgentTask } from "../types";

/** Resolve shebang to an interpreter + args for the script body.
 *  Returns an optional `stdin` string to pipe into the child process. */
function interpreter(script: string): { cmd: string; args: string[]; stdin?: string } {
  const firstLine = script.split("\n")[0]?.trim();
  if (firstLine?.startsWith("#!")) {
    const parts = firstLine.slice(2).split(/\s+/);
    return { cmd: parts[0], args: [...parts.slice(1), "-c", script] };
  }
  // Only check the body (after shebang) for ESM patterns to avoid
  // false-positives on #!/bin/sh scripts containing "async"/"import".
  const rest = script.split("\n").slice(firstLine?.startsWith("#!") ? 1 : 0).join("\n");
  if (/^\s*import\b.*\bfrom\b/m.test(rest) || /^\s*export\b/m.test(rest)) {
    return { cmd: "bun", args: ["run", "-"], stdin: script };
  }
  if (/\brequire\s*\(/.test(rest) || /\bconsole\.log\b/.test(rest))
    return { cmd: "node", args: ["-e", script] };
  return { cmd: "bash", args: ["-c", script] };
}

/**
 * Script executor — runs shell/python/node/bun scripts in the task
 * workspace. Detects interpreter from shebang; reports stdout/stderr
 * as message events. Exits with { ok: exitCode === 0 }.
 */
export async function* runScript(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };

  const { cmd, args, stdin } = interpreter(task.prompt);
  const env = { ...process.env, ...task.env };

  try {
    const { stdout, stderr, exitCode } = await runWithAbort(cmd, args, stdin, {
      cwd: task.cwd,
      env,
      signal: task.signal,
    });

    if (stdout.trim()) {
      for (const line of stdout.trim().split("\n")) {
        const t = line.trim();
        if (t) yield { type: "message", text: t };
      }
    }
    if (stderr.trim()) {
      // Always emit stderr — don't gate on stdout emptiness
      for (const line of stderr.trim().split("\n")) {
        const t = line.trim();
        if (t) yield { type: "message", text: t };
      }
    }
    yield { type: "done", ok: exitCode === 0, exitCode, costUsd: 0 };
  } catch (e) {
    yield { type: "error", message: (e as Error).message };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}

function runWithAbort(
  cmd: string, args: string[], stdin: string | undefined,
  opts: { cwd: string; env: Record<string, string>; signal?: AbortSignal },
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolve) => {
    const proc: ChildProcess = execFile(cmd, args, {
      cwd: opts.cwd, env: opts.env, maxBuffer: 10 * 1024 * 1024,
    }, (err, stdout, stderr) => {
      const code = err && "code" in (err as object)
        ? (err as { code: number }).code
        : (err ? 1 : 0); // killed/maxBuffer → non-zero
      resolve({ stdout, stderr, exitCode: typeof code === "number" ? code : 1 });
    });

    // Feed script body to stdin when interpreter requires it (bun run -).
    if (stdin && proc.stdin) {
      proc.stdin.write(stdin);
      proc.stdin.end();
    }

    const onAbort = () => { proc.kill("SIGTERM"); };
    opts.signal?.addEventListener("abort", onAbort, { once: true });
    proc.on("close", () => { opts.signal?.removeEventListener("abort", onAbort); });
  });
}
