import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { useTranslations } from "use-intl"
import { Activity, CircleDollarSign, Network, ShieldQuestion } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { controlApi, type DashboardStats } from "../../lib/control-api"

const RUN_STATUS_LABEL: Record<string, string> = {
  running: "进行中",
  waiting_approval: "待审批",
  paused: "已暂停",
  failed: "失败",
  completed: "已完成",
}

function heartbeatText(ts: number): string {
  if (!ts) return "无心跳"
  const secs = Math.max(0, Math.round((Date.now() - ts) / 1000))
  if (secs < 60) return `${secs}s 前`
  if (secs < 3600) return `${Math.round(secs / 60)} 分钟前`
  return `${Math.round(secs / 3600)} 小时前`
}

export default function DashboardPage() {
  const t = useTranslations("platform.dashboard")
  const navigate = useNavigate()
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [failed, setFailed] = useState(false)

  const load = useCallback(() => {
    void controlApi
      .dashboard()
      .then((s) => {
        setStats(s)
        setFailed(false)
      })
      .catch(() => setFailed(true))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const byStatus = stats?.runsByStatus ?? {}
  const activeRuns = (byStatus.running ?? 0) + (byStatus.waiting_approval ?? 0) + (byStatus.paused ?? 0)
  const totalRuns = Object.values(byStatus).reduce((a, b) => a + b, 0)
  const isEmpty = !failed && stats !== null && totalRuns === 0 && stats.machinesTotal === 0

  if (isEmpty) {
    return (
      <div className="space-y-4">
        <h1 className="text-xl font-semibold">{t("title")}</h1>
        <div className="rounded-xl border border-dashed p-8">
          <p className="text-sm text-muted-foreground">{t("emptyHint")}</p>
          <ol className="mt-4 grid gap-3 sm:grid-cols-3">
            {[
              { n: 1, title: "创建频道", desc: "会话区里开一个项目频道", to: "/sessions" },
              { n: 2, title: "接入 machine", desc: "在团队管理里生成接入命令", to: "/team/machines" },
              { n: 3, title: "创建数字人", desc: "配置 agent 的职责与运行时", to: "/team/agents" },
            ].map((s) => (
              <li key={s.n}>
                <button
                  onClick={() => navigate(s.to)}
                  className="w-full cursor-pointer rounded-lg border p-4 text-left transition-colors hover:border-primary/40 hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                >
                  <span className="grid size-6 place-items-center rounded-full bg-primary text-xs text-primary-foreground">{s.n}</span>
                  <p className="mt-2 font-medium">{s.title}</p>
                  <p className="text-sm text-muted-foreground">{s.desc}</p>
                </button>
              </li>
            ))}
          </ol>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">{t("title")}</h1>
      {failed && <p className="text-sm text-destructive">控制面暂时无法访问,显示的是上次数据。</p>}

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-1.5 text-sm">
              <Activity className="size-4 text-muted-foreground" />
              活跃 run
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold tabular-nums">{activeRuns}</p>
            <div className="mt-2 flex flex-wrap gap-1.5 text-xs text-muted-foreground">
              {["running", "waiting_approval", "paused", "failed"].map((k) =>
                byStatus[k] ? (
                  <span key={k} className="rounded-full bg-muted px-2 py-0.5">
                    {RUN_STATUS_LABEL[k]} {byStatus[k]}
                  </span>
                ) : null,
              )}
              {totalRuns === 0 && <span>{t("none")}</span>}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-1.5 text-sm">
              <Network className="size-4 text-muted-foreground" />
              Runner 健康
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold tabular-nums">
              {stats?.machinesOnline ?? 0}
              <span className="text-base font-normal text-muted-foreground"> / {stats?.machinesTotal ?? 0} {t("online")}</span>
            </p>
            <p className="mt-2 text-xs text-muted-foreground">{t("lastHeartbeat")}:{heartbeatText(stats?.lastHeartbeatAt ?? 0)}</p>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-1.5 text-sm">
              <ShieldQuestion className="size-4 text-muted-foreground" />
              待审批
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold tabular-nums">{stats?.pendingApprovals.length ?? 0}</p>
            <div className="mt-2 space-y-1">
              {(stats?.pendingApprovals ?? []).slice(0, 3).map((p) => (
                <button
                  key={`${p.runId}-${p.nodeId}`}
                  onClick={() => navigate(p.channelId ? `/sessions/${p.channelId}` : `/workflow/runs/${p.runId}`)}
                  className="flex min-h-11 w-full cursor-pointer items-center rounded px-2 text-left text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                  title={`${p.title || p.runId} · ${p.nodeId}`}
                >
                  <span className="truncate">{p.title || p.runId} · {p.nodeId}</span>
                </button>
              ))}
              {(stats?.pendingApprovals.length ?? 0) === 0 && <span className="text-xs text-muted-foreground">{t("none")}</span>}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader className="pb-2">
            <CardTitle className="flex items-center gap-1.5 text-sm">
              <CircleDollarSign className="size-4 text-muted-foreground" />
              今日成本
            </CardTitle>
          </CardHeader>
          <CardContent>
            <p className="text-2xl font-semibold tabular-nums">${(stats?.costTodayUsd ?? 0).toFixed(4)}</p>
            <p className="mt-2 text-xs text-muted-foreground">按 agent 上报的 costUsd 汇总</p>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
