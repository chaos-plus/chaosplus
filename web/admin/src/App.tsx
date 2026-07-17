import { useEffect, useMemo, useState } from 'react'
import { Navigate, NavLink, Outlet, Route, Routes, useLocation, useNavigate } from 'react-router-dom'
import { Boxes, ChevronDown, KeyRound, LayoutDashboard, LogOut, Menu as MenuIcon, PanelLeftClose, ShieldCheck, Users, X } from 'lucide-react'
import { api, ApiError, getTenant, oidcURL, setTenant } from './api'
import { SessionContext } from './auth'
import type { Menu, Session } from './types'
import LoginPage from './pages/LoginPage'
import DashboardPage from './pages/DashboardPage'
import UsersPage from './pages/UsersPage'
import RolesPage from './pages/RolesPage'
import MenusPage from './pages/MenusPage'

function ProtectedLayout() {
  const [session, setSession] = useState<Session | null>()
  const [menus, setMenus] = useState<Menu[]>([])
  const [menuState, setMenuState] = useState<'loading' | 'ready' | 'error'>('loading')
  const [drawer, setDrawer] = useState(false)
  const [tenant, setTenantCommitted] = useState(getTenant())
  const [tenantDraft, setTenantDraft] = useState(getTenant())
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => { api.session().then(setSession).catch(() => setSession(null)) }, [])
  useEffect(() => {
    if (!session) return
    let stale = false
    setMenuState('loading')
    api.effectiveMenus().then((data) => {
      if (stale) return
      setMenus(data ?? [])
      setMenuState('ready')
    }).catch((error) => {
      if (stale) return
      if (error instanceof ApiError && error.status === 401) { setSession(null); return }
      setMenus([])
      setMenuState('error')
    })
    return () => { stale = true }
  }, [session, tenant])
  useEffect(() => setDrawer(false), [location.pathname])

  if (session === undefined) return <div className="app-loading"><ShieldCheck size={30} /><span>正在验证会话</span></div>
  if (!session) return <Navigate to="/login" replace state={{ from: location.pathname }} />

  const changeTenant = () => {
    const value = tenantDraft.trim()
    if (!value || value === tenant) return
    setTenant(value)
    setTenantCommitted(value)
    navigate('/')
  }
  const logout = async () => {
    const result = await api.logout().catch(() => null)
    setSession(null)
    if (result?.logout_url) window.location.assign(result.logout_url)
  }

  return <SessionContext.Provider value={session}>
    <div className="app-shell">
      <aside className={`sidebar ${drawer ? 'sidebar-open' : ''}`}>
        <div className="brand"><span className="brand-mark"><ShieldCheck size={20} /></span><strong>Chaosplus</strong><button className="icon-button sidebar-close" onClick={() => setDrawer(false)} aria-label="关闭导航"><PanelLeftClose size={19} /></button></div>
        <nav className="nav-list" aria-label="主导航">
          <NavLink to="/" end><LayoutDashboard size={18} /><span>概览</span></NavLink>
          <MenuTree items={menus} />
          {menuState === 'loading' && menus.length === 0 && <div className="nav-fallback">菜单加载中…</div>}
          {menuState === 'error' && <div className="nav-fallback">菜单加载失败，请刷新重试</div>}
          {menuState === 'ready' && menus.length === 0 && <div className="nav-fallback">当前租户暂无可用菜单</div>}
        </nav>
        <div className="sidebar-user"><span className="avatar">{(session.preferred_username ?? session.email ?? session.subject).slice(0, 1).toUpperCase()}</span><div><strong>{session.preferred_username ?? '当前用户'}</strong><small>{session.email ?? session.subject}</small></div><button className="icon-button" onClick={logout} aria-label="退出登录"><LogOut size={18} /></button></div>
      </aside>
      {drawer && <button className="mobile-scrim" onClick={() => setDrawer(false)} aria-label="关闭导航" />}
      <main className="main-area">
        <header className="topbar"><button className="icon-button mobile-menu" onClick={() => setDrawer(true)} aria-label="打开导航"><MenuIcon size={20} /></button><div className="tenant-control"><label htmlFor="tenant">当前租户</label><input id="tenant" value={tenantDraft} onChange={(event) => setTenantDraft(event.target.value)} onBlur={changeTenant} onKeyDown={(event) => event.key === 'Enter' && changeTenant()} /><ChevronDown size={15} /></div><span className="subject-chip">{session.subject}</span></header>
        <div className="page-content"><Outlet /></div>
      </main>
    </div>
  </SessionContext.Provider>
}

function MenuTree({ items, depth = 0 }: { items: Menu[]; depth?: number }) {
  return <>{items.map((item) => <div key={item.id} className="nav-group">
    {item.route ? <NavLink to={item.route} style={{ paddingLeft: 14 + depth * 14 }}><NavIcon path={item.route} /><span>{item.label}</span></NavLink> : <div className="nav-label" style={{ paddingLeft: 14 + depth * 14 }}>{item.label}</div>}
    {item.children && <MenuTree items={item.children} depth={depth + 1} />}
  </div>)}</>
}

function NavIcon({ path }: { path: string }) {
  if (path.includes('users')) return <Users size={18} />
  if (path.includes('roles')) return <KeyRound size={18} />
  if (path.includes('menus')) return <Boxes size={18} />
  return <LayoutDashboard size={18} />
}

export default function App() {
  return <Routes>
    <Route path="/login" element={<LoginPage mode="login" />} />
    <Route path="/register" element={<LoginPage mode="register" />} />
    <Route element={<ProtectedLayout />}>
      <Route index element={<DashboardPage />} />
      <Route path="/iam/users" element={<UsersPage />} />
      <Route path="/iam/roles" element={<RolesPage />} />
      <Route path="/iam/menus" element={<MenusPage />} />
    </Route>
    <Route path="*" element={<Navigate to="/" replace />} />
  </Routes>
}
