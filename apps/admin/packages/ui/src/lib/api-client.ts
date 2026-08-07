/**
 * Framework-agnostic API client for Chaosplus services.
 *
 * Mirrors the legacy admin's `boot/axios` + `getQueryString`: it speaks the backend's
 * "simple" query syntax (`filter[field]=op:value`, `sort[field]=ASC`, `offset`, `limit`),
 * which the Go `rsql.ParseSimple` accepts, and unwraps the `{code,message,data,meta}`
 * envelope. The list adapter accepts BOTH the framework-native `{data:[…],meta}` shape
 * and the legacy `{data:{list,total,offset}}` shape, so old and new specs stay compatible.
 *
 * It also speaks full RSQL when asked (`ListQuery.syntax = 'rsql'`): see {@link ./rsql}.
 */

import { filterStateToRsql, isFilterValue, isEmptyFilterValue } from "./rsql"

export { rsql, filterStateToRsql, rsqlComparison } from "./rsql"

export type FilterOp =
  | "eq"
  | "ne"
  | "like"
  | "notlike"
  | "gt"
  | "gte"
  | "lt"
  | "lte"
  | "in"
  | "between"

export type SortDir = "ASC" | "DESC"

export interface FilterValue {
  op?: FilterOp
  value: unknown
}

/** Query dialect: `simple` (`filter[field]=op:value`) or `rsql` (`filter=<expr>`). */
export type FilterSyntax = "simple" | "rsql"

export interface ListQuery {
  offset?: number
  limit?: number
  /** field → {op,value} or a bare value (defaults to eq). Empty values are skipped. */
  filter?: Record<string, FilterValue | unknown>
  /** field → ASC | DESC */
  order?: Record<string, SortDir>
  /** Filter dialect emitted on the wire. Defaults to `simple`. */
  syntax?: FilterSyntax
  /**
   * Raw RSQL expression (rsql syntax only). When provided it is sent verbatim as `filter=<expr>`,
   * overriding `filter`; otherwise `filter` is serialized to RSQL. Build with the `rsql` helper.
   */
  rsql?: string
  /** Comma-joined field names to request backend projection (?fields=name,code,…). */
  fields?: string[]
}

export interface ApiEnvelope<T = unknown> {
  code: number
  message: string
  data: T
  meta?: { total?: number; offset?: number; limit?: number }
  field?: string
  errCode?: string
}

export interface ListResult<T> {
  list: T[]
  total: number
  offset: number
}

/** Build the backend query string (no leading `?`), in either the simple or RSQL dialect. */
export function buildListQuery(q: ListQuery): string {
  const parts: string[] = []
  if (q.offset != null)
    parts.push(`offset=${encodeURIComponent(String(q.offset))}`)
  if (q.limit != null)
    parts.push(`limit=${encodeURIComponent(String(q.limit))}`)

  if (q.syntax === "rsql") {
    // RSQL dialect: a single `filter=<expr>` plus `sort=field:DIR,…`. Emit NO `filter[…]`/`sort[…]`
    // keys — the backend prefers the simple dialect whenever any are present (rsql.ParseSimple wins).
    const expr = q.rsql ?? filterStateToRsql(q.filter)
    if (expr) parts.push(`filter=${encodeURIComponent(expr)}`)
    const sorts: string[] = []
    for (const [field, dir] of Object.entries(q.order ?? {})) {
      if (dir) sorts.push(`${field}:${dir}`)
    }
    if (sorts.length) parts.push(`sort=${encodeURIComponent(sorts.join(","))}`)
    if (q.fields?.length) parts.push(`fields=${q.fields.join(",")}`)
    return parts.join("&")
  }

  for (const [field, raw] of Object.entries(q.filter ?? {})) {
    const fv: FilterValue = isFilterValue(raw) ? raw : { value: raw }
    if (isEmptyFilterValue(fv.value)) continue
    const op = fv.op ?? "eq"
    let value = Array.isArray(fv.value) ? fv.value.join(",") : String(fv.value)
    // LIKE/NOT LIKE match exactly unless the caller provides wildcards; wrap for substring search.
    if ((op === "like" || op === "notlike") && !value.includes("%")) {
      value = `%${value}%`
    }
    parts.push(
      `filter[${encodeURIComponent(field)}]=${encodeURIComponent(`${op}:${value}`)}`
    )
  }

  for (const [field, dir] of Object.entries(q.order ?? {})) {
    if (!dir) continue
    parts.push(`sort[${encodeURIComponent(field)}]=${dir}`)
  }
  if (q.fields?.length) parts.push(`fields=${q.fields.join(",")}`)
  return parts.join("&")
}

