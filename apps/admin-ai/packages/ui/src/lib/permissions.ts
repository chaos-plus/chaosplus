/**
 * Permission matching, mirroring the backend's `pkg/httpx` wildcard rules so the client
 * gates UI identically to how the server gates routes:
 *   - `*`            grants everything
 *   - exact match    `users:read` grants `users:read`
 *   - `resource:*`   grants `resource:<anything>` (e.g. `users:*` grants `users:read`)
 *
 * Pure logic, no React/Next deps — safe to import anywhere.
 */

/** Does the granted permission set satisfy a single required permission string? */
export function matchPermission(
  granted: readonly string[],
  required: string
): boolean {
  if (!required) return true
  const [reqResource] = required.split(":")
  for (const g of granted) {
    if (g === "*" || g === required) return true
    if (g.endsWith(":*") && g.slice(0, -2) === reqResource) return true
  }
  return false
}

/** Does the granted set satisfy ANY of the required permissions? (empty `required` = allow) */
export function hasAny(
  granted: readonly string[],
  required: readonly string[]
): boolean {
  if (required.length === 0) return true
  return required.some((r) => matchPermission(granted, r))
}
