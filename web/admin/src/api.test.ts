import { beforeEach, describe, expect, it, vi } from 'vitest'
import { api, getTenant, oidcURL, setTenant } from './api'

describe('api client', () => {
  beforeEach(() => { localStorage.clear(); vi.restoreAllMocks() })
  it('persists tenant selection', () => { setTenant(' t1 '); expect(getTenant()).toBe('t1') })
  it('uses cookie credentials and tenant header', async () => {
    setTenant('t1')
    const fetcher = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response(JSON.stringify({ code: 0, data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } }))
    await api.roles()
    const init = fetcher.mock.calls[0][1]!
    expect(init.credentials).toBe('include')
    expect(new Headers(init.headers).get('X-Tenant-Id')).toBe('t1')
  })
	it('builds a server-side OIDC URL', () => { expect(oidcURL('login', 'http://127.0.0.1:5173/')).toContain('/authn/oidc/start?') })
	it('covers every production API operation', async () => {
		setTenant('t1')
		const fetcher = vi.spyOn(globalThis, 'fetch').mockImplementation(async (_input, init) => {
			if (init?.method === 'POST' && String(_input).endsWith('/authn/logout')) return new Response(null, { status: 204 })
			return new Response(JSON.stringify({ code: 0, data: [] }), { status: 200, headers: { 'Content-Type': 'application/json' } })
		})
		await Promise.all([
			api.session(), api.login({ login_name: 'alice', password: 'secret', return_url: 'http://app/' }), api.logout(), api.members('alice'), api.member('u1'), api.createMember({ subject: 'u1', display_name: 'User', email: '', status: 'active' }), api.updateMember('u1', { status: 'disabled' }), api.memberRoles('u1'),
			api.roles(), api.createRole({ name: 'Role', description: '' }), api.updateRole('r1', { name: 'Next' }), api.deleteRole('r1'), api.rolePermissions('r1'), api.grantPermission('r1', 'user_view'), api.revokePermission('r1', 'user_view'), api.roleMembers('r1'), api.addRoleMember('r1', 'u1'), api.removeRoleMember('r1', 'u1'),
			api.permissions(), api.menus(), api.effectiveMenus(), api.createMenu({ label: 'Users', route: '/iam/users', sort_order: 0, status: 'active' }), api.updateMenu('m1', { label: 'People' }), api.deleteMenu('m1'),
		])
		expect(fetcher).toHaveBeenCalledTimes(24)
		const sessionHeaders = new Headers(fetcher.mock.calls[0][1]?.headers)
		expect(sessionHeaders.has('X-Tenant-Id')).toBe(false)
	})
	it('maps error envelopes to ApiError', async () => {
		vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(JSON.stringify({ code: 403, message: 'forbidden' }), { status: 403 })).mockResolvedValueOnce(new Response('not-json', { status: 500 }))
		await expect(api.roles()).rejects.toEqual(expect.objectContaining({ status: 403, message: 'forbidden' }))
		await expect(api.roles()).rejects.toEqual(expect.objectContaining({ status: 500, message: 'HTTP 500' }))
	})
})
