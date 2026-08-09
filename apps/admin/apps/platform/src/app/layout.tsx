import { useEffect, useState } from "react"
import {
  Building2,
  FolderKanban,
  Gauge,
  GitBranch,
  Hash,
  Languages,
  LayoutDashboard,
  MessagesSquare,
  Network,
  Repeat2,
  Settings,
  UsersRound,
} from "lucide-react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router"
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu"
import { useAuth } from "../components/auth"
import { ThemeModeButton } from "../components/theme-mode-button"
import { controlApi, type Channel } from "../lib/control-api"
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
  { label: "工作流", path: "/workflow/runs", icon: GitBranch },
  { label: "团队管理", path: "/team/machines", icon: UsersRound },
]

/** 每个一级菜单下的左侧二级菜单。 */
const SECONDARY: Record<string, NavItem[]> = {
  dashboard: [],
  sessions: [{ label: "频道", path: "/sessions", icon: Hash }],
  workspace: [{ label: "工作区", path: "/workspace", icon: FolderKanban }],
  team: [
    { label: "机器 Machines", path: "/team/machines", icon: Network },
    { label: "人类 Human", path: "/team/humans", icon: UsersRound },
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
  const [lang, setLang] = useState<string>(() => localStorage.getItem("platform-lang") ?? "zh")
  const [channels, setChannels] = useState<Channel[]>([])

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

  // 会话区左侧菜单:实时列出频道。
  useEffect(() => {
    if (top !== "sessions") return
    const load = () => {
      void controlApi.channels().then((x) => setChannels(x ?? [])).catch(() => setChannels([]))
    }
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [top])

  // 本地单用户工具:无登录体系,shell 直接渲染(租户默认 platform)。
  if (status === "loading" && session)
    return (
      <div className="grid min-h-svh place-items-center bg-background text-sm text-muted-foreground">
        <div className="grid justify-items-center gap-3">
          <GitBranch className="size-8 animate-pulse" />
          正在验证会话
        </div>
      </div>
    )

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
            {/* 配置管理 */}
            <DropdownMenu>
              <DropdownMenuTrigger
                aria-label="配置管理"
                className="grid size-9 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Settings className="size-4" aria-hidden="true" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel>配置管理</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => navigate("/workspace")}>平台设置</DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigate("/team/machines")}>接入配置</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            {/* 主体切换 */}
            <DropdownMenu>
              <DropdownMenuTrigger
                aria-label="主体切换"
                className="grid size-9 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Repeat2 className="size-4" aria-hidden="true" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-48">
                <DropdownMenuLabel>主体</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled>
                  当前: {session?.preferred_username ?? session?.subject ?? "未登录"}
                </DropdownMenuItem>
                <DropdownMenuItem disabled className="text-xs text-muted-foreground">
                  主体由 IAM 身份体系提供
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            {/* 语言切换 */}
            <DropdownMenu>
              <DropdownMenuTrigger
                aria-label="语言切换"
                className="grid size-9 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
              >
                <Languages className="size-4" aria-hidden="true" />
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-32">
                <DropdownMenuItem onClick={() => { setLang("zh"); localStorage.setItem("platform-lang", "zh") }}>
                  简体中文 {lang === "zh" && "✓"}
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => { setLang("en"); localStorage.setItem("platform-lang", "en") }}>
                  English {lang === "en" && "✓"}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            {/* 登录用户头像 + 用户名 + 下拉 */}
            <DropdownMenu>
              <DropdownMenuTrigger className="flex items-center gap-2 rounded-full p-1 pr-2 transition-colors hover:bg-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring">
                <Avatar className="size-8">
                  <AvatarFallback className="bg-gradient-to-br from-primary to-accent text-xs text-primary-foreground">
                    {(session?.preferred_username ?? session?.subject ?? "U").slice(0, 1).toUpperCase()}
                  </AvatarFallback>
                </Avatar>
                <span className="hidden max-w-28 truncate text-sm font-medium sm:inline">
                  {session?.preferred_username ?? session?.subject ?? "未登录"}
                </span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel>{session?.email ?? "本地单用户"}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => navigate("/")}>个人中心</DropdownMenuItem>
                <DropdownMenuItem onClick={() => navigate("/team/machines")}>我的机器</DropdownMenuItem>
                <DropdownMenuSeparator />
                <DropdownMenuItem disabled className="text-muted-foreground">
                  {session ? "退出登录" : "本地无登录"}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>

            <ThemeModeButton />
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
            {top === "sessions" && (
              <div className="mt-2 space-y-0.5 border-t pt-2">
                {channels.map((c) => (
                  <NavLink
                    key={c.id}
                    to={`/sessions/${c.id}`}
                    className={({ isActive }) =>
                      `mb-0.5 flex items-center gap-2 rounded-md px-3 py-1.5 text-sm transition-colors ${
                        isActive
                          ? "bg-accent/60 text-accent-foreground"
                          : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                      }`
                    }
                  >
                    <Hash className="size-3.5 shrink-0" aria-hidden="true" />
                    <span className="truncate">{c.name}</span>
                  </NavLink>
                ))}
              </div>
            )}
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
