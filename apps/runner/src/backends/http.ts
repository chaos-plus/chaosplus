import type { AgentEvent, AgentTask } from "../types";

interface HttpConfig {
  method?: string;
  url: string;
  headers?: Record<string, string>;
  body?: unknown;
}

/**
 * HTTP executor — calls external APIs. The task prompt is JSON:
 * `{ method, url, headers?, body? }`. Response body is reported as
 * a message event. Exits with { ok: resp.ok }.
 */
export async function* runHttp(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };

  let cfg: HttpConfig;
  try {
    cfg = JSON.parse(task.prompt) as HttpConfig;
  } catch {
    yield { type: "error", message: "HTTP executor requires JSON input: { method, url, headers?, body? }" };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
    return;
  }

  try {
    const resp = await fetch(cfg.url, {
      method: cfg.method ?? "GET",
      headers: { "Content-Type": "application/json", ...cfg.headers },
      body: cfg.body ? JSON.stringify(cfg.body) : undefined,
      signal: task.signal,
    });

    // Stream response body line-by-line if it's text
    const text = await resp.text();
    for (const line of text.split("\n")) {
      const t = line.trim();
      if (t) yield { type: "message", text: t };
    }
    yield { type: "done", ok: resp.ok, exitCode: resp.ok ? 0 : 1, costUsd: 0 };
  } catch (e) {
    // AbortError = cancelled — report as error, not crash
    if ((e as Error).name === "AbortError") {
      yield { type: "error", message: "HTTP request aborted" };
    } else {
      yield { type: "error", message: (e as Error).message };
    }
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}
