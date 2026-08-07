export interface Envelope<T> {
  code: number
  message: string
  data: T
  meta?: {
    page?: { offset: number; limit: number; count: number; total: number }
  }
}

export interface Session {
  subject: string
  issuer: string
  preferred_username?: string
  email?: string
  email_verified: boolean
  organization_id?: string
}

export interface BrowserSession {
  id: string
  current: boolean
  created_at: string
  last_seen_at: string
  expires_at: string
  absolute_expires_at: string
  ip_address?: string
  user_agent?: string
}

export interface LoginResult {
  status: "authenticated" | "mfa_required"
  return_url: string
  challenge_id?: string
  expires_at?: string
  methods?: Array<"totp" | "recovery_code">
}

export interface MfaStatus {
  totp_enabled: boolean
  recovery_codes_remaining: number
}

export interface AuthnCapabilities {
  registration: boolean
  password_recovery: boolean
  passkey: boolean
}

export interface MfaEnrollment {
  secret: string
  provisioning_uri: string
  expires_at: string
}

export interface MfaConfirmation {
  recovery_codes: string[]
}

export interface PasskeyOptions {
  challenge_id: string
  expires_at: string
  options: {
    publicKey: Record<string, unknown>
    mediation?: CredentialMediationRequirement
  }
}

export interface Passkey {
  id: string
  name: string
  sign_count: number
  created_at: string
  last_used_at?: string
}

export interface Principal {
  id: string
  login_name: string
  email?: string
  email_verified: boolean
  display_name: string
  status: "active" | "disabled"
  created_at: string
  updated_at: string
}

export interface PrincipalList {
  items: Principal[]
  total: number
}

export interface ServiceAccount {
  id: string
  login_name: string
  display_name: string
  description?: string
  status: "active" | "disabled"
  expires_at?: string
  version: number
  created_at: string
  updated_at: string
}

export interface ServiceAccountList {
  items: ServiceAccount[]
  total: number
}