/** Normalize either envelope list shape into {list,total,offset}. */
export function adaptList<T>(env: ApiEnvelope<unknown>): ListResult<T> {
  const data = env.data
  // Legacy shape: data = { list, total, offset }
  if (
    data &&
    typeof data === "object" &&
    !Array.isArray(data) &&
    "list" in (data as object)
  ) {
    const d = data as { list?: T[]; total?: number; offset?: number }
    return { list: d.list ?? [], total: d.total ?? 0, offset: d.offset ?? 0 }
  }
  // Native shape: data = [] + meta
  const list = Array.isArray(data) ? (data as T[]) : []
  return {
    list,
    total: env.meta?.total ?? list.length,
    offset: env.meta?.offset ?? 0,
  }
}

export class ApiError extends Error {
  readonly code: number
  readonly status: number
  readonly field?: string

  constructor(message: string, code: number, status: number, field?: string) {
    super(message)
    this.name = "ApiError"
    this.code = code
    this.status = status
    this.field = field
  }
}

export interface ApiClientOptions {
  baseUrl: string
  /** Origin for static assets (uploads). Defaults to baseUrl with a trailing /api/* stripped. */
  assetBaseUrl?: string
  /** Returns the bearer token (e.g. from cookie/localStorage), or undefined. */
  getToken?: () => string | undefined
  /** Returns the active UI locale, sent as Accept-Language so the backend can localize
   * server-resolved strings (e.g. menu names). Undefined → header omitted (backend defaults). */
  getLocale?: () => string | undefined
  /** Additional headers evaluated per request, e.g. a portal scope header. */
  getHeaders?: () => Record<string, string | undefined> | undefined
  /** Called with a user-facing message on every failed request. */
  onError?: (message: string, error: ApiError) => void
  /**
   * Called when a request gets HTTP 401. Should attempt to renew the session (e.g. via a
   * refresh token) and resolve true if it succeeded — the request is then retried ONCE with
   * the refreshed token. Implementations MUST de-duplicate concurrent calls (share one
   * in-flight refresh) so simultaneous 401s don't trigger competing token rotations.
   */
  onUnauthorized?: () => Promise<boolean>
}

export interface ApiClient {
  request<T = unknown>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<ApiEnvelope<T>>
  get<T = unknown>(path: string, query?: ListQuery): Promise<ApiEnvelope<T>>
  list<T = unknown>(path: string, query?: ListQuery): Promise<ListResult<T>>
  post<T = unknown>(path: string, body: unknown): Promise<ApiEnvelope<T>>
  put<T = unknown>(path: string, body: unknown): Promise<ApiEnvelope<T>>
  patch<T = unknown>(path: string, body: unknown): Promise<ApiEnvelope<T>>
  delete<T = unknown>(path: string): Promise<ApiEnvelope<T>>
  upload<T = unknown>(
    path: string,
    file: File,
    fields?: Record<string, string>
  ): Promise<ApiEnvelope<T>>
  /** Resolves a server-relative asset path (e.g. /uploads/x.png) to an absolute URL. */
  resolveAsset(path: string): string
}

function joinUrl(base: string, path: string): string {
  const b = base.replace(/\/+$/, "")
  const p = path.startsWith("/") ? path : `/${path}`
  return `${b}${p}`
}

