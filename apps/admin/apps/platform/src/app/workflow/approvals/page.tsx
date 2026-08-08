import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Badge } from "@workspace/ui/components/badge"
import { Card } from "@workspace/ui/components/card"
import { controlApi, type Run } from "../../../lib/control-api"

export default function ApprovalsPage() {
  const navigate = useNavigate()
  const [runs, setRuns] = useState<Run[]>([])

  const load = useCallback(() => {
    void controlApi.runs().then(setRuns).catch(() => setRuns([]))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [load])

  const pending = runs.filter((r) => r.status === "waiting_approval")

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">审批 Approvals</h1>
      <p className="text-sm text-muted-foreground">等待人工审批的 run(点进去在 DAG 上审批)。</p>
      <div className="space-y-2">
        {pending.map((r) => (
          <Card key={r.id} className="cursor-pointer p-3 text-sm hover:bg-accent" onClick={() => navigate(`/workflow/runs/${r.id}`)}>
            <div className="flex items-center gap-2">
              <span className="font-medium">{r.id}</span>
              <Badge>待审批</Badge>
              <span className="ml-auto text-muted-foreground">{r.createdAt}</span>
            </div>
          </Card>
        ))}
        {pending.length === 0 && <p className="text-sm text-muted-foreground">暂无待审批 run。</p>}
      </div>
    </div>
  )
}
