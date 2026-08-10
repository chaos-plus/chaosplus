import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router";
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  addEdge,
  useNodesState,
  useEdgesState,
  type Connection,
  type Edge,
  type Node,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import { Button } from "@workspace/ui/components/button";
import { Input } from "@workspace/ui/components/input";
import { Badge } from "@workspace/ui/components/badge";
import { NodeLibrary, templateByType } from "../../../components/workflow-editor/node-library";
import { PropertyPanel } from "../../../components/workflow-editor/property-panel";
import { FlowNode, STATUS_COLOR } from "../../../components/workflow-canvas";
import { loadWorkflows, saveWorkflow, getWorkflow, type SavedWorkflow } from "../../../lib/workflow-store";
import { controlApi } from "../../../lib/control-api";

const nodeTypes = { flow: FlowNode };

/** Empty canvas default: one trigger node. */
function emptyCanvas(): { nodes: Node[]; edges: Edge[] } {
  return {
    nodes: [
      { id: "start", type: "flow", position: { x: 80, y: 200 },
        data: { label: "start · trigger", status: "pending", type: "trigger",
          defaults: { trigger: { source: "manual" } } } },
    ],
    edges: [],
  };
}

/** Add node-type defaults to a React Flow node when created from the library. */
function applyDefaults(node: Node, type: string): Node {
  const tpl = templateByType(type);
  if (!tpl) return node;
  return {
    ...node,
    data: {
      ...node.data,
      type,
      defaults: tpl.defaults,
      label: `${node.id} · ${tpl.label.toLowerCase()}`,
    },
  };
}

