import { useEffect, useMemo, useState } from "react"
import { useParams } from "react-router"
import { ReactFlow, Background, Controls, Handle, Position, type Edge, type Node, type NodeProps } from "@xyflow/react"
import "@xyflow/react/dist/style.css"
import { Button } from "@workspace/ui/components/button"
import { controlApi } from "../../../lib/control-api"

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

  const waitingNodes = flowNodes.filter((n) => (n.data as { status: string }).status === "waiting_approval")

  const decide = async (nodeId: string, approve: boolean) => {
    if (!runId) return
    await controlApi.approve(runId, nodeId, approve)
  }

  if (!detail) return <div className="text-sm text-muted-foreground">加载中…</div>

  return (
    <div className="space-y-4">
      <div className="flex items-center gap-2">
        <h1 className="text-xl font-semibold">Run {detail.id}</h1>
        <span className="text-sm text-muted-foreground">{detail.status}</span>
      </div>

      {waitingNodes.map((n) => (
        <div key={n.id} className="flex items-center gap-2 rounded-md border border-primary/50 p-3 text-sm">
          <span>审批节点「{n.id}」等待审批:</span>
          <Button size="sm" onClick={() => decide(n.id, true)}>通过</Button>
          <Button size="sm" variant="outline" onClick={() => decide(n.id, false)}>拒绝</Button>
        </div>
      ))}

      <div className="h-[520px] rounded-md border">
        <ReactFlow nodes={flowNodes} edges={edges} nodeTypes={{ flow: FlowNode }} fitView>
          <Background />
          <Controls />
        </ReactFlow>
      </div>
    </div>
  )
}
