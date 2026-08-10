import { useCallback, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { ArrowLeft, Bot, Copy, Eye, RefreshCw, Search } from "lucide-react"
import { useTranslations } from "use-intl"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs"
import { controlApi, machineConnectCommand, type MachineDetail } from "../../../lib/control-api"

/** agent status → platform.machines.* 文案 key。 */
const AGENT_STATUS_KEY: Record<string, string> = {
  running: "machines.agentRunning",
  stopped: "machines.agentStopped",
  retired: "machines.agentRetired",
}

function timeText(ts: number): string {
  return ts ? new Date(ts).toLocaleString() : "—"
}

export default function MachineDetailPage() {
  const { machineId = "" } = useParams()
  const navigate = useNavigate()
  const t = useTranslations("platform")
  const [detail, setDetail] = useState<MachineDetail | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [filter, setFilter] = useState("")
  const [command, setCommand] = useState("")

  const load = useCallback(() => {
    if (!machineId) return
    void controlApi
      .machineDetail(machineId)
      .then((d) => {
        setDetail(d)
        setNotFound(false)
      })
      .catch(() => setNotFound(true))
  }, [machineId])

  useEffect(() => {
    load()
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [load])

  // 只读查看当前接入命令:不轮换、不踢守护进程。控制面重启后内存无原始 token → 提示轮换。
  const viewCommand = async () => {
    try {
      const r = await controlApi.machineToken(machineId)
      setCommand(machineConnectCommand(r.token))
    } catch {
      toast.error(t("machines.noToken"))
    }
  }

  // 轮换接入命令:重新签发长期 token,旧 token 失效、旧守护进程被踢下线。
  const refreshCommand = async () => {
    try {
      const r = await controlApi.refreshToken(machineId)
      setCommand(machineConnectCommand(r.token))
      toast.success(t("machines.rotated"))
    } catch (e) {
      toast.error(`${t("machines.rotateFailed")}:${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const copyCommand = () => {
    if (!command) return
    void navigator.clipboard.writeText(command).then(
      () => toast.success(t("machines.copied")),
      () => toast.error(t("machines.copyFailed"))
    )
  }

  if (notFound) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" className="cursor-pointer gap-1.5" onClick={() => navigate("/team/machines")}>
          <ArrowLeft className="size-4" />
          {t("machines.backToList")}
        </Button>
        <p className="text-sm text-muted-foreground">{t("machines.notFound")}</p>
      </div>
    )
  }
  if (!detail) return <div className="text-sm text-muted-foreground">{t("common.loading")}</div>

  const agents = detail.agents.filter((a) => !filter || a.name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" className="cursor-pointer gap-1.5" onClick={() => navigate("/team/machines")}>
          <ArrowLeft className="size-4" />
          {t("machines.back")}
        </Button>
        <h1 className="text-xl font-semibold">{detail.name}</h1>
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-medium ${
            detail.online ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400" : "bg-muted text-muted-foreground"
          }`}
        >
          {detail.online ? t("dashboard.online") : t("machines.offline")}
        </span>
      </div>

      <Tabs defaultValue="info">
        <TabsList>
          <TabsTrigger value="info" className="cursor-pointer">{t("machines.tabInfo")}</TabsTrigger>
          <TabsTrigger value="runtime" className="cursor-pointer">{t("machines.tabRuntime")}</TabsTrigger>
          <TabsTrigger value="agents" className="cursor-pointer">{t("machines.tabAgents", { count: detail.agents.length })}</TabsTrigger>
        </TabsList>

        <TabsContent value="info">
          <Card className="p-4">
            <dl className="grid gap-3 sm:grid-cols-2">
              {[
                [t("machines.fieldId"), detail.id],
                [t("machines.fieldAddress"), detail.address || "—"],
                [t("machines.fieldOs"), detail.os || t("machines.unreported")],
                [t("machines.fieldStatus"), detail.status],
                [t("machines.fieldRegisteredAt"), timeText(detail.registeredAt)],
                [t("machines.fieldLastHeartbeat"), timeText(detail.lastHeartbeatAt)],
              ].map(([k, v]) => (
                <div key={k}>
                  <dt className="text-xs text-muted-foreground">{k}</dt>
                  <dd className="text-sm break-all">{v}</dd>
                </div>
              ))}
            </dl>

            <div className="mt-4 border-t pt-4">
              <p className="text-sm font-medium">{t("machines.connectCommand")}</p>
              <p className="mt-0.5 text-xs text-muted-foreground">{t("machines.connectCommandHint")}</p>
              {command && (
                <div className="mt-2 space-y-2">
                  <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">{command}</pre>
                  <Button size="sm" variant="outline" className="cursor-pointer gap-1.5" onClick={copyCommand}>
                    <Copy className="size-3.5" />
                    {t("machines.copyCommand")}
                  </Button>
                </div>
              )}
              <div className="mt-2 flex flex-wrap gap-2">
                <Button size="sm" variant="outline" className="cursor-pointer gap-1.5" onClick={viewCommand}>
                  <Eye className="size-3.5" />
                  {t("machines.viewCommand")}
                </Button>
                <Button size="sm" variant="outline" className="cursor-pointer gap-1.5" onClick={refreshCommand}>
                  <RefreshCw className="size-3.5" />
                  {t("machines.rotateCommand")}
                </Button>
              </div>
            </div>
          </Card>
        </TabsContent>

        <TabsContent value="runtime">
          <Card className="p-4">
            <p className="text-sm font-medium">{t("machines.detectedRuntimes")}</p>
            {detail.runtimes.length > 0 ? (
              <ul className="mt-2 flex flex-wrap gap-2">
                {detail.runtimes.map((rt) => (
                  <li key={rt} className="rounded-full border px-2.5 py-1 text-xs">
                    {rt}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="mt-2 text-sm text-muted-foreground">
                {detail.online ? t("machines.noRuntimes") : t("machines.offlineRuntimes")}
              </p>
            )}
            <p className="mt-3 text-xs text-muted-foreground">{t("machines.runtimeHint")}</p>
          </Card>
        </TabsContent>

        <TabsContent value="agents">
          <Card className="p-4">
            <div className="flex items-center gap-2">
              <Search className="size-4 text-muted-foreground" aria-hidden="true" />
              <label htmlFor="agent-filter" className="sr-only">{t("machines.filterAgents")}</label>
              <Input
                id="agent-filter"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t("machines.filterAgents")}
                className="h-8 max-w-xs"
              />
            </div>
            <ul className="mt-3 space-y-2">
              {agents.map((a) => (
                <li key={a.id} className="flex items-center gap-2 rounded-md border p-2">
                  <Bot className="size-4 text-muted-foreground" aria-hidden="true" />
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{a.name}</p>
                    <p className="truncate text-xs text-muted-foreground">
                      {a.runtime}
                      {a.description ? ` · ${a.description}` : ""}
                    </p>
                  </div>
                  <span className="text-xs text-muted-foreground">
                    {AGENT_STATUS_KEY[a.status] ? t(AGENT_STATUS_KEY[a.status]) : a.status}
                  </span>
                  <Button size="sm" variant="ghost" className="cursor-pointer" onClick={() => navigate("/team/agents")}>
                    {t("machines.manage")}
                  </Button>
                </li>
              ))}
              {agents.length === 0 && (
                <li className="py-6 text-center text-sm text-muted-foreground">
                  {detail.agents.length === 0 ? t("machines.noAgents") : t("machines.noAgentsMatch")}
                </li>
              )}
            </ul>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
