import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card, CardContent } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { WorkflowCanvas } from "../../../components/workflow-canvas"
import { controlApi, type Run } from "../../../lib/control-api"

export const smokeWorkflow = {
  id: "smoke",
  version: "1",
  nodes: [
    { id: "start", type: "trigger", trigger: { source: "manual" } },
    {
      id: "w",
      type: "agent",
      agent: { id: "w", role: "pm", executor: "claude", systemPrompt: "你是测试 agent,完成任务后把结论写入 output.json(单个 JSON 对象,含 ok 与 summary)。" },
    },
    { id: "gate", type: "human_approval", humanApproval: { approvers: "any_human", timeoutMs: 600000, onTimeout: "pause", onReject: "pause" } },
  ],
  edges: [
    { from: "start", to: "w" },
    { from: "w", to: "gate" },
  ],
}

export default function RunsPage() {
  const navigate = useNavigate()
  const [runs, setRuns] = useState<Run[]>([])
  const [workflowJSON, setWorkflowJSON] = useState(JSON.stringify(smokeWorkflow, null, 2))
  const [workspace, setWorkspace] = useState("C:/tmp/chaos-smoke-ws")
  const [busy, setBusy] = useState(false)

  // 实时解析工作流 JSON,渲染 React Flow 预览。
  let preview: { nodes: { id: string; type?: string }[]; edges: { from: string; to: string; condition?: string }[] } | null = null
  try {
    const def = JSON.parse(workflowJSON) as { nodes?: { id: string; type?: string }[]; edges?: { from: string; to: string; condition?: string }[] }
    if (Array.isArray(def.nodes)) preview = { nodes: def.nodes, edges: def.edges ?? [] }
  } catch {
    preview = null
  }

  const load = useCallback(() => {
    void controlApi.runs().then(setRuns).catch(() => setRuns([]))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [load])

  const launch = async () => {
    setBusy(true)
    try {
      let def: unknown
      try {
        def = JSON.parse(workflowJSON)
      } catch {
        alert("工作流 JSON 不合法")
        return
      }
      const r = await controlApi.launchRun(def, workspace.trim())
      navigate(`/workflow/runs/${r.runId}`)
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">运行 Runs</h1>

      <Card>
        <CardContent className="grid gap-3 pt-4">
          <Textarea rows={8} className="font-mono text-xs" value={workflowJSON} onChange={(e) => setWorkflowJSON(e.target.value)} />
          <Input placeholder="workspace 目录" value={workspace} onChange={(e) => setWorkspace(e.target.value)} />
          <Button onClick={launch} disabled={busy}>▶ 发起 Run</Button>
        </CardContent>
      </Card>

      {preview && (
        <div>
          <h2 className="mb-2 text-sm font-semibold text-muted-foreground">DAG 预览(React Flow)</h2>
          <WorkflowCanvas nodes={preview.nodes} edges={preview.edges} height={420} />
        </div>
      )}

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
        {runs.length === 0 && <p className="text-sm text-muted-foreground">还没有 run,填上面的工作流 JSON 发起一个。</p>}
      </div>
    </div>
  )
}