export interface ServiceAccountCredential {
  id: string
  service_account_id: string
  name: string
  scopes: string[]
  expires_at?: string
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface ServiceAccountCredentialSecret {
  credential: ServiceAccountCredential
  client_secret: string
}

export interface ServiceAccountInput {
  login_name: string
  display_name?: string
  description?: string
  expires_at?: string
}

export interface ServiceAccountUpdate {
  display_name: string
  description?: string
  status: ServiceAccount["status"]
  expires_at?: string
  version: number
}

export interface Invitation {
  id: string
  tenant_id: string
  email: string
  status: "pending" | "accepted" | "revoked" | "expired"
  department_id?: string
  role_ids: string[]
  expires_at: string
  accepted_by?: string
  accepted_at?: string
  revoked_at?: string
  created_at: string
  updated_at: string
}

export interface IssuedInvitation {
  invitation: Invitation
  token: string
}

export interface InvitationAcceptance {
  invitation_id: string
  tenant_id: string
  principal_id: string
  email: string
  already_accepted: boolean
}

export interface Tenant {
  id: string
  slug: string
  name: string
  status: "active" | "suspended" | "deleted"
  version: number
  created_at: string
  updated_at: string
}

export interface TenantInput {
  slug: string
  name: string
}

export interface TenantUpdate {
  name?: string
  status?: "active" | "suspended"
  version: number
}

export interface Member {
  tenant_id: string
  subject: string
  display_name: string
  email?: string
  department_id?: string
  status: "active" | "disabled"
  role_ids?: string[]
  disabled_at?: string
  created_at: string
  updated_at: string
}

export interface Permission {
  resource: string
  verb: string
  code: string
  scope: string
  summary: string
  allowed_relations: Array<"owner" | "editor" | "viewer">
  data_scoped: boolean
  menu: boolean
}

export interface Role {
  id: string
  tenant_id: string
  name: string
  description: string
  created_at: string
  updated_at: string
}

export interface RolePermissionGrant {
  permission_code: string
  condition?: RelationshipCondition
  created_at: string
}

export type AccessRequestStatus =
  "pending" | "approved" | "rejected" | "cancelled" | "revoked" | "expired"

export interface AccessRequest {
  id: string
  tenant_id: string
  requester_id: string
  role_id: string
  role_name: string
  reason: string
  status: AccessRequestStatus
  access_expires_at: string
  request_expires_at: string
  decided_by?: string
  decision_note?: string
  decided_at?: string
  revoked_by?: string
  revoke_reason?: string
  revoked_at?: string
  created_at: string
  updated_at: string
}

export interface RequestableRole {
  id: string
  name: string
  description: string
}

export interface AccessRequestInput {
  role_id: string
  reason: string
  access_expires_at: string
}

export type AccessReviewStatus = "open" | "completed" | "cancelled" | "expired"
export type AccessReviewDecision = "pending" | "keep" | "revoke"

export interface AccessReviewItem {
  id: string
  review_id: string
  principal_id: string
  principal_name: string
  role_id: string
  role_name: string
  grant_type: "permanent" | "temporary"
  grant_id?: string
  grant_created_at: string
  grant_expires_at?: string
  decision: AccessReviewDecision
  decided_by?: string
  decision_note?: string
  decided_at?: string
}

export interface AccessReview {
  id: string
  tenant_id: string
  name: string
  owner_id: string
  status: AccessReviewStatus
  due_at: string
  completed_at?: string
  created_at: string
  updated_at: string
  total: number
  pending: number
  kept: number
  revoked: number
  items: AccessReviewItem[]
}

export interface AccessReviewInput {
  name: string
  due_at: string
}

export interface RoleDirectoryBinding {
  role_id: string
  assignee_type: "group" | "position"
  assignee_id: string
  created_at: string
}

export type DataScope =
  | "all"
  | "self"
  | "department"
  | "department_and_descendants"
  | "selected_departments"

export interface RoleDataScope {
  role_id: string
  scope: DataScope
  department_ids: string[]
  updated_at?: string
}

export interface Entity {
  id: string
  tenant_id: string
  parent_id?: string
  type: string
  name: string
  status: "active" | "disabled"
  metadata: Record<string, unknown>
  created_at: string
  updated_at: string
}

export interface EntityInput {
  parent_id?: string
  type: string
  name: string
  status: Entity["status"]
  metadata: Record<string, unknown>
}

export interface EntityRoleBinding {
  entity_id: string
  role_id: string
  principal_id: string
  effect: "allow" | "deny"
  expires_at?: string
  created_at: string
}

export interface EntityRoleBindingInput {
  effect: EntityRoleBinding["effect"]
  expires_at?: string
}

export type RelationshipSubjectType =
  "principal" | "group" | "position" | "entity"
export type RelationshipRelation = "owner" | "editor" | "viewer"

export type RelationshipConditionExpression =
  | { all: RelationshipConditionExpression[] }
  | { any: RelationshipConditionExpression[] }
  | { not: RelationshipConditionExpression }
  | {
      eq: [
        { context: "auth.acr" | "client.id" | "network.zone" },
        { value: number | string },
      ]
    }
  | {
      neq: [
        { context: "auth.acr" | "client.id" | "network.zone" },
        { value: number | string },
      ]
    }
  | { gt: [{ context: "auth.acr" }, { value: number }] }
  | { gte: [{ context: "auth.acr" }, { value: number }] }
  | { lt: [{ context: "auth.acr" }, { value: number }] }
  | { lte: [{ context: "auth.acr" }, { value: number }] }
  | {
      in: [{ context: "client.id" | "network.zone" }, { value: string[] }]
    }
  | { contains: [{ context: "auth.amr" }, { value: string }] }
  | { between_time: [string, string, string] }

export type RelationshipCondition = {
  version: 1
} & RelationshipConditionExpression

export interface RelationshipInput {
  entity_id?: string
  subject_type: RelationshipSubjectType
  subject_id: string
  subject_relation?: "member" | RelationshipRelation
  relation: RelationshipRelation
  resource_type: string
  resource_id: string
  starts_at?: string
  ends_at?: string
  condition?: RelationshipCondition
}

export interface Relationship extends RelationshipInput {
  created_at: string
}

export interface ResourceRef {
  type: string
  id: string
}

export interface DataConstraint {
  allow_all: boolean
  owner_ids: string[]
  resource_ids: string[]
  department_ids: string[]
  ancestors: ResourceRef[]
  denied_ids: string[]
  revision: number
}

export interface AuthorizationDecisionMatch {
  permission_code: string
  role_id: string
  source_type: string
  source_id: string
  scope_type: string
  scope_id: string
  effect: "allow" | "deny"
  inherited: boolean
  relation?: RelationshipRelation
  path?: RelationshipInput[]
}

export interface AuthorizationExplanation {
  allowed: boolean
  reason:
    | "inactive_membership"
    | "inactive_tenant"
    | "inactive_resource"
    | "explicit_deny"
    | "administrator_scope_denied"
    | "permission_grant"
    | "administrator_grant"
    | "relationship_grant"
    | "no_matching_grant"
  revision: number
  matches: AuthorizationDecisionMatch[]
}

export interface Department {
  id: string
  tenant_id: string
  parent_id?: string
  name: string
  status: "active" | "disabled"
  sort_order: number
  depth: number
  version: number
  created_at: string
  updated_at: string
}

export interface DepartmentInput {
  parent_id?: string
  name: string
  status: "active" | "disabled"
  sort_order: number
}

export interface Position {
  id: string
  tenant_id: string
  code: string
  name: string
  status: "active" | "disabled"
  sort_order: number
  version: number
  created_at: string
  updated_at: string
}

export interface PositionInput {
  code: string
  name: string
  status: "active" | "disabled"
  sort_order: number
}

export interface PositionMember {
  position_id: string
  principal_id: string
  display_name: string
  email?: string
  starts_at?: string
  ends_at?: string
  created_at: string
  updated_at: string
}

export interface MembershipWindowInput {
  starts_at?: string
  ends_at?: string
}

export type PositionMemberInput = MembershipWindowInput

export interface Group {
  id: string
  tenant_id: string
  name: string
  type: "static" | "dynamic"
  membership_rule?: GroupMembershipRule
  description?: string
  status: "active" | "disabled"
  sort_order: number
  version: number
  created_at: string
  updated_at: string
}

export interface GroupInput {
  name: string
  type: Group["type"]
  membership_rule?: GroupMembershipRule
  description: string
  status: "active" | "disabled"
  sort_order: number
}

export type GroupRuleField =
  | "member.subject"
  | "member.email"
  | "member.email_domain"
  | "member.department_id"
  | "member.status"

export interface GroupRuleCondition {
  field: GroupRuleField
  operator: "in" | "not_in"
  values: string[]
}

export interface GroupMembershipRule {
  version: 1
  match: "all" | "any"
  conditions: GroupRuleCondition[]
}

export interface GroupMember {
  group_id: string
  principal_id: string
  display_name: string
  email?: string
  starts_at?: string
  ends_at?: string
  created_at: string
  updated_at: string
}

export type GroupMemberInput = MembershipWindowInput

export interface Menu {
  id: string
  tenant_id: string
  parent_id?: string
  label: string
  route?: string
  icon?: string
  sort_order: number
  permission_code?: string
  status: "active" | "disabled"
  created_at: string
  updated_at: string
}

export interface MenuInput {
  parent_id?: string
  label: string
  route?: string
  icon?: string
  sort_order: number
  permission_code?: string
  status: "active" | "disabled"
}

export interface EffectiveMenu {
  id: string
  label: string
  path?: string
  icon?: string
  sort_order: number
  permission_code: string
  children?: EffectiveMenu[]
}

export type OAuthGrantType =
  "authorization_code" | "refresh_token" | "client_credentials"

export interface OAuthClient {
  id: string
  tenant_id: string
  name: string
  redirect_uris: string[]
  grant_types: OAuthGrantType[]
  scopes: string[]
  public_client: boolean
  status: "active" | "disabled"
}

export interface OAuthClientInput {
  name: string
  redirect_uris: string[]
  grant_types: OAuthGrantType[]
  scopes: string[]
  public_client: boolean
}

export interface OAuthClientCreateResult {
  client: OAuthClient
  client_secret?: string
}

export interface SCIMDirectory {
  id: string
  tenant_id: string
  name: string
  status: "active" | "disabled"
  version: number
  created_at: string
  updated_at: string
}

export interface SCIMDirectoryInput {
  name: string
}

export interface SCIMCredential {
  id: string
  directory_id: string
  name: string
  expires_at?: string
  last_used_at?: string
  revoked_at?: string
  created_at: string
}

export interface SCIMCredentialSecret {
  credential: SCIMCredential
  bearer_token: string
}

export interface AuditEvent {
  id: string
  tenant_id: string
  principal_id?: string
  event_type: string
  target_type?: string
  target_id?: string
  outcome: "success" | "denied" | "failure"
  ip_address?: string
  user_agent?: string
  detail: Record<string, unknown>
  sequence: number
  previous_hash?: string
  event_hash: string
  created_at: string
}

export interface AuditIntegrity {
  tenant_id: string
  valid: boolean
  verified_events: number
  head_sequence: number
  head_hash?: string
  anchor?: AuditAnchorStatus
}

export interface AuditExportDownload {
  blob: Blob
  filename: string
}

export interface AuditAnchor {
  schema: string
  tenant_id: string
  head_sequence: number
  head_hash: string
  anchored_at: string
  previous_anchor_hash?: string
  anchor_hash: string
  signing_key_id?: string
  root_public_key?: string
  root_signature?: string
}

export interface AuditAnchorStatus {
  enabled: boolean
  sequence?: number
  hash?: string
  anchored_at?: string
  signed: boolean
  signature_valid?: boolean
  valid: boolean
}

export interface AuditRetentionPolicy {
  tenant_id: string
  min_days: number
  archive_after_days: number
  updated_at?: string
}

export interface AuditGovernance {
  policy: AuditRetentionPolicy
  integrity: AuditIntegrity
  anchors: AuditAnchor[]
  total_events: number
  archive_ready_events: number
  anchored_events: number
}

const tenantKey = "chaosplus.tenant"
const baseUrl = (import.meta.env.VITE_API_URL ?? "/api").replace(/\/$/, "")
const clientErrorMessages = {
  "en-US": {
    invalid_response: "The service returned an invalid response.",
    clipboard_access_denied:
      "The browser denied clipboard access. Copy the value manually.",
    invalid_audit_export_content_type:
      "The audit export returned an unexpected file type.",
    incomplete_audit_export: "The audit export is incomplete.",
  },
  "zh-CN": {
    invalid_response: "服务返回了无效响应。",
    clipboard_access_denied: "浏览器拒绝访问剪贴板，请手动复制。",
    invalid_audit_export_content_type: "审计导出返回了非预期文件类型。",
    incomplete_audit_export: "审计导出不完整。",
  },
  "ms-MY": {
    invalid_response: "Perkhidmatan mengembalikan respons yang tidak sah.",
    clipboard_access_denied:
      "Pelayar menolak akses papan keratan. Salin nilai secara manual.",
    invalid_audit_export_content_type:
      "Eksport audit mengembalikan jenis fail yang tidak dijangka.",
    incomplete_audit_export: "Eksport audit tidak lengkap.",
  },
}

interface ErrorResponse {
  code?: number | string
  message?: string
  detail?: string
}

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(
    status: number,
    code: string,
    message = clientErrorMessage(code)
  ) {
    super(message)
    this.name = "ApiError"
    this.status = status
    this.code = code
  }
}

