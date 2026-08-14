import { describe, test, expect, afterEach } from "bun:test";
import { mkdtempSync, writeFileSync, chmodSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { detectBinary, missingBinary } from "./detect";

const savedPath = process.env.PATH;

afterEach(() => {
  process.env.PATH = savedPath;
  delete process.env.CLAUDE_BINARY;
});

/** 创建一个含 name 可执行文件的临时目录，并 prepend 到 PATH。 */
function installExecutable(name: string): string {
  const dir = mkdtempSync(join(tmpdir(), "detect-"));
  const bin = join(dir, process.platform === "win32" ? name + ".exe" : name);
  writeFileSync(bin, "#!/bin/sh\nexit 0\n");
  if (process.platform !== "win32") chmodSync(bin, 0o755);
  process.env.PATH = dir + (savedPath ? (process.platform === "win32" ? ";" : ":") + savedPath : "");
  return bin;
}

describe("detectBinary", () => {
  test("env override 优先", () => {
    process.env.CLAUDE_BINARY = "C:/custom/claude.exe";
    expect(detectBinary(["claude"], "CLAUDE_BINARY")).toBe("C:/custom/claude.exe");
  });

  test("PATH 探测到临时 executable", () => {
    const bin = installExecutable("claude");
    expect(detectBinary(["claude"], "CLAUDE_BINARY")).toBe(bin);
  });

  test("找不到返回 undefined", () => {
    process.env.PATH = join(tmpdir(), "definitely-not-a-bin-dir");
    expect(detectBinary(["claude-nonexistent"], "CLAUDE_BINARY")).toBeUndefined();
  });
});

describe("missingBinary", () => {
  test("格式化清晰报错", () => {
    const e = missingBinary("claude", "CLAUDE_BINARY");
    expect(e.message).toContain("claude");
    expect(e.message).toContain("CLAUDE_BINARY");
  });
});
