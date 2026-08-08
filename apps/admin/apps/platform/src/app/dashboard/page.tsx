import { useEffect, useState } from "react"
import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { controlApi, type Channel, type Machine, type Run } from "../../lib/control-api"

export default function DashboardPage() {
  const [machines, setMachines] = useState<Machine[]>([])
  const [runs, setRuns] = useState<Run[]>([])
  const [channels, setChannels] = useState<Channel[]>([])

  useEffect(() => {
    void controlApi.machines().then(setMachines).catch(() => setMachines([]))
    void controlApi.runs().then(setRuns).catch(() => setRuns([]))
    void controlApi.channels().then(setChannels).catch(() => setChannels([]))
  }, [])

  const online = machines.filter((m) => m.online).length
  const running = runs.filter((r) => r.status === "running" || r.status === "waiting_approval").length

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">仪表盘</h1>
      <div className="grid gap-3 sm:grid-cols-3">
        <Card>
          <CardHeader><CardTitle className="text-sm">机器</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">
            {online}
            <span className="text-sm font-normal text-muted-foreground"> / {machines.length} 在线</span>
          </CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle className="text-sm">进行中 run</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">{running}</CardContent>
        </Card>
        <Card>
          <CardHeader><CardTitle className="text-sm">频道</CardTitle></CardHeader>
          <CardContent className="text-2xl font-semibold">{channels.length}</CardContent>
        </Card>
      </div>
      <p className="text-sm text-muted-foreground">
        顶部一级菜单:仪表盘 / 会话区 / 工作区 / 团队管理 / 工作流。右侧顶部切换租户/实体。
      </p>
    </div>
  )
}
