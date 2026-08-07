import {
  AppWindow,
  Bot,
  Boxes,
  BriefcaseBusiness,
  Building2,
  ClipboardCheck,
  FileClock,
  KeyRound,
  Landmark,
  LayoutDashboard,
  ListChecks,
  MonitorSmartphone,
  MailPlus,
  Network,
  FolderSync,
  Users,
  UsersRound,
} from "lucide-react"
import { NavLink } from "react-router"
import type { EffectiveMenu } from "../lib/iam-api"
import { effectiveMenuPaths, flattenEffectiveMenus } from "../lib/navigation"

const administrationRoutes = [
  { path: "/iam/users", icon: <Users />, label: "主体与成员" },
  { path: "/iam/invitations", icon: <MailPlus />, label: "成员邀请" },
  { path: "/iam/service-accounts", icon: <Bot />, label: "服务账号" },
  { path: "/iam/entities", icon: <Network />, label: "实体管理" },
  { path: "/iam/departments", icon: <Building2 />, label: "部门管理" },
  { path: "/iam/positions", icon: <BriefcaseBusiness />, label: "岗位管理" },
  { path: "/iam/groups", icon: <UsersRound />, label: "用户组管理" },
  { path: "/iam/roles", icon: <KeyRound />, label: "角色权限" },
  { path: "/iam/access-reviews", icon: <ListChecks />, label: "访问复核" },
  { path: "/iam/menus", icon: <Boxes />, label: "菜单管理" },
  { path: "/iam/oauth-clients", icon: <AppWindow />, label: "OAuth 客户端" },
  { path: "/iam/scim-directories", icon: <FolderSync />, label: "SCIM 目录" },
  { path: "/iam/audit-events", icon: <FileClock />, label: "审计日志" },
]
const administrationPaths = new Set([
  ...administrationRoutes.map((route) => route.path),
  "/iam/access-requests",
])

export function AppSidebar({
  menus,
  platformTenantAccess = false,
  className = "",
}: {
  menus: EffectiveMenu[]
  platformTenantAccess?: boolean
  className?: string
}) {
  const authorizedPaths = effectiveMenuPaths(menus)
  const authorizedAdministration = administrationRoutes.filter((route) =>
    authorizedPaths.has(route.path)
  )
  const customMenus = flattenEffectiveMenus(menus).filter(
    (menu) => menu.path && !administrationPaths.has(menu.path)
  )

  return (
    <aside
      className={`w-60 shrink-0 overflow-y-auto border-e border-sidebar-border bg-sidebar p-3 ${className}`}
    >
      <nav className="space-y-4" aria-label="主导航">
        <div className="space-y-1">
          <SideLink to="/" icon={<LayoutDashboard />} label="概览" />
        </div>
        {platformTenantAccess && (
          <div>
            <h2 className="mb-2 px-3 text-xs font-semibold text-sidebar-foreground/60 uppercase">
              平台
            </h2>
            <div className="space-y-1">
              <SideLink
                to="/iam/tenants"
                icon={<Landmark />}
                label="租户管理"
              />
            </div>
          </div>
        )}
        {authorizedAdministration.length > 0 && (
          <div>
            <h2 className="mb-2 px-3 text-xs font-semibold text-sidebar-foreground/60 uppercase">
              身份与访问
            </h2>
            <div className="space-y-1">
              {authorizedAdministration.map((route) => (
                <SideLink
                  key={route.path}
                  to={route.path}
                  icon={route.icon}
                  label={route.label}
                />
              ))}
            </div>
          </div>
        )}
        {customMenus.length > 0 && (
          <div>
            <h2 className="mb-2 px-3 text-xs font-semibold text-sidebar-foreground/60 uppercase">
              授权菜单
            </h2>
            <div className="space-y-1">
              {customMenus.map((item) => (
                <SideLink
                  key={item.id}
                  to={item.path!}
                  icon={<LayoutDashboard />}
                  label={item.label}
                />
              ))}
            </div>
          </div>
        )}
        <div>
          <h2 className="mb-2 px-3 text-xs font-semibold text-sidebar-foreground/60 uppercase">
            账户
          </h2>
          <div className="space-y-1">
            <SideLink
              to="/iam/access-requests"
              icon={<ClipboardCheck />}
              label="访问申请"
            />
            <SideLink
              to="/security"
              icon={<MonitorSmartphone />}
              label="安全中心"
            />
          </div>
        </div>
      </nav>
    </aside>
  )
}

function SideLink({
  to,
  icon,
  label,
}: {
  to: string
  icon: React.ReactElement
  label: string
}) {
  return (
    <NavLink
      to={to}
      end={to === "/"}
      className={({ isActive }) =>
        `flex min-h-10 items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors ${isActive ? "sidebar-active-item bg-sidebar-primary font-medium text-sidebar-primary-foreground" : "text-sidebar-foreground/90 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"}`
      }
    >
      <span className="shrink-0 [&>svg]:size-4" aria-hidden="true">
        {icon}
      </span>
      <span className="truncate">{label}</span>
    </NavLink>
  )
}
