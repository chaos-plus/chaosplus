import { accessSync, constants } from "node:fs";
import { join } from "node:path";

/**
 * Resolve a CLI binary: `envVar` (if set) wins, otherwise search PATH for the
 * first candidate that is an executable. Windows tries `.exe`/`.cmd`/`.bat`.
 * Returns undefined when nothing is found — callers surface a clear error.
 */
export function detectBinary(names: string[], envVar?: string): string | undefined {
  const fromEnv = envVar ? process.env[envVar] : undefined;
  if (fromEnv) return fromEnv;

  const isWin = process.platform === "win32";
  // win32 PATH is ';'-separated (':' is the drive-letter colon) — must not split on it.
  const pathDirs = (process.env.PATH ?? "").split(isWin ? /;+/ : /:+/).filter(Boolean);
  const exts = isWin ? ["", ".exe", ".cmd", ".bat"] : [""];
  // Windows has no POSIX execute bit — executability is by extension, so check
  // existence (X_OK is unreliable there).
  const mode = isWin ? constants.F_OK : constants.X_OK;

  for (const dir of pathDirs) {
    for (const name of names) {
      for (const ext of exts) {
        const candidate = join(dir, name + ext);
        try {
          accessSync(candidate, mode);
          return candidate;
        } catch {
          // not executable here — keep searching
        }
      }
    }
  }
  return undefined;
}

/** Format a missing-CLI error with the fix hint. */
export function missingBinary(name: string, envVar: string): Error {
  return new Error(
    `binary "${name}" not found on PATH — install ${name} or set ${envVar} to its path`,
  );
}
