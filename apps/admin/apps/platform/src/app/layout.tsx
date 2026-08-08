import { useEffect, useState } from "react"
import {
  Building2,
  FolderKanban,
  Gauge,
  GitBranch,
  Hash,
  LayoutDashboard,
  MessagesSquare,
  Network,
  Users,
} from "lucide-react"
import { Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router"
import { useAuth } from "../components/auth"
import { ThemeModeButton } from "../components/theme-mode-button"
import { UserMenu } from "../components/user-menu"
import { getTenant, iamApi, setTenant, type Tenant } from "../lib/iam-api"

interface NavItem {
  label: string
  path: string
  icon: typeof Hash
}

/** 顶部一级菜单(PRD 平台域)。 */
const TOP_MENUS: NavItem[] = [
  { label: "仪表盘", path: "/", icon: LayoutDashboard },
  { label: "会话区", path: "/sessions", icon: MessagesSquare },
  { label: "工作区", path: "/workspace", icon: FolderKanban },
  { label: "团队管理", path: "/team/machines", icon: Users },
  { label: "工作流", path: "/workflow/runs", icon: GitBranch },
]

/** 每个一级菜单下的左侧二级菜单。 */
const SECONDARY: Record<string, NavItem[]> = {
  dashboard: [],
  sessions: [{ label: "频道", path: "/sessions", icon: Hash }],
  workspace: [{ label: "工作区", path: "/workspace", icon: FolderKanban }],
  team: [
    { label: "机器 Machines", path: "/team/machines", icon: Network },
    { label: "人类 Human", path: "/team/humans", icon: Users },
    { label: "Agent", path: "/team/agents", icon: Gauge },
  ],
  workflow: [
    { label: "运行 Runs", path: "/workflow/runs", icon: GitBranch },
    { label: "审批 Approvals", path: "/workflow/approvals", icon: Gauge },
  ],
}

function activeTop(pathname: string): string {
  if (pathname.startsWith("/sessions")) return "sessions"
  if (pathname.startsWith("/workspace")) return "workspace"
  if (pathname.startsWith("/team")) return "team"
  if (pathname.startsWith("/workflow")) return "workflow"
  return "dashboard"
}

export default function PlatformLayout() {
  const { session, status } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const top = activeTop(location.pathname)
  const secondary = SECONDARY[top] ?? []

  const [tenantDraft, setTenantDraft] = useState(getTenant())
  const [tenantValue, setTenantValue] = useState(getTenant())
  const [platformTenants, setPlatformTenants] = useState<Tenant[] | null>(null)

  useEffect(() => {
    if (status !== "authenticated") return
    const refresh = () => {
      void iamApi.tenants().then(setPlatformTenants).catch(() => setPlatformTenants(null))
    }
    refresh()
    window.addEventListener("tenant-catalog-change", refresh)
    return () => window.removeEventListener("tenant-catalog-change", refresh)
  }, [status])

  useEffect(() => {
    const sync = () => {
      const next = getTenant()
      setTenantValue(next)
      setTenantDraft(next)
    }
    window.addEventListener("tenant-change", sync)
    return () => window.removeEventListener("tenant-change", sync)
  }, [])

  if (status === "loading")
    return (
      <div className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        <div className="grid justify-items-center gap-3">
          <GitBranch className="size-8 animate-pulse" />
          正在验证会话
        </div>
      </div>
    )
  if (!session) return <Navigate to="/login" replace state={{ from: location.pathname }} />

  const commitTenant = () => {
    const next = tenantDraft.trim()
    if (!next || next === tenantValue) return
    setTenant(next)
    setTenantValue(next)
    setTenantDraft(next)
    navigate("/")
  }

  return (
    <div className="flex h-svh flex-col bg-background">
      <header className="sticky top-0 z-40 border-b bg-background shadow-sm">
        <div className="flex h-14 items-center gap-3 px-4">
          <div className="flex min-w-0 items-center gap-2 font-semibold" aria-label="chaos.plus">
            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
              <GitBranch className="size-4" />
            </span>
            <span className="hidden truncate sm:inline">chaos.plus</span>
          </div>
          <nav className="ml-4 hidden min-w-0 items-center gap-1 md:flex" aria-label="一级菜单">
            {TOP_MENUS.map((m) => (
              <NavLink
                key={m.path}
                to={m.path}
                className={({ isActive }) =>
                  `inline-flex h-9 items-center gap-1.5 rounded-md px-3 text-sm font-medium transition-colors ${
                    isActive
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                  }`
                }
              >
                <m.icon className="size-4" aria-hidden="true" />
                {m.label}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex min-w-0 items-center gap-1 sm:gap-2">
            <div className="flex h-9 min-w-0 items-center gap-2 rounded-md border border-input px-2">
              <Building2 className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
              <label htmlFor="tenant" className="sr-only">
                当前租户
              </label>
              <input
                id="tenant"
                list={platformTenants ? "tenant-options" : undefined}
                className="w-24 min-w-0 bg-transparent text-sm outline-none sm:w-36"
                value={tenantDraft}
                onChange={(event) => setTenantDraft(event.target.value)}
                onBlur={commitTenant}
                onKeyDown={(event) => event.key === "Enter" && commitTenant()}
              />
              {platformTenants && (
                <datalist id="tenant-options">
                  {platformTenants
                    .filter((tenant) => tenant.status === "active")
                    .map((tenant) => (
                      <option key={tenant.id} value={tenant.id}>
                        {tenant.name} ({tenant.slug})
                      </option>
                    ))}
                </datalist>
              )}
            </div>
            <ThemeModeButton />
            <UserMenu />
          </div>
        </div>
      </header>
      <div className="flex min-h-0 flex-1">
        {secondary.length > 0 && (
          <nav
            className="w-56 shrink-0 border-r bg-background p-2"
            aria-label={`${TOP_MENUS.find((m) => m.path === top)?.label ?? ""} 二级菜单`}
          >
            {secondary.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                className={({ isActive }) =>
                  `mb-0.5 flex items-center gap-2 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
                    isActive
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                  }`
                }
              >
                <item.icon className="size-4" aria-hidden="true" />
                {item.label}
              </NavLink>
            ))}
          </nav>
        )}
        <main className="min-w-0 flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-[1440px] p-4 sm:p-6">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