export default function EditorPage() {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const wfId = params.get("id");

  const [name, setName] = useState("Untitled");
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [workspace, setWorkspace] = useState("");
  const [busy, setBusy] = useState(false);

  // Load existing or start fresh
  useEffect(() => {
    if (wfId) {
      const saved = getWorkflow(wfId);
      if (saved) {
        setName(saved.name);
        const def = saved.def as { nodes?: Array<{ id: string; type?: string; [k: string]: unknown }>; edges?: Array<{ from: string; to: string; condition?: string }> };
        const imported = canvasFromDef(def);
        setNodes(imported.nodes);
        setEdges(imported.edges);
        return;
      }
    }
    const { nodes: n, edges: e } = emptyCanvas();
    setNodes(n);
    setEdges(e);
  }, [wfId]);

  const onConnect = useCallback((conn: Connection) => setEdges((eds) => addEdge(conn, eds)), [setEdges]);

  const onDrop = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    const type = event.dataTransfer.getData("application/reactflow-type");
    if (!type) return;
    const pos = { x: event.clientX - 320, y: event.clientY - 120 };
    const id = `${type}-${Date.now()}`;
    const newNode: Node = applyDefaults(
      { id, type: "flow", position: pos, data: { label: `${id} · ${type}`, status: "pending", type } },
      type,
    );
    setNodes((nds) => [...nds, newNode]);
  }, [setNodes]);

  const onDragOver = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    event.dataTransfer.dropEffect = "move";
  }, []);

  // Delete selected with Backspace
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Backspace" && selectedNode && document.activeElement === document.body) {
        setNodes((nds) => nds.filter((n) => n.id !== selectedNode.id));
        setEdges((eds) => eds.filter((e) => e.source !== selectedNode.id && e.target !== selectedNode.id));
        setSelectedNode(null);
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [selectedNode]);

  // Serialize canvas → WorkflowDef JSON
  const toDef = useCallback(() => {
    const defNodes = nodes.map((n) => {
      const data = n.data as Record<string, unknown>;
      const d: Record<string, unknown> = { id: n.id, type: data.type };
      const defs = data.defaults as Record<string, unknown> | undefined;
      if (defs) Object.assign(d, defs);
      // Merge any overrides from property panel edits
      if (data.onError) d.onError = data.onError;
      if (data.name) d.name = data.name;
      return d;
    });
    const defEdges = edges.map((e) => ({ from: e.source, to: e.target, condition: (e.label as string) || "success" }));
    return { id: wfId ?? `wf-${Date.now()}`, version: "1", name, nodes: defNodes, edges: defEdges };
  }, [nodes, edges, name, wfId]);

  // Save to localStorage
  const save = useCallback(() => {
    const def = toDef() as unknown;
    saveWorkflow({ id: wfId ?? `wf-${Date.now()}`, name, def, updatedAt: new Date().toISOString() } as SavedWorkflow);
  }, [toDef, name, wfId]);

  // Launch run
  const run_ = useCallback(async () => {
    setBusy(true);
    try {
      const def = toDef();
      const r = await controlApi.launchRun(def, workspace.trim() || "/tmp/chaos-workspace");
      navigate(`/workflow/runs/${r.runId}`);
    } catch (e) {
      alert(`启动失败: ${(e as Error).message}`);
    } finally {
      setBusy(false);
    }
  }, [toDef, workspace, navigate]);

  const nodeCount = nodes.length;
  const edgeCount = edges.length;

  return (
    <div className="flex flex-col h-[calc(100vh-4rem)]">
      {/* Toolbar */}
      <div className="flex items-center gap-3 px-4 py-2 border-b border-border bg-card">
        <Input className="w-48 h-8 text-sm" value={name} onChange={(e) => setName(e.target.value)} placeholder="工作流名称" />
        <Button size="sm" variant="outline" onClick={save}>保存</Button>
        <Button size="sm" variant="outline" onClick={() => {
          const json = JSON.stringify(toDef(), null, 2);
          void navigator.clipboard.writeText(json);
        }}>导出 JSON</Button>
        <div className="flex-1" />
        <Input className="w-48 h-8 text-xs" value={workspace} onChange={(e) => setWorkspace(e.target.value)} placeholder="workspace 目录 (可选)" />
        <Button size="sm" onClick={run_} disabled={busy}>{busy ? "启动中..." : "▶ 运行"}</Button>
      </div>

      {/* 3-panel body */}
      <div className="flex flex-1 overflow-hidden">
        {/* Left: Node Library */}
        <div className="w-48 border-r border-border bg-card overflow-y-auto shrink-0">
          <NodeLibrary />
        </div>

        {/* Center: Canvas */}
        <div className="flex-1" onDrop={onDrop} onDragOver={onDragOver}>
          <ReactFlow
            nodes={nodes}
            edges={edges}
            onNodesChange={onNodesChange}
            onEdgesChange={onEdgesChange}
            onConnect={onConnect}
            onNodeClick={(_e, node) => setSelectedNode(node)}
            onPaneClick={() => setSelectedNode(null)}
            nodeTypes={nodeTypes}
            fitView
            deleteKeyCode={["Backspace", "Delete"]}
          >
            <Background />
            <Controls />
            <MiniMap />
          </ReactFlow>
        </div>

        {/* Right: Property Panel */}
        <div className="w-64 border-l border-border bg-card overflow-y-auto shrink-0">
          <PropertyPanel
            node={selectedNode}
            onChange={(id, newData) => {
              setNodes((nds) => nds.map((n) => n.id === id ? { ...n, data: { ...n.data, ...newData } } : n));
            }}
          />
        </div>
      </div>

      {/* Status bar */}
      <div className="flex items-center gap-4 px-4 py-1 border-t border-border bg-card text-xs text-muted-foreground">
        <span>节点: <Badge variant="secondary" className="text-[10px]">{nodeCount}</Badge></span>
        <span>连线: <Badge variant="secondary" className="text-[10px]">{edgeCount}</Badge></span>
        <span className="ml-auto">React Flow · WorkflowDef JSON</span>
      </div>
    </div>
  );
}

/** Convert a WorkflowDef JSON to React Flow nodes + edges. */
function canvasFromDef(def: { nodes?: Array<{ id: string; type?: string; [k: string]: unknown }>; edges?: Array<{ from: string; to: string; condition?: string }> }) {
  const flowNodes: Node[] = (def.nodes ?? []).map((n, i) => ({
    id: n.id as string,
    type: "flow",
    position: { x: 80 + i * 200, y: 200 + (i % 3) * 120 },
    data: {
      label: `${n.id} · ${(n.type as string) ?? "node"}`,
      status: "pending",
      type: n.type as string,
      defaults: templateByType(n.type as string)?.defaults ?? {},
      ...Object.fromEntries(Object.entries(n).filter(([k]) => k !== "id" && k !== "type")),
    },
  }));
  const flowEdges: Edge[] = (def.edges ?? []).map((e, i) => ({
    id: `e${i}`,
    source: e.from,
    target: e.to,
    animated: true,
    label: e.condition as string ?? "success",
  }));
  return { nodes: flowNodes, edges: flowEdges };
}
