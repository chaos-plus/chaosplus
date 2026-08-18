import { useEffect, useState } from "react"
import { Building2, Menu as MenuIcon, ShieldCheck } from "lucide-react"
import { Navigate, Outlet, useLocation, useNavigate } from "react-router"
import {
  Sheet,
  SheetContent,
  SheetTrigger,
} from "@workspace/ui/components/sheet"
import { AppSidebar } from "../components/app-sidebar"
import { useAuth } from "../components/auth"
import { ThemeModeButton } from "../components/theme-mode-button"
import { UserMenu } from "../components/user-menu"
import {
  getTenant,
  iamApi,
  setTenant,
  type EffectiveMenu,
  type Tenant,
} from "../lib/iam-api"

export default function AppLayout() {
  const { session, status } = useAuth()
  const [menus, setMenus] = useState<EffectiveMenu[] | null>(null)
  const [tenantValue, setTenantValue] = useState(getTenant())
  const [tenantDraft, setTenantDraft] = useState(getTenant())
  const [platformTenants, setPlatformTenants] = useState<Tenant[] | null>(null)
  const activeTenants = (platformTenants ?? []).filter((t) => t.status === "active")
  const currentTenant = activeTenants.find((t) => t.id === tenantDraft)
  const tenantLabel = currentTenant
    ? `${currentTenant.name} (${currentTenant.slug})`
    : tenantDraft
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    if (status === "authenticated") {
      void iamApi
        .effectiveMenus()
        .then(setMenus)
        .catch(() => setMenus([]))
    }
  }, [status, tenantValue])

  useEffect(() => {
    if (status !== "authenticated") return
    const refresh = () => {
      void iamApi
        .tenants()
        .then(setPlatformTenants)
        .catch(() => setPlatformTenants(null))
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
          <ShieldCheck className="size-8 animate-pulse" />
          正在验证会话
        </div>
      </div>
    )
  if (!session)
    return <Navigate to="/login" replace state={{ from: location.pathname }} />

  const commitTenant = () => {
    const draft = tenantDraft.trim()
    const opt = activeTenants.find(
      (t) => `${t.name} (${t.slug})` === draft || t.name === draft,
    )
    const next = opt ? opt.id : draft
    if (!next || next === tenantValue) return
    setMenus(null)
    setTenant(next)
    setTenantValue(next)
    navigate("/")
  }

  return (
    <div className="flex h-svh flex-col bg-background">
      <header className="sticky top-0 z-40 border-b bg-background shadow-sm">
        <div className="flex h-14 items-center gap-3 px-4">
          <Sheet>
            <SheetTrigger
              aria-label="打开主导航"
              className="grid size-9 place-items-center rounded-md border border-input md:hidden"
            >
              <MenuIcon className="size-5" />
            </SheetTrigger>
            <SheetContent side="left" className="w-72 p-0">
              <AppSidebar
                menus={menus ?? []}
                platformTenantAccess={platformTenants !== null}
                className="block h-full w-full border-0"
              />
            </SheetContent>
          </Sheet>
          <div
            className="flex min-w-0 items-center gap-2 font-semibold"
            aria-label="Chaosplus IAM"
          >
            <span className="grid size-8 shrink-0 place-items-center rounded-md bg-primary text-primary-foreground">
              <ShieldCheck className="size-4" />
            </span>
            <span className="hidden truncate sm:inline">Chaosplus IAM</span>
          </div>
          <div className="ml-auto flex min-w-0 items-center gap-1 sm:gap-2">
            <div className="flex h-9 min-w-0 items-center gap-2 rounded-md border border-input px-2">
              <Building2
                className="size-4 shrink-0 text-muted-foreground"
                aria-hidden="true"
              />
              <label htmlFor="tenant" className="sr-only">
                当前租户
              </label>
              <input
                id="tenant"
                list={platformTenants ? "tenant-options" : undefined}
                className="w-24 min-w-0 bg-transparent text-sm outline-none sm:w-36"
                value={tenantLabel}
                onChange={(event) => setTenantDraft(event.target.value)}
                onBlur={commitTenant}
                onKeyDown={(event) => event.key === "Enter" && commitTenant()}
              />
              {platformTenants && (
                <datalist id="tenant-options">
                  {activeTenants.map((tenant) => (
                    <option
                      key={tenant.id}
                      value={`${tenant.name} (${tenant.slug})`}
                    >
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
        <AppSidebar
          menus={menus ?? []}
          platformTenantAccess={platformTenants !== null}
          className="hidden md:block"
        />
        <main className="min-w-0 flex-1 overflow-auto">
          <div className="mx-auto w-full max-w-[1440px] p-4 sm:p-6">
            <Outlet context={{ menus }} />
          </div>
        </main>
      </div>
    </div>
  )
}