export function getTenant(): string {
  return (
    (typeof localStorage === "undefined"
      ? undefined
      : localStorage.getItem(tenantKey)) ??
    import.meta.env.VITE_DEFAULT_TENANT ??
    "platform"
  )
}

export function setTenant(value: string): void {
  localStorage.setItem(tenantKey, value.trim())
  window.dispatchEvent(new Event("tenant-change"))
}

async function requestWithBase<T>(
  apiBase: string,
  tenantID: () => string,
  path: string,
  init: RequestInit = {},
  tenant = true
): Promise<T> {
  return (
    await requestEnvelopeWithBase<T>(apiBase, tenantID, path, init, tenant)
  ).data
}

async function requestEnvelopeWithBase<T>(
  apiBase: string,
  tenantID: () => string,
  path: string,
  init: RequestInit = {},
  tenant = true
): Promise<Envelope<T>> {
  const headers = new Headers(init.headers)
  if (tenant) headers.set("X-Tenant-Id", tenantID())
  if (init.body) headers.set("Content-Type", "application/json")
  const response = await fetch(`${apiBase}${path}`, {
    ...init,
    headers,
    credentials: "include",
  })
  if (response.status === 204)
    return { code: 0, message: "ok", data: undefined as T }
  const body = (await response.json().catch(() => null)) as
    (Envelope<T> & ErrorResponse) | null
  if (!response.ok) throw apiError(response.status, body)
  if (!body) throw new ApiError(response.status, "invalid_response")
  return body
}

