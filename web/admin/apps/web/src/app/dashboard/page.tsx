import { useEffect, useState } from "react"
import { KeyRound, MonitorSmartphone, Network, Users } from "lucide-react"
import { useOutletContext } from "react-router"
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import { iamApi, type EffectiveMenu } from "../../lib/iam-api"
import { effectiveMenuPaths } from "../../lib/navigation"

interface Metrics {
  principals?: number
  roles?: number
  menus?: number
  sessions: number
}

export default function DashboardPage() {
  const { menus } = useOutletContext<{ menus: EffectiveMenu[] | null }>()
  const [metrics, setMetrics] = useState<Metrics>({
    sessions: 0,
  })
  const [error, setError] = useState("")
  useEffect(() => {
    if (menus === null) return
    const paths = effectiveMenuPaths(menus)
    let active = true
    void Promise.all([
      paths.has("/iam/users")
        ? iamApi.principals().then((result) => result.total)
        : undefined,
      paths.has("/iam/roles")
        ? iamApi.roles().then((result) => result.length)
        : undefined,
      paths.has("/iam/menus")
        ? iamApi.menus().then((result) => result.length)
        : undefined,
      iamApi.sessions().then((result) => result.length),
    ])
      .then(([principals, roles, menuCount, sessions]) => {
        if (active) {
          setError("")
          setMetrics({ principals, roles, menus: menuCount, sessions })
        }
      })
      .catch((cause: Error) => {
        if (active) setError(cause.message)
      })
    return () => {
      active = false
    }
  }, [menus])
  return (
    <>
      <PageHeader
        title="身份控制面"
        description="平台身份、授权和会话的实时状态"
      />
      {error && <Alert>{error}</Alert>}
      <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
        {metrics.principals !== undefined && (
          <Metric
            icon={<Users />}
            label="本地主体"
            value={metrics.principals}
          />
        )}
        {metrics.roles !== undefined && (
          <Metric icon={<KeyRound />} label="租户角色" value={metrics.roles} />
        )}
        {metrics.menus !== undefined && (
          <Metric icon={<Network />} label="菜单节点" value={metrics.menus} />
        )}
        <Metric
          icon={<MonitorSmartphone />}
          label="我的会话"
          value={metrics.sessions}
        />
      </div>
      <section className="mt-6 flex min-h-24 flex-wrap items-center justify-between gap-4 rounded-md border bg-card px-5 py-4">
        <div>
          <h2 className="text-sm font-semibold">授权状态</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            权限变更按请求实时计算，不依赖会话内角色快照。
          </p>
        </div>
        <span className="flex items-center gap-2 text-xs font-semibold text-success">
          <i className="size-2 rounded-full bg-success ring-4 ring-success/10" />
          策略引擎在线
        </span>
      </section>
    </>
  )
}

function Metric({
  icon,
  label,
  value,
}: {
  icon: React.ReactNode
  label: string
  value: number
}) {
  return (
    <article className="flex min-h-28 items-center gap-4 rounded-md border bg-card p-5">
      <span className="grid size-10 place-items-center rounded-md bg-primary/10 text-primary [&>svg]:size-5">
        {icon}
      </span>
      <div>
        <small className="text-muted-foreground">{label}</small>
        <strong className="mt-1 block text-2xl">{value}</strong>
      </div>
    </article>
  )
}
