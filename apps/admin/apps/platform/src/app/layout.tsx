import { useEffect, useState } from "react"
import {
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
import { Navigate, NavLink, Outlet, useLocation, useNavigate } from "react-router"
import { useTranslations } from "use-intl"
import type { Locale } from "@workspace/ui/i18n/config"
import { useClientLocale } from "@workspace/ui/i18n/intl-provider"
import { currentLocale, persistLocale } from "../i18n"
import { Avatar, AvatarFallback } from "@workspace/ui/components/avatar"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu"
import { Toaster } from "@workspace/ui/components/sonner"
import { useAuth } from "../components/auth"
import { ThemeModeButton } from "../components/theme-mode-button"
import { controlApi, type Channel } from "../lib/control-api"
import { getEntity, getTenant, iamApi, setEntity, setTenant, type Entity } from "../lib/iam-api"

interface NavItem {
  /** i18n key,位于 platform.nav.*。 */
  key: string
  path: string
  icon: typeof Hash
}

const LOCALES: Array<{ code: Locale; label: string }> = [
  { code: "zh-CN", label: "简体中文" },
  { code: "en-US", label: "English" },
  { code: "ms-MY", label: "Bahasa Melayu" },
]

/** 顶部一级菜单(PRD 平台域)。 */
const TOP_MENUS: NavItem[] = [
  { key: "dashboard", path: "/", icon: LayoutDashboard },
  { key: "sessions", path: "/sessions", icon: MessagesSquare },
  { key: "workspace", path: "/workspace/requirements", icon: FolderKanban },
  { key: "workflow", path: "/workflow/runs", icon: GitBranch },
  { key: "team", path: "/team/machines", icon: UsersRound },
]

/** 每个一级菜单下的左侧二级菜单。 */
const SECONDARY: Record<string, NavItem[]> = {
  dashboard: [],
  sessions: [{ key: "channels", path: "/sessions", icon: Hash }],
  workspace: [
    { key: "requirements", path: "/workspace/requirements", icon: FolderKanban },
    { key: "tasks", path: "/workspace/tasks", icon: FolderKanban },
    { key: "tests", path: "/workspace/tests", icon: FolderKanban },
    { key: "bugs", path: "/workspace/bugs", icon: FolderKanban },
    { key: "okrs", path: "/workspace/okrs", icon: Gauge },
  ],
  team: [
    { key: "machines", path: "/team/machines", icon: Network },
    { key: "humans", path: "/team/humans", icon: UsersRound },
    { key: "agents", path: "/team/agents", icon: Gauge },
  ],
  workflow: [
    { key: "runs", path: "/workflow/runs", icon: GitBranch },
    { key: "approvals", path: "/workflow/approvals", icon: Gauge },
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
  const t = useTranslations("platform")
  const { session, status } = useAuth()
  const location = useLocation()
  const navigate = useNavigate()
  const top = activeTop(location.pathname)
  const secondary = SECONDARY[top] ?? []

  const [tenantValue, setTenantValue] = useState(getTenant())
  const [entityValue, setEntityValue] = useState(getEntity())
  const [tenantEntities, setTenantEntities] = useState<Entity[]>([])
  const { setLocale } = useClientLocale()
  const [lang, setLang] = useState<Locale>(() => currentLocale())
  const [channels, setChannels] = useState<Channel[]>([])

  useEffect(() => {
    if (status !== "authenticated") return
    // 当前租户:session 里的 organization_id 最可靠(平台级 /iam/tenants 需管理员)。
    if (session?.organization_id) {
      setTenantValue(session.organization_id)
      setTenant(session.organization_id)
    }
    // 登录用户自己的租户(注册即自动创建);平台级 /iam/tenants 需管理员。
    const refresh = () => {
      void iamApi.myTenants().then((x) => {
        const mine = (x ?? [])[0]
        if (mine && !session?.organization_id && !getEntity()) {
          setTenantValue(mine.id)
          setTenant(mine.id)
        }
      }).catch(() => {})
    }
    refresh()
    window.addEventListener("tenant-catalog-change", refresh)
    return () => window.removeEventListener("tenant-catalog-change", refresh)
  }, [status, session?.organization_id])

  useEffect(() => {
    if (!tenantValue) {
      setTenantEntities([])
      return
    }
    const loadEntities = () => {
      void iamApi
        .entities(tenantValue)
        .then((x) => setTenantEntities((x ?? []).filter((en) => en.tenant_id === tenantValue)))
        .catch(() => setTenantEntities([]))
    }
    loadEntities()
    const t = setInterval(loadEntities, 5000)
    return () => clearInterval(t)
  }, [tenantValue])

  useEffect(() => {
    const sync = () => {
      const next = getTenant()
      setTenantValue(next)
    }
    window.addEventListener("tenant-change", sync)
    return () => window.removeEventListener("tenant-change", sync)
  }, [])

  // 动态浏览器 tab 标题:二级菜单 · 一级菜单 · chaos.plus。
  useEffect(() => {
    if (location.pathname.startsWith("/profile")) {
      document.title = `${t("nav.profile")} · chaos.plus`
      return
    }
    const topLabel = t(`nav.${top}`)
    const sub = secondary.find((s) => location.pathname.startsWith(s.path))
    document.title = [sub ? t(`nav.${sub.key}`) : undefined, topLabel, "chaos.plus"].filter(Boolean).join(" · ")
  }, [top, secondary, location.pathname])

  // 顶部头像展示个人中心里设置的昵称。
  const [displayName, setDisplayName] = useState("未登录")
  useEffect(() => {
    if (!tenantValue) {
      setTenantEntities([])
      return
    }
    const loadEntities = () => {
      void iamApi
        .entities(tenantValue)
        .then((x) => setTenantEntities((x ?? []).filter((en) => en.tenant_id === tenantValue)))
        .catch(() => setTenantEntities([]))
    }
    loadEntities()
    const t = setInterval(loadEntities, 5000)
    return () => clearInterval(t)
  }, [tenantValue])

  useEffect(() => {
    const sync = () => {
      try {
        const raw = localStorage.getItem("platform-profile")
        const p = raw ? (JSON.parse(raw) as { nickname?: string; email?: string }) : null
        setDisplayName(p?.nickname || p?.email || "未登录")
      } catch {
        setDisplayName("未登录")
      }
    }
    sync()
    // storage 事件只跨标签页触发,同页保存要靠自定义事件。
    window.addEventListener("storage", sync)
    window.addEventListener("profile-change", sync)
    return () => {
      window.removeEventListener("storage", sync)
      window.removeEventListener("profile-change", sync)
    }
  }, [location.pathname])

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

  // 未登录一律去 /login;登录成功后 finishLogin 会回首页。
  if (status === "anonymous") return <Navigate to="/login" replace />

  // 登录但没有选择实例实体 → 强制去实体创建/加入页;没有实体看不到任何资源。
  const hasEntity = !!getEntity()
  if (status === "authenticated" && !hasEntity && !location.pathname.startsWith("/entities")) {
    return <Navigate to="/entities" replace />
  }

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
                {t(`nav.${m.key}`)}
              </NavLink>
            ))}
          </nav>
          <div className="ml-auto flex min-w-0 items-center gap-1 sm:gap-2">
            {/* 当前实体(instance):租户下的 entity + 新建 */}
            <label htmlFor="entity-select" className="sr-only">当前实体</label>
            <select
              id="entity-select"
              value={entityValue}
              onChange={(e) => {
                const v = e.target.value
                if (v === "__new__") {
                  navigate("/entities")
                  return
                }
                setEntity(v)
                setEntityValue(v)
              }}
              className="h-9 max-w-40 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
            >
              <option value="">选择实体…</option>
              {tenantEntities.map((en) => (
                <option key={en.id} value={en.id}>
                  {en.name}
                </option>
              ))}
              <option value="__new__">＋ 新建实体</option>
            </select>
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
                <DropdownMenuItem onClick={() => navigate("/workspace/requirements")}>平台配置</DropdownMenuItem>
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
                  当前: {session?.preferred_username ?? session?.subject ?? displayName}
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
                {LOCALES.map((l) => (
                  <DropdownMenuItem
                    key={l.code}
                    onClick={() => {
                      setLang(l.code)
                      persistLocale(l.code)
                      setLocale(l.code)
                    }}
                  >
                    {l.label} {lang === l.code && "✓"}
                  </DropdownMenuItem>
                ))}
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
                  {session?.preferred_username ?? session?.subject ?? displayName}
                </span>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                <DropdownMenuLabel>{session?.email ?? "本地单用户"}</DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onClick={() => navigate("/profile")}>个人中心</DropdownMenuItem>
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
      <div className="flex min-h-0 flex-1 flex-col md:flex-row">
        {secondary.length > 0 && (
          <nav
            className="flex w-full shrink-0 gap-1 overflow-x-auto border-b bg-background p-2 md:w-56 md:flex-col md:overflow-visible md:border-r md:border-b-0"
            aria-label={`${t(`nav.${top}`)} 二级菜单`}
          >
            {secondary.map((item) => (
              <NavLink
                key={item.path}
                to={item.path}
                className={({ isActive }) =>
                  `flex shrink-0 cursor-pointer items-center gap-2 whitespace-nowrap rounded-md px-3 py-2 text-sm font-medium transition-colors md:mb-0.5 ${
                    isActive
                      ? "bg-accent text-accent-foreground"
                      : "text-muted-foreground hover:bg-accent/50 hover:text-accent-foreground"
                  }`
                }
              >
                <item.icon className="size-4" aria-hidden="true" />
                {t(`nav.${item.key}`)}
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
          <div className="mx-auto w-full max-w-[1440px] p-4 pb-20 sm:p-6 md:pb-6">
            <Outlet />
          </div>
        </main>
      </div>

      {/* 手机端底部导航(PRD V1-M2:桌面顶部 / 手机底部)。 */}
      <nav
        aria-label="底部导航"
        className="fixed inset-x-0 bottom-0 z-40 border-t bg-background pb-[env(safe-area-inset-bottom)] md:hidden"
      >
        <div className="flex items-stretch">
          {TOP_MENUS.map((m) => (
            <NavLink
              key={m.path}
              to={m.path}
              className={({ isActive }) =>
                `flex min-h-[56px] flex-1 cursor-pointer flex-col items-center justify-center gap-0.5 px-1 text-[11px] font-medium transition-colors ${
                  isActive ? "text-primary" : "text-muted-foreground hover:text-accent-foreground"
                }`
              }
            >
              <m.icon className="size-5" aria-hidden="true" />
              <span className="truncate">{t(`nav.${m.key}`)}</span>
            </NavLink>
          ))}
        </div>
      </nav>
      <Toaster />
    </div>
  )
}