async function requestAuditExportWithBase(
  apiBase: string,
  tenantID: () => string,
  query: string
): Promise<AuditExportDownload> {
  const response = await fetch(
    `${apiBase}/iam/audit-events/export${query ? `?${query}` : ""}`,
    {
      headers: { "X-Tenant-Id": tenantID() },
      credentials: "include",
    }
  )
  if (!response.ok) {
    const body = (await response
      .json()
      .catch(() => null)) as ErrorResponse | null
    throw apiError(response.status, body)
  }
  if (!response.headers.get("content-type")?.startsWith("application/x-ndjson"))
    throw new ApiError(502, "invalid_audit_export_content_type")
  const blob = await response.blob()
  const tail = await blob.slice(Math.max(0, blob.size - 4096)).text()
  const lastLine = tail.trimEnd().split("\n").at(-1)
  let completion: unknown
  try {
    completion = lastLine ? JSON.parse(lastLine) : null
  } catch {
    completion = null
  }
  if (!isAuditExportCompletion(completion))
    throw new ApiError(502, "incomplete_audit_export")
  return {
    blob,
    filename: auditExportFilename(response.headers.get("content-disposition")),
  }
}

function apiError(status: number, body: ErrorResponse | null): ApiError {
  const message = body?.message?.trim()
  const code =
    (typeof body?.code === "string" ? body.code.trim() : "") ||
    body?.detail?.trim() ||
    message ||
    `HTTP ${status}`
  return new ApiError(status, code, message || undefined)
}

export function clientErrorMessage(code: string): string {
  const language =
    typeof navigator === "undefined" ? "en-US" : navigator.language || "en-US"
  const locale = language.toLowerCase().startsWith("zh")
    ? "zh-CN"
    : language.toLowerCase().startsWith("ms")
      ? "ms-MY"
      : "en-US"
  return (
    clientErrorMessages[locale][
      code as keyof (typeof clientErrorMessages)[typeof locale]
    ] ?? code
  )
}

function isAuditExportCompletion(value: unknown): value is {
  type: "complete"
  exported_events: number
  content_sha256: string
} {
  if (!value || typeof value !== "object") return false
  const record = value as Record<string, unknown>
  return (
    record.type === "complete" &&
    typeof record.exported_events === "number" &&
    Number.isSafeInteger(record.exported_events) &&
    record.exported_events >= 0 &&
    typeof record.content_sha256 === "string" &&
    /^[a-f0-9]{64}$/.test(record.content_sha256)
  )
}

function auditExportFilename(disposition: string | null): string {
  const candidate = disposition?.match(/filename="([^"]+)"/i)?.[1]
  return candidate && /^[a-zA-Z0-9._-]+$/.test(candidate)
    ? candidate
    : "chaosplus-audit.ndjson"
}

