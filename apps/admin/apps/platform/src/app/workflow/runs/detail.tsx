import { useEffect, useMemo, useState } from "react"
import { useParams } from "react-router"
import { ReactFlow, Background, Controls, Handle, Position, type Edge, type Node, type NodeProps } from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { Check, ShieldQuestion, X } from "lucide-react"
import { Button } from "@workspace/ui/components/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, FEEDBACK_CATEGORIES, type FeedbackCategory } from "../../../lib/control-api"

const STATUS_COLOR: Record<string, string> = {
  pending: "#5b6270",
  running: "#d9a13c",
  completed: "#3fb96f",
  failed: "#e0564e",
  skipped: "#5b6270",
  waiting_approval: "#4aa3e8",
  rejected: "#e0564e",
}

interface DefNode { id: string; type?: string }
interface DefEdge { from: string; to: string; condition?: string }

interface RunDetail {
  id: string
  status: string
  def: { nodes?: DefNode[]; edges?: DefEdge[] }
}

function FlowNode({ data }: NodeProps) {
  const d = data as { label: string; status: string }
  return (
    <>
      <Handle type="target" position={Position.Top} />
      <div
        className="rounded-md border px-3 py-2 text-xs"
        style={{
          borderColor: STATUS_COLOR[d.status] ?? "#5b6270",
          background: d.status === "waiting_approval" ? "#1c2a3a" : "#1b1e26",
          color: "#e6e8ee",
        }}
      >
        <div className="font-medium">{d.label}</div>
        <div className="text-[10px] opacity-70">{d.status}</div>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </>
  )
}

/** 简单 rank 布局:BFS 按入度分层,同层纵向排开。 */
function layout(nodes: DefNode[], edges: DefEdge[]): { nodes: Node[]; edges: Edge[] } {
  const inDegree: Record<string, number> = {}
  for (const n of nodes) inDegree[n.id] = 0
  for (const e of edges) inDegree[e.to] = (inDegree[e.to] ?? 0) + 1

  const rank: Record<string, number> = {}
  const byRank: Record<number, string[]> = {}
  const queue = nodes.filter((n) => (inDegree[n.id] ?? 0) === 0).map((n) => n.id)
  for (const id of queue) {
    rank[id] = 0
    byRank[0] = [...(byRank[0] ?? []), id]
  }

  const index = new Set(queue)
  while (queue.length) {
    const id = queue.shift()!
    for (const e of edges.filter((e) => e.from === id)) {
      inDegree[e.to] -= 1
      if (inDegree[e.to] === 0 && !index.has(e.to)) {
        index.add(e.to)
        rank[e.to] = (rank[id] ?? 0) + 1
        byRank[rank[e.to]] = [...(byRank[rank[e.to]] ?? []), e.to]
        queue.push(e.to)
      }
    }
  }

  const rn: Node[] = nodes.map((n) => ({
    id: n.id,
    type: "flow",
    position: { x: (rank[n.id] ?? 0) * 240, y: ((byRank[rank[n.id] ?? 0] ?? []).indexOf(n.id)) * 120 },
    data: { label: `${n.id} · ${n.type ?? "node"}`, status: "pending" },
  }))
  const re: Edge[] = edges.map((e, i) => ({
    id: `e${i}`,
    source: e.from,
    target: e.to,
    animated: true,
    label: e.condition ?? "success",
  }))
  return { nodes: rn, edges: re }
}

