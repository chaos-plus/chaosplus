import type { Envelope, Member, Menu, Permission, Role, Session } from './types'

export const API_URL = import.meta.env.VITE_API_URL ?? 'http://127.0.0.1:18080'
const tenantKey = 'chaosplus.tenant'

export class ApiError extends Error {
  constructor(public status: number, message: string) { super(message) }
}

export function getTenant(): string {
	return localStorage.getItem(tenantKey) ?? import.meta.env.VITE_DEFAULT_TENANT ?? 'platform'
}

export function setTenant(value: string): void {
  localStorage.setItem(tenantKey, value.trim())
  window.dispatchEvent(new Event('tenant-change'))
}

async function request<T>(path: string, init: RequestInit = {}, tenant = true): Promise<T> {
  const headers = new Headers(init.headers)
  if (tenant) headers.set('X-Tenant-Id', getTenant())
  if (init.body) headers.set('Content-Type', 'application/json')
  const response = await fetch(`${API_URL}${path}`, { ...init, headers, credentials: 'include' })
  if (response.status === 204) return undefined as T
  const body = await response.json().catch(() => null) as Envelope<T> | null
  if (!response.ok) throw new ApiError(response.status, body?.message ?? `HTTP ${response.status}`)
  return body!.data
}

export const api = {
  session: () => request<Session>('/authn/session', {}, false),
  login: (body: { login_name: string; password: string; return_url: string }) => request<{ return_url: string }>('/authn/login', { method: 'POST', body: JSON.stringify(body) }, false),
  logout: () => request<{ logout_url: string }>('/authn/logout', { method: 'POST' }, false),
  members: (search = '') => request<Member[]>(`/iam/members?limit=200&search=${encodeURIComponent(search)}`),
  member: (subject: string) => request<Member>(`/iam/members/${encodeURIComponent(subject)}`),
  createMember: (body: Pick<Member, 'subject' | 'display_name' | 'email' | 'status'>) => request<Member>('/iam/members', { method: 'POST', body: JSON.stringify(body) }),
  updateMember: (subject: string, body: Partial<Pick<Member, 'display_name' | 'email' | 'status'>>) => request<Member>(`/iam/members/${encodeURIComponent(subject)}`, { method: 'PATCH', body: JSON.stringify(body) }),
  memberRoles: (subject: string) => request<string[]>(`/iam/members/${encodeURIComponent(subject)}/roles`),
  roles: () => request<Role[]>('/iam/roles'),
  createRole: (body: Pick<Role, 'name' | 'description'>) => request<Role>('/iam/roles', { method: 'POST', body: JSON.stringify(body) }),
  updateRole: (id: string, body: Partial<Pick<Role, 'name' | 'description'>>) => request<Role>(`/iam/roles/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteRole: (id: string) => request(`/iam/roles/${id}`, { method: 'DELETE' }),
  rolePermissions: (id: string) => request<string[]>(`/iam/roles/${id}/permissions`),
  grantPermission: (id: string, code: string) => request(`/iam/roles/${id}/permissions/${code}`, { method: 'PUT' }),
  revokePermission: (id: string, code: string) => request(`/iam/roles/${id}/permissions/${code}`, { method: 'DELETE' }),
  roleMembers: (id: string) => request<string[]>(`/iam/roles/${id}/members`),
  addRoleMember: (id: string, subject: string) => request(`/iam/roles/${id}/members/${encodeURIComponent(subject)}`, { method: 'PUT' }),
  removeRoleMember: (id: string, subject: string) => request(`/iam/roles/${id}/members/${encodeURIComponent(subject)}`, { method: 'DELETE' }),
  permissions: () => request<Permission[]>('/iam/permission-catalog'),
  menus: () => request<Menu[]>('/iam/menus'),
  effectiveMenus: () => request<Menu[]>('/iam/me/menus'),
  createMenu: (body: Omit<Menu, 'id' | 'tenant_id'>) => request<Menu>('/iam/menus', { method: 'POST', body: JSON.stringify(body) }),
  updateMenu: (id: string, body: Partial<Menu>) => request<Menu>(`/iam/menus/${id}`, { method: 'PATCH', body: JSON.stringify(body) }),
  deleteMenu: (id: string) => request(`/iam/menus/${id}`, { method: 'DELETE' }),
}

export function oidcURL(mode: 'login' | 'register', returnURL: string): string {
  const query = new URLSearchParams({ mode, return_url: returnURL })
  return `${API_URL}/authn/oidc/start?${query}`
}
