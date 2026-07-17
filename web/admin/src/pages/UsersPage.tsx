import { useEffect, useMemo, useState } from 'react'
import { Plus, Search, UserRoundCheck } from 'lucide-react'
import { api } from '../api'
import { Badge, Dialog, Empty, FormActions, PageHeader, submitJSON } from '../components'
import type { Member, Role } from '../types'

export default function UsersPage() {
  const [members, setMembers] = useState<Member[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [search, setSearch] = useState('')
  const [selected, setSelected] = useState<Member | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const refresh = () => Promise.all([api.members(search), api.roles()]).then(([nextMembers, nextRoles]) => { setMembers(nextMembers); setRoles(nextRoles) })
  useEffect(() => { refresh().catch((e) => setError(e.message)) }, [search])
  const filtered = useMemo(() => members, [members])

  const create = async (event: React.FormEvent<HTMLFormElement>) => {
    const data = submitJSON(event); setBusy(true); setError('')
    try { await api.createMember({ subject: data.subject, display_name: data.display_name, email: data.email, status: 'active' }); setCreateOpen(false); await refresh() } catch (e) { setError((e as Error).message) } finally { setBusy(false) }
  }
  return <><PageHeader title="用户成员" description="租户成员与 Zitadel 身份绑定" action={<button className="button primary" onClick={() => setCreateOpen(true)}><Plus size={17} />绑定成员</button>} />
    <div className="toolbar"><label className="search-box"><Search size={17} /><input aria-label="搜索成员" placeholder="搜索姓名、邮箱或 subject" value={search} onChange={(e) => setSearch(e.target.value)} /></label><span className="record-count">{members.length} 条</span></div>
    {error && <div className="alert">{error}</div>}
    <div className="table-wrap"><table><thead><tr><th>成员</th><th>Subject</th><th>状态</th><th>更新时间</th></tr></thead><tbody>{filtered.map((member) => <tr key={member.subject} onClick={() => setSelected(member)} tabIndex={0}><td><div className="identity-cell"><span className="avatar small">{member.display_name.slice(0, 1).toUpperCase()}</span><div><strong>{member.display_name}</strong><small>{member.email || '未登记邮箱'}</small></div></div></td><td><code>{member.subject}</code></td><td><Badge status={member.status} /></td><td>{new Date(member.updated_at).toLocaleString()}</td></tr>)}</tbody></table>{!members.length && <Empty><UserRoundCheck size={28} />暂无租户成员</Empty>}</div>
    <Dialog open={createOpen} onClose={() => setCreateOpen(false)} title="绑定 Zitadel 成员"><form onSubmit={create} className="form-grid"><label>显示名称<input name="display_name" required maxLength={128} /></label><label>邮箱<input name="email" type="email" maxLength={320} /></label><label className="full">Zitadel subject<input name="subject" required maxLength={255} autoComplete="off" /></label><FormActions busy={busy} onCancel={() => setCreateOpen(false)} /></form></Dialog>
    <MemberDrawer member={selected} roles={roles} onClose={() => setSelected(null)} onChanged={async () => { await refresh(); if (selected) setSelected(await api.member(selected.subject)) }} />
  </>
}

function MemberDrawer({ member, roles, onClose, onChanged }: { member: Member | null; roles: Role[]; onClose: () => void; onChanged: () => Promise<void> }) {
	const [assigned, setAssigned] = useState<string[]>([])
	const [rolesLoading, setRolesLoading] = useState(false)
	const [busy, setBusy] = useState(false)
	const [error, setError] = useState('')
	useEffect(() => { if (member) { setRolesLoading(true); setError(''); api.memberRoles(member.subject).then((data) => setAssigned(data ?? [])).catch((e) => { setAssigned([]); setError((e as Error).message) }).finally(() => setRolesLoading(false)) } }, [member])
  if (!member) return null
	const toggleRole = async (role: Role, checked: boolean) => { setBusy(true); setError(''); try { if (checked) await api.addRoleMember(role.id, member.subject); else await api.removeRoleMember(role.id, member.subject); setAssigned((await api.memberRoles(member.subject)) ?? []) } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const toggleStatus = async () => { setBusy(true); setError(''); try { await api.updateMember(member.subject, { status: member.status === 'active' ? 'disabled' : 'active' }); await onChanged() } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
	return <div className="drawer-backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}><aside className="detail-drawer"><header><div><h2>{member.display_name}</h2><code>{member.subject}</code></div><button className="button secondary" onClick={onClose}>关闭</button></header>{error && <div className="alert">{error}</div>}<section><h3>成员状态</h3><div className="status-line"><Badge status={member.status} /><button className={`button ${member.status === 'active' ? 'danger' : 'primary'}`} disabled={busy} onClick={toggleStatus}>{member.status === 'active' ? '停用成员' : '重新启用'}</button></div></section><section><h3>角色分配</h3><div className="check-list">{roles.map((role) => <label key={role.id}><input type="checkbox" checked={assigned.includes(role.id)} disabled={rolesLoading || busy || member.status !== 'active'} onChange={(e) => toggleRole(role, e.target.checked)} /><span><strong>{role.name}</strong><small>{role.description || role.id}</small></span></label>)}</div></section></aside></div>
}
