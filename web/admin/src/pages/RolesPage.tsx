import { useEffect, useMemo, useState } from 'react'
import { Plus, Shield, Trash2 } from 'lucide-react'
import { api } from '../api'
import { Dialog, Empty, FormActions, PageHeader, submitJSON } from '../components'
import type { Permission, Role } from '../types'

export default function RolesPage() {
  const [roles, setRoles] = useState<Role[]>([])
  const [permissions, setPermissions] = useState<Permission[]>([])
  const [selected, setSelected] = useState<Role | null>(null)
  const [granted, setGranted] = useState<string[]>([])
  const [members, setMembers] = useState<string[]>([])
  const [open, setOpen] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const refresh = () => api.roles().then((data) => { setRoles(data); if (selected) setSelected(data.find((r) => r.id === selected.id) ?? null) })
  useEffect(() => { Promise.all([api.roles(), api.permissions()]).then(([nextRoles, nextPermissions]) => { setRoles(nextRoles); setPermissions(nextPermissions); setSelected(nextRoles[0] ?? null) }).catch((e) => setError(e.message)) }, [])
	useEffect(() => { if (selected) Promise.all([api.rolePermissions(selected.id), api.roleMembers(selected.id)]).then(([p, m]) => { setGranted(p ?? []); setMembers(m ?? []) }) }, [selected])
	const groups = useMemo(() => permissions.reduce<Record<string, Permission[]>>((result, item) => { (result[item.resource] ??= []).push(item); return result }, {}), [permissions])
  const create = async (event: React.FormEvent<HTMLFormElement>) => { const data = submitJSON(event); setBusy(true); try { const role = await api.createRole({ name: data.name, description: data.description }); setOpen(false); await refresh(); setSelected(role) } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const toggle = async (code: string, checked: boolean) => { if (!selected) return; setBusy(true); setError(''); try { if (checked) await api.grantPermission(selected.id, code); else await api.revokePermission(selected.id, code); setGranted(await api.rolePermissions(selected.id)) } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const remove = async () => { if (!selected || !confirm(`删除角色“${selected.name}”？`)) return; setBusy(true); setError(''); try { await api.deleteRole(selected.id); setSelected(null); await refresh() } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }

  return <><PageHeader title="角色与权限" description="权限变更通过 outbox 同步到 SpiceDB" action={<button className="button primary" onClick={() => setOpen(true)}><Plus size={17} />新建角色</button>} />{error && <div className="alert">{error}</div>}
    <div className="split-view"><aside className="role-list"><header><strong>角色</strong><span>{roles.length}</span></header>{roles.map((role) => <button key={role.id} className={selected?.id === role.id ? 'selected' : ''} onClick={() => setSelected(role)}><span className="role-icon"><Shield size={17} /></span><span><strong>{role.name}</strong><small>{role.description || '无描述'}</small></span></button>)}{!roles.length && <Empty>暂无角色</Empty>}</aside>
			<section className="permission-pane">{selected ? <><header className="pane-header"><div><h2>{selected.name}</h2><p>{members.length} 个成员 · {granted.length} 项权限</p></div><button className="icon-button danger-icon" onClick={remove} aria-label="删除角色"><Trash2 size={18} /></button></header><div className="permission-groups">{Object.entries(groups).map(([resource, actions]) => <section key={resource}><h3>{resource}</h3><div className="permission-grid">{actions.map((permission) => <label key={permission.code}><input type="checkbox" checked={granted.includes(permission.code)} disabled={busy} onChange={(e) => toggle(permission.code, e.target.checked)} /><span><strong>{permission.code}</strong><small>{permission.summary}</small></span></label>)}</div></section>)}</div></> : <Empty>选择一个角色查看权限</Empty>}</section></div>
    <Dialog title="新建角色" open={open} onClose={() => setOpen(false)}><form className="form-grid" onSubmit={create}><label className="full">角色名称<input name="name" required maxLength={128} /></label><label className="full">描述<textarea name="description" maxLength={4096} rows={4} /></label><FormActions busy={busy} onCancel={() => setOpen(false)} /></form></Dialog>
  </>
}