export default function RunDetail() {
  const { runId } = useParams()
  const [detail, setDetail] = useState<RunDetail | null>(null)
  const [statuses, setStatuses] = useState<Record<string, string>>({})

  useEffect(() => {
    if (!runId) return
    void fetch(`/control/api/runs/${runId}`)
      .then((r) => r.json())
      .then(setDetail)
      .catch(() => setDetail(null))
  }, [runId])

  useEffect(() => {
    if (!runId) return
    const proto = location.protocol === "https:" ? "wss:" : "ws:"
    const ws = new WebSocket(`${proto}//${location.host}/control/api/runs/${runId}/events`)
    ws.onmessage = (e) => {
      const ev = JSON.parse(e.data) as { nodeId?: string; status?: string }
      const nodeId = ev.nodeId
      const st = ev.status
      if (nodeId && st) setStatuses((s) => ({ ...s, [nodeId]: st }))
    }
    return () => ws.close()
  }, [runId])

  const { nodes, edges } = useMemo(() => {
    if (!detail?.def?.nodes) return { nodes: [], edges: [] }
    return layout(detail.def.nodes, detail.def.edges ?? [])
  }, [detail])

  const flowNodes = useMemo<Node[]>(
    () =>
      nodes.map((n) => ({
        ...n,
        data: { ...(n.data as object), status: statuses[n.id] ?? "pending" },
      })),
    [nodes, statuses],
  )

  const [reject, setReject] = useState<{ nodeId: string } | null>(null)
  const [fb, setFb] = useState<{ category: FeedbackCategory; location: string; expected: string; detail: string }>({
    category: "功能缺陷",
    location: "",
    expected: "",
    detail: "",
  })
  const [fbError, setFbError] = useState("")

  const waitingNodes = flowNodes.filter((n) => (n.data as { status: string }).status === "waiting_approval")

  const approveNode = async (nodeId: string) => {
    if (!runId) return
    await controlApi.approve(runId, nodeId, true)
  }

  const submitRejection = async () => {
    if (!runId || !reject) return
    if (!fb.detail.trim()) {
      setFbError("请填写问题描述")
      return
    }
    try {
      await controlApi.approve(runId, reject.nodeId, false, fb.detail, {
        category: fb.category,
        location: fb.location || undefined,
        expected: fb.expected || undefined,
        detail: fb.detail.trim(),
      })
      setReject(null)
      setFb({ category: "功能缺陷", location: "", expected: "", detail: "" })
      setFbError("")
    } catch (e) {
      setFbError(e instanceof Error ? e.message : "提交失败")
    }
  }

  if (!detail) return <div className="text-sm text-muted-foreground">加载中…</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <h1 className="text-xl font-semibold">Run {detail.id}</h1>
        <span className="text-sm text-muted-foreground">{detail.status}</span>
      </div>

      {waitingNodes.map((n) => (
        <div key={n.id} className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-4">
          <div className="flex items-start gap-3">
            <ShieldQuestion className="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0 flex-1">
              <p className="font-medium">审批节点「{n.id}」等待人工确认</p>
              <p className="mt-0.5 text-sm text-muted-foreground">通过后工作流继续;拒绝需填写结构化反馈,供下一次执行改进。</p>
              <div className="mt-3 flex gap-2">
                <Button size="sm" className="cursor-pointer gap-1.5" onClick={() => approveNode(n.id)}>
                  <Check className="size-3.5" />
                  通过
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  className="cursor-pointer gap-1.5"
                  onClick={() => {
                    setReject({ nodeId: n.id })
                    setFbError("")
                  }}
                >
                  <X className="size-3.5" />
                  拒绝
                </Button>
              </div>
            </div>
          </div>
        </div>
      ))}

      <Dialog open={!!reject} onOpenChange={(v) => !v && setReject(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>拒绝审批 —— 填写反馈</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="fb-cat" className="text-sm font-medium">
                问题类别 <span className="text-destructive">*</span>
              </label>
              <select
                id="fb-cat"
                value={fb.category}
                onChange={(e) => setFb({ ...fb, category: e.target.value as FeedbackCategory })}
                className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
              >
                {FEEDBACK_CATEGORIES.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-loc" className="text-sm font-medium">
                位置
              </label>
              <Input
                id="fb-loc"
                value={fb.location}
                onChange={(e) => setFb({ ...fb, location: e.target.value })}
                placeholder="如:login.tsx:42 或 结算页"
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-exp" className="text-sm font-medium">
                期望结果
              </label>
              <Input
                id="fb-exp"
                value={fb.expected}
                onChange={(e) => setFb({ ...fb, expected: e.target.value })}
                placeholder="如:点击后跳转首页"
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-detail" className="text-sm font-medium">
                问题描述 <span className="text-destructive">*</span>
              </label>
              <Textarea
                id="fb-detail"
                rows={3}
                value={fb.detail}
                onChange={(e) => setFb({ ...fb, detail: e.target.value })}
                placeholder="具体哪里不对、怎么复现"
              />
            </div>
            {fbError && <p className="text-sm text-destructive">{fbError}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" className="cursor-pointer" onClick={() => setReject(null)}>
              取消
            </Button>
            <Button variant="destructive" className="cursor-pointer" onClick={submitRejection} disabled={!fb.detail.trim()}>
              提交拒绝
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="h-[520px] rounded-md border">
        <ReactFlow nodes={flowNodes} edges={edges} nodeTypes={{ flow: FlowNode }} fitView>
          <Background />
          <Controls />
        </ReactFlow>
      </div>
    </div>
  )
}
