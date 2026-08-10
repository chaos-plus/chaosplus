import { execFile, type ChildProcess } from "node:child_process";
import type { AgentEvent, AgentTask } from "../types";

/** Resolve shebang to an interpreter + args for the script body. */
function interpreter(script: string): { cmd: string; args: string[] } {
  const firstLine = script.split("\n")[0]?.trim();
  if (firstLine?.startsWith("#!")) {
    const parts = firstLine.slice(2).split(/\s+/);
    return { cmd: parts[0], args: [...parts.slice(1), "-c", script] };
  }
  if (/\bimport\b.*\bfrom\b/.test(script) || /\basync\b/.test(script))
    return { cmd: "bun", args: ["run", "-"] };
  if (/\brequire\s*\(/.test(script) || /\bconsole\.log\b/.test(script))
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

  const { cmd, args } = interpreter(task.prompt);
  const env = { ...process.env, ...task.env };

  try {
    const { stdout, stderr, exitCode } = await runWithAbort(cmd, args, {
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
    if (stderr.trim() && !stdout.trim()) {
      yield { type: "message", text: stderr.trim() };
    }
    yield { type: "done", ok: exitCode === 0, exitCode, costUsd: 0 };
  } catch (e) {
    yield { type: "error", message: (e as Error).message };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}

function runWithAbort(
  cmd: string, args: string[], opts: { cwd: string; env: Record<string, string>; signal?: AbortSignal },
): Promise<{ stdout: string; stderr: string; exitCode: number }> {
  return new Promise((resolve, reject) => {
    // execFile with shell:true: cmd is the shell program, args are shell arguments.
    // When interpreter returns e.g. { cmd: "bash", args: ["-c", script] },
    // this runs: bash -c '<script>'
    const proc: ChildProcess = execFile(cmd, args, {
      cwd: opts.cwd, env: opts.env, maxBuffer: 10 * 1024 * 1024,
    }, (err, stdout, stderr) => {
      const code = err && "code" in (err as object) ? (err as { code: number }).code : 0;
      resolve({ stdout, stderr, exitCode: typeof code === "number" ? code : 1 });
    });

    const onAbort = () => { proc.kill("SIGTERM"); };
    opts.signal?.addEventListener("abort", onAbort, { once: true });
    proc.on("close", () => { opts.signal?.removeEventListener("abort", onAbort); });
  });
}
