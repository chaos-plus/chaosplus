import { useCallback, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { ArrowLeft, Bot, KeyRound, Search } from "lucide-react"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@workspace/ui/components/tabs"
import { controlApi, type MachineDetail } from "../../../lib/control-api"

const AGENT_STATUS: Record<string, string> = { running: "运行中", stopped: "已停止", retired: "已注销" }

function timeText(ts: number): string {
  return ts ? new Date(ts).toLocaleString() : "—"
}

export default function MachineDetailPage() {
  const { machineId = "" } = useParams()
  const navigate = useNavigate()
  const [detail, setDetail] = useState<MachineDetail | null>(null)
  const [notFound, setNotFound] = useState(false)
  const [filter, setFilter] = useState("")
  const [newToken, setNewToken] = useState("")

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
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const rotate = async () => {
    try {
      const r = await controlApi.refreshToken(machineId)
      setNewToken(r.token)
      toast.success("长期 token 已轮换,旧 token 立即失效")
    } catch (e) {
      toast.error(`轮换失败:${e instanceof Error ? e.message : String(e)}`)
    }
  }

  if (notFound) {
    return (
      <div className="space-y-4">
        <Button variant="ghost" className="cursor-pointer gap-1.5" onClick={() => navigate("/team/machines")}>
          <ArrowLeft className="size-4" />
          返回列表
        </Button>
        <p className="text-sm text-muted-foreground">找不到这台 machine,可能已被取消接入。</p>
      </div>
    )
  }
  if (!detail) return <div className="text-sm text-muted-foreground">加载中…</div>

  const agents = detail.agents.filter((a) => !filter || a.name.toLowerCase().includes(filter.toLowerCase()))

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" size="sm" className="cursor-pointer gap-1.5" onClick={() => navigate("/team/machines")}>
          <ArrowLeft className="size-4" />
          返回
        </Button>
        <h1 className="text-xl font-semibold">{detail.name}</h1>
        <span
          className={`rounded-full px-2 py-0.5 text-xs font-medium ${
            detail.online ? "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400" : "bg-muted text-muted-foreground"
          }`}
        >
          {detail.online ? "在线" : "离线"}
        </span>
      </div>

      <Tabs defaultValue="info">
        <TabsList>
          <TabsTrigger value="info" className="cursor-pointer">关键信息</TabsTrigger>
          <TabsTrigger value="runtime" className="cursor-pointer">运行时</TabsTrigger>
          <TabsTrigger value="agents" className="cursor-pointer">agent 列表({detail.agents.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="info">
          <Card className="p-4">
            <dl className="grid gap-3 sm:grid-cols-2">
              {[
                ["ID", detail.id],
                ["地址", detail.address || "—"],
                ["操作系统", detail.os || "未上报"],
                ["状态", detail.status],
                ["注册时间", timeText(detail.registeredAt)],
                ["最近心跳", timeText(detail.lastHeartbeatAt)],
              ].map(([k, v]) => (
                <div key={k}>
                  <dt className="text-xs text-muted-foreground">{k}</dt>
                  <dd className="text-sm break-all">{v}</dd>
                </div>
              ))}
            </dl>

            <div className="mt-4 border-t pt-4">
              <p className="text-sm font-medium">长期 token</p>
              <p className="mt-0.5 text-xs text-muted-foreground">token 长期有效,只能手动轮换;轮换后该机需用新 token 重连。</p>
              <Button size="sm" variant="outline" className="mt-2 cursor-pointer gap-1.5" onClick={rotate}>
                <KeyRound className="size-3.5" />
                轮换 token
              </Button>
              {newToken && (
                <div className="mt-2 rounded-md border bg-muted p-2">
                  <p className="text-xs text-muted-foreground">新 token(仅此一次可见):</p>
                  <code className="text-xs break-all">{newToken}</code>
                </div>
              )}
            </div>
          </Card>
        </TabsContent>

        <TabsContent value="runtime">
          <Card className="p-4">
            <p className="text-sm font-medium">检测到的执行器</p>
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
                {detail.online ? "该机未上报可用执行器。" : "机器离线,重新连接后会上报。"}
              </p>
            )}
            <p className="mt-3 text-xs text-muted-foreground">新建数字人时,运行时下拉的选项就来自这里。</p>
          </Card>
        </TabsContent>

        <TabsContent value="agents">
          <Card className="p-4">
            <div className="flex items-center gap-2">
              <Search className="size-4 text-muted-foreground" aria-hidden="true" />
              <label htmlFor="agent-filter" className="sr-only">按名称过滤</label>
              <Input
                id="agent-filter"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="按名称过滤"
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
                  <span className="text-xs text-muted-foreground">{AGENT_STATUS[a.status] ?? a.status}</span>
                  <Button size="sm" variant="ghost" className="cursor-pointer" onClick={() => navigate("/team/agents")}>
                    管理
                  </Button>
                </li>
              ))}
              {agents.length === 0 && (
                <li className="py-6 text-center text-sm text-muted-foreground">
                  {detail.agents.length === 0 ? "这台机器还没有托管数字人。" : "没有匹配的数字人。"}
                </li>
              )}
            </ul>
          </Card>
        </TabsContent>
      </Tabs>
    </div>
  )
}