export function createApiClient(opts: ApiClientOptions): ApiClient {
  const assetBase = (
    opts.assetBaseUrl ?? opts.baseUrl.replace(/\/api\/v\d+\/?$/, "")
  ).replace(/\/+$/, "")

  const authHeader = (): Record<string, string> => {
    const token = opts.getToken?.()
    return token ? { Authorization: `Bearer ${token}` } : {}
  }

  const langHeader = (): Record<string, string> => {
    const loc = opts.getLocale?.()
    return loc ? { "Accept-Language": loc } : {}
  }

  const extraHeaders = (): Record<string, string> => {
    const headers = opts.getHeaders?.()
    if (!headers) return {}
    return Object.fromEntries(
      Object.entries(headers).filter(
        (entry): entry is [string, string] =>
          typeof entry[1] === "string" && entry[1] !== ""
      )
    )
  }

  const handle = async <T>(res: Response): Promise<ApiEnvelope<T>> => {
    const text = await res.text()
    // Empty body (e.g. 204 No Content from logout/delete): synthesize a success envelope.
    if (text === "") {
      if (res.ok) return { code: 0, message: "", data: undefined as T }
      const err = new ApiError(
        res.statusText || "Request failed",
        -1,
        res.status
      )
      opts.onError?.(err.message, err)
      throw err
    }
    let env: ApiEnvelope<T>
    try {
      // The backend serializes all int64 snowflake ids as JSON strings (see pkg/types.ID),
      // so no precision rescue is needed at the parse boundary.
      env = JSON.parse(text) as ApiEnvelope<T>
    } catch {
      const err = new ApiError(
        res.statusText || "Request failed",
        -1,
        res.status
      )
      opts.onError?.(err.message, err)
      throw err
    }
    if (!res.ok || (env.code != null && env.code !== 0)) {
      const err = new ApiError(
        env.message || res.statusText,
        env.code ?? -1,
        res.status,
        env.field
      )
      opts.onError?.(err.message, err)
      throw err
    }
    return env
  }

  const fetchOnce = (
    method: string,
    path: string,
    body?: unknown
  ): Promise<Response> =>
    fetch(joinUrl(opts.baseUrl, path), {
      method,
      headers: {
        "Content-Type": "application/json",
        ...authHeader(),
        ...langHeader(),
        ...extraHeaders(),
      },
      // Ids travel as strings end-to-end (backend accepts/returns string snowflake ids).
      body: body == null ? undefined : JSON.stringify(body),
      // Auth is a Bearer token in the Authorization header, not cookies; omitting credentials keeps
      // the request compatible with the backend's wildcard CORS (Allow-Origin: *, Allow-Credentials: false).
      credentials: "omit",
    })

  const request = async <T>(
    method: string,
    path: string,
    body?: unknown
  ): Promise<ApiEnvelope<T>> => {
    let res = await fetchOnce(method, path, body)
    // Access tokens are short-lived; on 401 try to renew the session once and replay the
    // request with the fresh token, so an expired access token doesn't force a re-login.
    if (
      res.status === 401 &&
      opts.onUnauthorized &&
      (await opts.onUnauthorized())
    ) {
      res = await fetchOnce(method, path, body)
    }
    return handle<T>(res)
  }

  const get = <T>(path: string, query?: ListQuery): Promise<ApiEnvelope<T>> => {
    const qs = query ? buildListQuery(query) : ""
    return request<T>("GET", qs ? `${path}?${qs}` : path)
  }

  return {
    request,
    get,
    list: async <T>(path: string, query?: ListQuery): Promise<ListResult<T>> =>
      adaptList<T>(await get(path, query)),
    post: (path, body) => request("POST", path, body),
    put: (path, body) => request("PUT", path, body),
    patch: (path, body) => request("PATCH", path, body),
    delete: (path) => request("DELETE", path),
    upload: async <T>(
      path: string,
      file: File,
      fields?: Record<string, string>
    ): Promise<ApiEnvelope<T>> => {
      const form = new FormData()
      form.append("file", file)
      if (fields) {
        for (const [k, v] of Object.entries(fields)) {
          form.append(k, v)
        }
      }
      const res = await fetch(joinUrl(opts.baseUrl, path), {
        method: "POST",
        headers: { ...authHeader(), ...langHeader(), ...extraHeaders() },
        body: form,
        // Auth is a Bearer token in the Authorization header, not cookies; omitting credentials keeps
        // the request compatible with the backend's wildcard CORS (Allow-Origin: *, Allow-Credentials: false).
        credentials: "omit",
      })
      return handle<T>(res)
    },
    resolveAsset: (path: string): string => {
      if (!path) return ""
      if (/^https?:\/\//.test(path)) return path
      return `${assetBase}${path.startsWith("/") ? path : `/${path}`}`
    },
  }
}
