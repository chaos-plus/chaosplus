/* eslint-disable react-hooks/set-state-in-effect, react-refresh/only-export-components */
import { useMemo } from "react"
import { ReactFlow, Background, Controls, Handle, Position, type Edge, type Node, type NodeProps } from "@xyflow/react"
import "@xyflow/react/dist/style.css"

export const STATUS_COLOR: Record<string, string> = {
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

export function FlowNode({ data }: NodeProps) {
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
export function buildFlow(nodes: DefNode[], edges: DefEdge[], statuses: Record<string, string> = {}) {
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

  const flowNodes: Node[] = nodes.map((n) => ({
    id: n.id,
    type: "flow",
    position: { x: (rank[n.id] ?? 0) * 240, y: ((byRank[rank[n.id] ?? 0] ?? []).indexOf(n.id)) * 120 },
    data: { label: `${n.id} · ${n.type ?? "node"}`, status: statuses[n.id] ?? "pending" },
  }))
  const flowEdges: Edge[] = edges.map((e, i) => ({
    id: `e${i}`,
    source: e.from,
    target: e.to,
    animated: true,
    label: e.condition ?? "success",
  }))
  return { nodes: flowNodes, edges: flowEdges }
}

export function WorkflowCanvas({
  nodes,
  edges,
  statuses,
  height = 460,
}: {
  nodes: DefNode[]
  edges: DefEdge[]
  statuses?: Record<string, string>
  height?: number
}) {
  const { nodes: fn, edges: fe } = useMemo(() => buildFlow(nodes, edges, statuses), [nodes, edges, statuses])
  return (
    <div className="rounded-md border" style={{ height }}>
      <ReactFlow nodes={fn} edges={fe} nodeTypes={{ flow: FlowNode }} fitView>
        <Background />
        <Controls />
      </ReactFlow>
    </div>
  )
}
