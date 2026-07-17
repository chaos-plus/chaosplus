import { useEffect, useState } from 'react'
import { GripVertical, Plus, Trash2 } from 'lucide-react'
import { api } from '../api'
import { Badge, Dialog, Empty, FormActions, PageHeader, submitJSON } from '../components'
import type { Menu, Permission } from '../types'

export default function MenusPage() {
  const [menus, setMenus] = useState<Menu[]>([])
  const [permissions, setPermissions] = useState<Permission[]>([])
  const [editing, setEditing] = useState<Menu | null | undefined>()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const refresh = () => api.menus().then(setMenus)
  useEffect(() => { Promise.all([api.menus(), api.permissions()]).then(([m, p]) => { setMenus(m); setPermissions(p) }).catch((e) => setError(e.message)) }, [])
  const save = async (event: React.FormEvent<HTMLFormElement>) => { const data = submitJSON(event); const body = { parent_id: data.parent_id, label: data.label, route: data.route, icon: data.icon, sort_order: Number(data.sort_order || 0), permission_code: data.permission_code, status: data.status as 'active' | 'disabled' }; setBusy(true); try { if (editing) await api.updateMenu(editing.id, body); else await api.createMenu(body); setEditing(undefined); await refresh() } catch (e) { setError((e as Error).message) } finally { setBusy(false) } }
  const remove = async (menu: Menu) => { if (!confirm(`删除菜单“${menu.label}”？`)) return; try { await api.deleteMenu(menu.id); await refresh() } catch (e) { setError((e as Error).message) } }
  return <><PageHeader title="菜单管理" description="菜单节点引用统一权限目录" action={<button className="button primary" onClick={() => setEditing(null)}><Plus size={17} />新建菜单</button>} />{error && <div className="alert">{error}</div>}
    <div className="table-wrap"><table><thead><tr><th>顺序</th><th>菜单</th><th>路由</th><th>权限码</th><th>状态</th><th aria-label="操作" /></tr></thead><tbody>{menus.map((menu) => <tr key={menu.id}><td><span className="sort-cell"><GripVertical size={16} />{menu.sort_order}</span></td><td><button className="link-button" onClick={() => setEditing(menu)}>{menu.label}</button><small className="subline">{menu.parent_id ? `父级 ${menus.find((m) => m.id === menu.parent_id)?.label ?? menu.parent_id}` : '根节点'}</small></td><td><code>{menu.route || '-'}</code></td><td><code>{menu.permission_code || 'container'}</code></td><td><Badge status={menu.status} /></td><td><button className="icon-button danger-icon" onClick={() => remove(menu)} aria-label={`删除 ${menu.label}`}><Trash2 size={17} /></button></td></tr>)}</tbody></table>{!menus.length && <Empty>暂无菜单节点</Empty>}</div>
    <Dialog title={editing ? '编辑菜单' : '新建菜单'} open={editing !== undefined} onClose={() => setEditing(undefined)}><form className="form-grid" onSubmit={save}><label>名称<input name="label" defaultValue={editing?.label} required maxLength={128} /></label><label>父级<select name="parent_id" defaultValue={editing?.parent_id}><option value="">根节点</option>{menus.filter((m) => m.id !== editing?.id).map((menu) => <option key={menu.id} value={menu.id}>{menu.label}</option>)}</select></label><label className="full">路由<input name="route" defaultValue={editing?.route} placeholder="/iam/users" maxLength={512} /></label><label>图标名称<input name="icon" defaultValue={editing?.icon} placeholder="Users" maxLength={64} /></label><label>排序<input name="sort_order" type="number" defaultValue={editing?.sort_order ?? 0} min={-100000} max={100000} /></label><label className="full">权限码<select name="permission_code" defaultValue={editing?.permission_code}><option value="">仅作容器</option>{permissions.map((permission) => <option key={permission.code} value={permission.code}>{permission.code} · {permission.summary}</option>)}</select></label><label>状态<select name="status" defaultValue={editing?.status ?? 'active'}><option value="active">启用</option><option value="disabled">停用</option></select></label><FormActions busy={busy} onCancel={() => setEditing(undefined)} /></form></Dialog>
  </>
}