export function createIamApi(
  apiBase = baseUrl,
  tenantID: () => string = getTenant
) {
  const request = <T>(path: string, init: RequestInit = {}, tenant = true) =>
    requestWithBase<T>(apiBase, tenantID, path, init, tenant)
  const requestEnvelope = <T>(path: string) =>
    requestEnvelopeWithBase<T>(apiBase, tenantID, path)
  return {
    capabilities: () =>
      request<AuthnCapabilities>("/authn/capabilities", {}, false),
    register: (body: {
      email: string
      password: string
      display_name?: string
    }) =>
      request<{ accepted: boolean }>(
        "/authn/register",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    session: () => request<Session>("/authn/session", {}, false),
    login: (body: {
      login_name: string
      password: string
      return_url: string
    }) =>
      request<LoginResult>(
        "/authn/login",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    verifyLoginMfa: (body: { challenge_id: string; code: string }) =>
      request<LoginResult>(
        "/authn/login/mfa",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    logout: () =>
      request<{ logout_url: string }>(
        "/authn/logout",
        { method: "POST" },
        false
      ),
    sessions: () => request<BrowserSession[]>("/authn/sessions", {}, false),
    revokeSession: (id: string) =>
      request<{ revoked: boolean }>(
        `/authn/sessions/${encodeURIComponent(id)}`,
        { method: "DELETE" },
        false
      ),
    logoutAll: () =>
      request<{ logout_url: string }>(
        "/authn/logout-all",
        { method: "POST" },
        false
      ),
    changePassword: (body: {
      current_password: string
      new_password: string
    }) =>
      request<{ changed: boolean }>(
        "/authn/password/change",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    startPasswordRecovery: (identifier: string) =>
      request<{ accepted: boolean }>(
        "/authn/password/recovery/start",
        { method: "POST", body: JSON.stringify({ identifier }) },
        false
      ),
    completePasswordRecovery: (body: { token: string; new_password: string }) =>
      request<{ changed: boolean }>(
        "/authn/password/recovery/complete",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    startEmailVerification: () =>
      request<{ accepted: boolean }>(
        "/authn/email/verification/start",
        { method: "POST" },
        false
      ),
    completeEmailVerification: (token: string) =>
      request<{ verified: boolean }>(
        "/authn/email/verification/complete",
        { method: "POST", body: JSON.stringify({ token }) },
        false
      ),
    mfaStatus: () => request<MfaStatus>("/authn/mfa", {}, false),
    beginTotpEnrollment: (currentPassword: string) =>
      request<MfaEnrollment>(
        "/authn/mfa/totp/enroll",
        {
          method: "POST",
          body: JSON.stringify({ current_password: currentPassword }),
        },
        false
      ),
    confirmTotpEnrollment: (code: string) =>
      request<MfaConfirmation>(
        "/authn/mfa/totp/confirm",
        { method: "POST", body: JSON.stringify({ code }) },
        false
      ),
    regenerateRecoveryCodes: (body: {
      current_password: string
      code: string
    }) =>
      request<MfaConfirmation>(
        "/authn/mfa/recovery-codes/regenerate",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    disableTotp: (body: { current_password: string; code: string }) =>
      request<{ disabled: boolean }>(
        "/authn/mfa/totp",
        { method: "DELETE", body: JSON.stringify(body) },
        false
      ),
    passkeys: () => request<Passkey[]>("/authn/passkeys", {}, false),
    beginPasskeyRegistration: (currentPassword: string) =>
      request<PasskeyOptions>(
        "/authn/passkeys/registration/options",
        {
          method: "POST",
          body: JSON.stringify({ current_password: currentPassword }),
        },
        false
      ),
    finishPasskeyRegistration: (body: {
      challenge_id: string
      name: string
      credential: Record<string, unknown>
    }) =>
      request<Passkey>(
        "/authn/passkeys/registration/verify",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    beginPasskeyLogin: (returnUrl: string) =>
      request<PasskeyOptions>(
        "/authn/passkey/login/options",
        { method: "POST", body: JSON.stringify({ return_url: returnUrl }) },
        false
      ),
    finishPasskeyLogin: (body: {
      challenge_id: string
      credential: Record<string, unknown>
    }) =>
      request<LoginResult>(
        "/authn/passkey/login/verify",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    renamePasskey: (id: string, name: string) =>
      request<Passkey>(
        `/authn/passkeys/${encodeURIComponent(id)}`,
        { method: "PATCH", body: JSON.stringify({ name }) },
        false
      ),
    deletePasskey: (id: string, currentPassword: string) =>
      request<{ deleted: boolean }>(
        `/authn/passkeys/${encodeURIComponent(id)}`,
        {
          method: "DELETE",
          body: JSON.stringify({ current_password: currentPassword }),
        },
        false
      ),
    principals: (search = "") =>
      request<PrincipalList>(
        `/iam/principals?limit=200&search=${encodeURIComponent(search)}`
      ),
    principal: (id: string) =>
      request<Principal>(`/iam/principals/${encodeURIComponent(id)}`),
    createPrincipal: (body: {
      login_name: string
      password: string
      display_name: string
      email?: string
    }) =>
      request<Principal>("/iam/principals", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updatePrincipal: (
      id: string,
      body: { display_name?: string; email?: string }
    ) =>
      request<Principal>(`/iam/principals/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    disablePrincipal: (id: string) =>
      request<Principal>(`/iam/principals/${encodeURIComponent(id)}/disable`, {
        method: "POST",
      }),
    restorePrincipal: (id: string) =>
      request<Principal>(`/iam/principals/${encodeURIComponent(id)}/restore`, {
        method: "POST",
      }),
    serviceAccounts: (search = "") =>
      request<ServiceAccountList>(
        `/iam/service-accounts?limit=200&search=${encodeURIComponent(search)}`
      ),
    createServiceAccount: (body: ServiceAccountInput) =>
      request<ServiceAccount>("/iam/service-accounts", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateServiceAccount: (id: string, body: ServiceAccountUpdate) =>
      request<ServiceAccount>(
        `/iam/service-accounts/${encodeURIComponent(id)}`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    deleteServiceAccount: (id: string, version: number) =>
      request<{ deleted: boolean }>(
        `/iam/service-accounts/${encodeURIComponent(id)}?version=${encodeURIComponent(version)}`,
        { method: "DELETE" }
      ),
    serviceAccountCredentials: (id: string) =>
      request<ServiceAccountCredential[]>(
        `/iam/service-accounts/${encodeURIComponent(id)}/credentials`
      ),
    createServiceAccountCredential: (
      id: string,
      body: { name: string; scopes: string[]; expires_at?: string }
    ) =>
      request<ServiceAccountCredentialSecret>(
        `/iam/service-accounts/${encodeURIComponent(id)}/credentials`,
        { method: "POST", body: JSON.stringify(body) }
      ),
    revokeServiceAccountCredential: (id: string, credentialID: string) =>
      request<{ revoked: boolean }>(
        `/iam/service-accounts/${encodeURIComponent(id)}/credentials/${encodeURIComponent(credentialID)}`,
        { method: "DELETE" }
      ),
    invitations: () => request<Invitation[]>("/iam/invitations"),
    createInvitation: (body: {
      email: string
      department_id?: string
      role_ids?: string[]
      expires_in_hours?: number
    }) =>
      request<IssuedInvitation>("/iam/invitations", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    revokeInvitation: (id: string) =>
      request<{ revoked: boolean }>(
        `/iam/invitations/${encodeURIComponent(id)}`,
        { method: "DELETE" }
      ),
    resendInvitation: (id: string, expiresInHours = 72) =>
      request<IssuedInvitation>(
        `/iam/invitations/${encodeURIComponent(id)}/resend`,
        {
          method: "POST",
          body: JSON.stringify({ expires_in_hours: expiresInHours }),
        }
      ),
    acceptInvitation: (body: {
      token: string
      login_name: string
      password: string
      display_name?: string
    }) =>
      request<InvitationAcceptance>(
        "/iam/invitations/accept",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    tenants: (includeDeleted = false) =>
      request<Tenant[]>(
        `/iam/tenants?include_deleted=${includeDeleted}`,
        {},
        false
      ),
    createTenant: (body: TenantInput) =>
      request<Tenant>(
        "/iam/tenants",
        { method: "POST", body: JSON.stringify(body) },
        false
      ),
    updateTenant: (id: string, body: TenantUpdate) =>
      request<Tenant>(
        `/iam/tenants/${encodeURIComponent(id)}`,
        { method: "PATCH", body: JSON.stringify(body) },
        false
      ),
    deleteTenant: (id: string, version: number) =>
      request<{ deleted: boolean }>(
        `/iam/tenants/${encodeURIComponent(id)}?version=${encodeURIComponent(version)}`,
        { method: "DELETE" },
        false
      ),
    members: (search = "") =>
      request<Member[]>(
        `/iam/members?limit=200&search=${encodeURIComponent(search)}`
      ),
    updateMember: (
      subject: string,
      body: Partial<
        Pick<Member, "display_name" | "email" | "department_id" | "status">
      >
    ) =>
      request<Member>(`/iam/members/${encodeURIComponent(subject)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    memberRoles: (subject: string) =>
      request<string[]>(`/iam/members/${encodeURIComponent(subject)}/roles`),
    entities: () => request<Entity[]>("/iam/entities"),
    entity: (id: string) =>
      request<Entity>(`/iam/entities/${encodeURIComponent(id)}`),
    createEntity: (body: EntityInput) =>
      request<Entity>("/iam/entities", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateEntity: (id: string, body: Partial<EntityInput>) =>
      request<Entity>(`/iam/entities/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deleteEntity: (id: string) =>
      request<{ changed: boolean; sync_status: string }>(
        `/iam/entities/${encodeURIComponent(id)}`,
        { method: "DELETE" }
      ),
    entityRoleBindings: (id: string) =>
      request<EntityRoleBinding[]>(
        `/iam/entities/${encodeURIComponent(id)}/role-bindings`
      ),
    putEntityRoleBinding: (
      id: string,
      roleID: string,
      principalID: string,
      body: EntityRoleBindingInput
    ) =>
      request<EntityRoleBinding>(
        `/iam/entities/${encodeURIComponent(id)}/role-bindings/${encodeURIComponent(roleID)}/${encodeURIComponent(principalID)}`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    deleteEntityRoleBinding: (
      id: string,
      roleID: string,
      principalID: string
    ) =>
      request<{ changed: boolean; sync_status: string }>(
        `/iam/entities/${encodeURIComponent(id)}/role-bindings/${encodeURIComponent(roleID)}/${encodeURIComponent(principalID)}`,
        { method: "DELETE" }
      ),
    relationships: (resourceID = "") =>
      request<Relationship[]>(
        `/iam/relationships${resourceID ? `?resource_id=${encodeURIComponent(resourceID)}` : ""}`
      ),
    resourceRelationships: (entityID: string) =>
      request<Relationship[]>(
        `/iam/relationships?entity_id=${encodeURIComponent(entityID)}`
      ),
    putRelationship: (body: RelationshipInput) =>
      request<Relationship>("/iam/relationships", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    deleteRelationship: (relationship: RelationshipInput) => {
      const query = new URLSearchParams()
      const fields = {
        entity_id: relationship.entity_id,
        subject_type: relationship.subject_type,
        subject_id: relationship.subject_id,
        subject_relation: relationship.subject_relation,
        relation: relationship.relation,
        resource_type: relationship.resource_type,
        resource_id: relationship.resource_id,
      }
      for (const [key, value] of Object.entries(fields))
        if (value) query.set(key, value)
      return request<{ changed: boolean; sync_status: string }>(
        `/iam/relationships?${query}`,
        { method: "DELETE" }
      )
    },
    checkEntityAuthorization: (
      entityID: string,
      permissionCode: string,
      subject: string
    ) =>
      request<
        Pick<AuthorizationExplanation, "allowed" | "reason" | "revision">
      >("/iam/authorization/check", {
        method: "POST",
        body: JSON.stringify({
          entity_id: entityID,
          permission_code: permissionCode,
          subject,
        }),
      }),
    checkResourceAuthorization: (
      entityID: string,
      resourceType: string,
      resourceID: string,
      permissionCode: string,
      subject: string
    ) =>
      request<
        Pick<AuthorizationExplanation, "allowed" | "reason" | "revision">
      >("/iam/authorization/check", {
        method: "POST",
        body: JSON.stringify({
          entity_id: entityID,
          resource_type: resourceType,
          resource_id: resourceID,
          permission_code: permissionCode,
          subject,
        }),
      }),
    authorizationConstraint: (permissionCode: string, subject: string) =>
      request<DataConstraint>("/iam/authorization/constraints", {
        method: "POST",
        body: JSON.stringify({
          permission_code: permissionCode,
          subject,
        }),
      }),
    explainEntityAuthorization: (
      entityID: string,
      permissionCode: string,
      subject: string
    ) =>
      request<AuthorizationExplanation>("/iam/authorization/explain", {
        method: "POST",
        body: JSON.stringify({
          entity_id: entityID,
          permission_code: permissionCode,
          subject,
        }),
      }),
    explainResourceAuthorization: (
      entityID: string,
      resourceType: string,
      resourceID: string,
      permissionCode: string,
      subject: string
    ) =>
      request<AuthorizationExplanation>("/iam/authorization/explain", {
        method: "POST",
        body: JSON.stringify({
          entity_id: entityID,
          resource_type: resourceType,
          resource_id: resourceID,
          permission_code: permissionCode,
          subject,
        }),
      }),
    departments: () => request<Department[]>("/iam/departments"),
    department: (id: string) =>
      request<Department>(`/iam/departments/${encodeURIComponent(id)}`),
    createDepartment: (body: DepartmentInput) =>
      request<Department>("/iam/departments", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateDepartment: (
      id: string,
      body: Partial<DepartmentInput> & Pick<Department, "version">
    ) =>
      request<Department>(`/iam/departments/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deleteDepartment: (id: string, version: number) =>
      request<{ deleted: boolean }>(
        `/iam/departments/${encodeURIComponent(id)}?version=${encodeURIComponent(version)}`,
        { method: "DELETE" }
      ),
    positions: () => request<Position[]>("/iam/positions"),
    position: (id: string) =>
      request<Position>(`/iam/positions/${encodeURIComponent(id)}`),
    createPosition: (body: PositionInput) =>
      request<Position>("/iam/positions", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updatePosition: (
      id: string,
      body: Partial<PositionInput> & Pick<Position, "version">
    ) =>
      request<Position>(`/iam/positions/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deletePosition: (id: string, version: number) =>
      request<{ deleted: boolean }>(
        `/iam/positions/${encodeURIComponent(id)}?version=${encodeURIComponent(version)}`,
        { method: "DELETE" }
      ),
    positionMembers: (id: string) =>
      request<PositionMember[]>(
        `/iam/positions/${encodeURIComponent(id)}/members`
      ),
    putPositionMember: (
      id: string,
      principalID: string,
      body: PositionMemberInput
    ) =>
      request<PositionMember>(
        `/iam/positions/${encodeURIComponent(id)}/members/${encodeURIComponent(principalID)}`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    deletePositionMember: (id: string, principalID: string) =>
      request<{ deleted: boolean }>(
        `/iam/positions/${encodeURIComponent(id)}/members/${encodeURIComponent(principalID)}`,
        { method: "DELETE" }
      ),
    groups: () => request<Group[]>("/iam/groups"),
    group: (id: string) =>
      request<Group>(`/iam/groups/${encodeURIComponent(id)}`),
    createGroup: (body: GroupInput) =>
      request<Group>("/iam/groups", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateGroup: (
      id: string,
      body: Partial<Omit<GroupInput, "type">> & Pick<Group, "version">
    ) =>
      request<Group>(`/iam/groups/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deleteGroup: (id: string, version: number) =>
      request<{ deleted: boolean }>(
        `/iam/groups/${encodeURIComponent(id)}?version=${encodeURIComponent(version)}`,
        { method: "DELETE" }
      ),
    groupMembers: (id: string) =>
      request<GroupMember[]>(`/iam/groups/${encodeURIComponent(id)}/members`),
    putGroupMember: (id: string, principalID: string, body: GroupMemberInput) =>
      request<GroupMember>(
        `/iam/groups/${encodeURIComponent(id)}/members/${encodeURIComponent(principalID)}`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    deleteGroupMember: (id: string, principalID: string) =>
      request<{ deleted: boolean }>(
        `/iam/groups/${encodeURIComponent(id)}/members/${encodeURIComponent(principalID)}`,
        { method: "DELETE" }
      ),
    roles: () => request<Role[]>("/iam/roles"),
    requestableRoles: () =>
      request<RequestableRole[]>("/iam/requestable-roles"),
    myAccessRequests: () => request<AccessRequest[]>("/iam/my/access-requests"),
    accessRequests: () => request<AccessRequest[]>("/iam/access-requests"),
    createAccessRequest: (body: AccessRequestInput) =>
      request<AccessRequest>("/iam/access-requests", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    approveAccessRequest: (id: string, note = "") =>
      request<AccessRequest>(
        `/iam/access-requests/${encodeURIComponent(id)}/approve`,
        { method: "POST", body: JSON.stringify({ note }) }
      ),
    rejectAccessRequest: (id: string, note = "") =>
      request<AccessRequest>(
        `/iam/access-requests/${encodeURIComponent(id)}/reject`,
        { method: "POST", body: JSON.stringify({ note }) }
      ),
    revokeAccessRequest: (id: string, reason = "") =>
      request<AccessRequest>(
        `/iam/access-requests/${encodeURIComponent(id)}/revoke`,
        { method: "POST", body: JSON.stringify({ reason }) }
      ),
    withdrawAccessRequest: (id: string, reason = "") =>
      request<AccessRequest>(
        `/iam/access-requests/${encodeURIComponent(id)}/withdraw`,
        { method: "POST", body: JSON.stringify({ reason }) }
      ),
    accessReviews: () => request<AccessReview[]>("/iam/access-reviews"),
    accessReview: (id: string) =>
      request<AccessReview>(`/iam/access-reviews/${encodeURIComponent(id)}`),
    createAccessReview: (body: AccessReviewInput) =>
      request<AccessReview>("/iam/access-reviews", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    decideAccessReviewItem: (
      reviewID: string,
      itemID: string,
      decision: AccessReviewDecision,
      note = ""
    ) =>
      request<AccessReviewItem>(
        `/iam/access-reviews/${encodeURIComponent(reviewID)}/items/${encodeURIComponent(itemID)}/decide`,
        { method: "POST", body: JSON.stringify({ decision, note }) }
      ),
    completeAccessReview: (id: string) =>
      request<AccessReview>(
        `/iam/access-reviews/${encodeURIComponent(id)}/complete`,
        { method: "POST" }
      ),
    cancelAccessReview: (id: string) =>
      request<AccessReview>(
        `/iam/access-reviews/${encodeURIComponent(id)}/cancel`,
        { method: "POST" }
      ),
    createRole: (body: Pick<Role, "name" | "description">) =>
      request<Role>("/iam/roles", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateRole: (
      id: string,
      body: Partial<Pick<Role, "name" | "description">>
    ) =>
      request<Role>(`/iam/roles/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deleteRole: (id: string) =>
      request(`/iam/roles/${encodeURIComponent(id)}`, { method: "DELETE" }),
    rolePermissions: (id: string) =>
      request<string[]>(`/iam/roles/${encodeURIComponent(id)}/permissions`),
    rolePermissionGrants: (id: string) =>
      request<RolePermissionGrant[]>(
        `/iam/roles/${encodeURIComponent(id)}/permission-grants`
      ),
    grantPermission: (id: string, code: string) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/permissions/${encodeURIComponent(code)}`,
        { method: "PUT" }
      ),
    revokePermission: (id: string, code: string) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/permissions/${encodeURIComponent(code)}`,
        { method: "DELETE" }
      ),
    setRolePermissionCondition: (
      id: string,
      code: string,
      condition: RelationshipCondition
    ) =>
      request<RolePermissionGrant>(
        `/iam/roles/${encodeURIComponent(id)}/permissions/${encodeURIComponent(code)}/condition`,
        { method: "PUT", body: JSON.stringify({ condition }) }
      ),
    clearRolePermissionCondition: (id: string, code: string) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/permissions/${encodeURIComponent(code)}/condition`,
        { method: "DELETE" }
      ),
    roleMembers: (id: string) =>
      request<string[]>(`/iam/roles/${encodeURIComponent(id)}/members`),
    addRoleMember: (id: string, subject: string) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/members/${encodeURIComponent(subject)}`,
        { method: "PUT" }
      ),
    removeRoleMember: (id: string, subject: string) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/members/${encodeURIComponent(subject)}`,
        { method: "DELETE" }
      ),
    roleDirectoryBindings: (id: string) =>
      request<RoleDirectoryBinding[]>(
        `/iam/roles/${encodeURIComponent(id)}/directory-bindings`
      ),
    roleDataScope: (id: string) =>
      request<RoleDataScope>(`/iam/roles/${encodeURIComponent(id)}/data-scope`),
    setRoleDataScope: (
      id: string,
      body: Pick<RoleDataScope, "scope" | "department_ids">
    ) =>
      request<RoleDataScope>(
        `/iam/roles/${encodeURIComponent(id)}/data-scope`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    addRoleDirectoryBinding: (
      id: string,
      assigneeType: RoleDirectoryBinding["assignee_type"],
      assigneeID: string
    ) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/directory-bindings/${encodeURIComponent(assigneeType)}/${encodeURIComponent(assigneeID)}`,
        { method: "PUT" }
      ),
    removeRoleDirectoryBinding: (
      id: string,
      assigneeType: RoleDirectoryBinding["assignee_type"],
      assigneeID: string
    ) =>
      request(
        `/iam/roles/${encodeURIComponent(id)}/directory-bindings/${encodeURIComponent(assigneeType)}/${encodeURIComponent(assigneeID)}`,
        { method: "DELETE" }
      ),
    permissions: () => request<Permission[]>("/iam/permission-catalog"),
    menus: () => request<Menu[]>("/iam/menus"),
    effectiveMenus: async () =>
      (await request<EffectiveMenu[] | null>("/iam/me/menus")) ?? [],
    createMenu: (body: MenuInput) =>
      request<Menu>("/iam/menus", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateMenu: (id: string, body: Partial<MenuInput>) =>
      request<Menu>(`/iam/menus/${encodeURIComponent(id)}`, {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    deleteMenu: (id: string) =>
      request(`/iam/menus/${encodeURIComponent(id)}`, { method: "DELETE" }),
    oauthClients: () => request<OAuthClient[]>("/iam/oauth-clients"),
    createOAuthClient: (body: OAuthClientInput) =>
      request<OAuthClientCreateResult>("/iam/oauth-clients", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateOAuthClient: (
      id: string,
      body: OAuthClientInput & Pick<OAuthClient, "status">
    ) =>
      request<OAuthClient>(`/iam/oauth-clients/${encodeURIComponent(id)}`, {
        method: "PUT",
        body: JSON.stringify(body),
      }),
    rotateOAuthClientSecret: (id: string) =>
      request<{ client_secret: string }>(
        `/iam/oauth-clients/${encodeURIComponent(id)}/rotate-secret`,
        { method: "POST" }
      ),
    deleteOAuthClient: (id: string) =>
      request<{ deleted: boolean }>(
        `/iam/oauth-clients/${encodeURIComponent(id)}`,
        { method: "DELETE" }
      ),
    scimDirectories: () => request<SCIMDirectory[]>("/iam/scim/directories"),
    createSCIMDirectory: (body: SCIMDirectoryInput) =>
      request<SCIMDirectory>("/iam/scim/directories", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    updateSCIMDirectory: (
      id: string,
      body: SCIMDirectoryInput & Pick<SCIMDirectory, "status" | "version">
    ) =>
      request<SCIMDirectory>(
        `/iam/scim/directories/${encodeURIComponent(id)}`,
        { method: "PUT", body: JSON.stringify(body) }
      ),
    scimCredentials: (directoryID: string) =>
      request<SCIMCredential[]>(
        `/iam/scim/directories/${encodeURIComponent(directoryID)}/credentials`
      ),
    createSCIMCredential: (
      directoryID: string,
      body: { name: string; expires_at?: string }
    ) =>
      request<SCIMCredentialSecret>(
        `/iam/scim/directories/${encodeURIComponent(directoryID)}/credentials`,
        { method: "POST", body: JSON.stringify(body) }
      ),
    revokeSCIMCredential: (directoryID: string, credentialID: string) =>
      request<{ revoked: boolean }>(
        `/iam/scim/directories/${encodeURIComponent(directoryID)}/credentials/${encodeURIComponent(credentialID)}`,
        { method: "DELETE" }
      ),
    auditEvents: async (query = "") => {
      const envelope = await requestEnvelope<AuditEvent[]>(
        `/iam/audit-events${query ? `?${query}` : ""}`
      )
      return {
        items: envelope.data,
        total: envelope.meta?.page?.total ?? envelope.data.length,
      }
    },
    auditEvent: (id: string) =>
      request<AuditEvent>(`/iam/audit-events/${encodeURIComponent(id)}`),
    auditIntegrity: () => request<AuditIntegrity>("/iam/audit-integrity"),
    auditExport: (query = "") =>
      requestAuditExportWithBase(apiBase, tenantID, query),
    auditGovernance: () =>
      request<AuditGovernance>("/iam/audit/governance"),
    setAuditRetention: (
      body: Pick<AuditRetentionPolicy, "min_days" | "archive_after_days">
    ) =>
      request<AuditRetentionPolicy>("/iam/audit/retention", {
        method: "PUT",
        body: JSON.stringify(body),
      }),
    auditSignRoot: () =>
      request<AuditAnchor>("/iam/audit/roots/sign", { method: "POST" }),
  }
}

export type IamApi = ReturnType<typeof createIamApi>
export const iamApi = createIamApi()
