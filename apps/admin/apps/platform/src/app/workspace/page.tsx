import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Badge } from "@workspace/ui/components/badge"
import { Card } from "@workspace/ui/components/card"
import { controlApi, type Run } from "../../lib/control-api"

export default function WorkspacePage() {
  const navigate = useNavigate()
  const [runs, setRuns] = useState<Run[]>([])

  const load = useCallback(() => {
    void controlApi.runs().then(setRuns).catch(() => setRuns([]))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">工作区</h1>
      <p className="text-sm text-muted-foreground">工作区里的 run 与产物(agent 在每个 run 的 workspace 目录产出 output.json 等)。</p>
      <div className="space-y-2">
        {runs.map((r) => (
          <Card key={r.id} className="cursor-pointer p-3 text-sm hover:bg-accent" onClick={() => navigate(`/workflow/runs/${r.id}`)}>
            <div className="flex items-center gap-2">
              <span className="font-medium">{r.id}</span>
              <Badge variant={r.status === "completed" ? "default" : r.status === "failed" ? "destructive" : "secondary"}>{r.status}</Badge>
              <span className="ml-auto text-muted-foreground">{r.nodes} 节点 · {r.createdAt}</span>
            </div>
          </Card>
        ))}
        {runs.length === 0 && <p className="text-sm text-muted-foreground">暂无 run。</p>}
      </div>
    </div>
  )
}
