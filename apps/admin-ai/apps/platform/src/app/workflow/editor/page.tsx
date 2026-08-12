import { useCallback, useEffect, useState } from "react";
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
import { FlowNode } from "../../../components/workflow-canvas";
import { loadWorkflows, saveWorkflow, type SavedWorkflow } from "../../../lib/workflow-store";
import { controlApi } from "../../../lib/control-api";

const nodeTypes = { flow: FlowNode };
const INTERNAL_NODE_KEYS = new Set(["label", "status", "type", "defaults"]);

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
  const [currentId, setCurrentId] = useState(wfId ?? ""); // pinned after first save
  const [nodes, setNodes, onNodesChange] = useNodesState<Node>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);
  const [selectedNode, setSelectedNode] = useState<Node | null>(null);
  const [workspace, setWorkspace] = useState("");
  const [busy, setBusy] = useState(false);

  // Load existing from server or start fresh
  useEffect(() => {
    let cancelled = false;
    if (wfId) {
      loadWorkflows().then((list: SavedWorkflow[]) => {
        if (cancelled) return;
        const saved = list.find((w: SavedWorkflow) => w.id === wfId);
        if (saved?.def) {
          setName(saved.name);
          try {
            const def = saved.def as { nodes?: Array<{ id: string; type?: string }>; edges?: Array<{ from: string; to: string; condition?: string }> };
            const imported = canvasFromDef(def);
            setNodes(imported.nodes);
            setEdges(imported.edges);
          } catch {
            const { nodes: n, edges: e } = emptyCanvas();
            setNodes(n); setEdges(e);
          }
        } else {
          const { nodes: n, edges: e } = emptyCanvas();
          setNodes(n); setEdges(e);
        }
      }).catch(() => {
        if (!cancelled) {
          const { nodes: n, edges: e } = emptyCanvas();
          setNodes(n); setEdges(e);
        }
      });
    } else {
      const { nodes: n, edges: e } = emptyCanvas();
      setNodes(n); setEdges(e);
    }
    return () => { cancelled = true; };
  }, [wfId, setEdges, setNodes]);

  const onConnect = useCallback((conn: Connection) => setEdges((eds) => addEdge(conn, eds)), [setEdges]);

  const onDrop = useCallback((event: React.DragEvent) => {
    event.preventDefault();
    const type = event.dataTransfer.getData("application/reactflow-type");
    if (!type) return;
    // ponytail: approximate screen-to-flow position using the drop target's bounding rect.
    const rect = (event.target as HTMLElement).closest(".react-flow")?.getBoundingClientRect();
    const pos = rect
      ? { x: event.clientX - rect.left - 120, y: event.clientY - rect.top - 20 }
      : { x: 100, y: 100 };
    const id = `${type}-${Date.now()}-${Math.random().toString(36).slice(2, 6)}`;
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
  }, [selectedNode, setEdges, setNodes]);

  // Serialize canvas → WorkflowDef JSON. Merges node template defaults
  // with any property-panel edits (which write to top-level data keys).
  const toDef = useCallback(() => {
    const defNodes = nodes.map((n) => {
      const data = n.data as Record<string, unknown>;
      const defs = (data.defaults as Record<string, unknown>) ?? {};
      const d: Record<string, unknown> = { id: n.id, type: data.type };
      // Start with template defaults
      Object.assign(d, defs);
      // Overlay all property-panel edits (top-level keys except internals)
      for (const [k, v] of Object.entries(data)) {
        if (INTERNAL_NODE_KEYS.has(k) || v === undefined) continue;
        if (typeof v === "object" && v !== null && k in (defs as object)) {
          d[k] = { ...((defs as Record<string, unknown>)[k] as Record<string, unknown> ?? {}), ...(v as Record<string, unknown>) };
        } else {
          d[k] = v;
        }
      }
      return d;
    });
    const defEdges = edges.map((e) => ({ from: e.source, to: e.target, condition: (e.label as string) || "success" }));
    return { id: currentId || wfId || "", version: "1", name, nodes: defNodes, edges: defEdges };
  }, [nodes, edges, name, currentId, wfId]);

  // Save to server. Pins the workflow id after first save.
  const [saving, setSaving] = useState(false);
  const [saveErr, setSaveErr] = useState("");
  const save = useCallback(async () => {
    setSaving(true);
    setSaveErr("");
    try {
      const def = toDef();
      const id = currentId || wfId || `wf-${Date.now()}`;
      await saveWorkflow({ id, name, def: def as unknown, updatedAt: new Date().toISOString() } as SavedWorkflow);
      if (!currentId && !wfId) {
        setCurrentId(id);
        const params = new URLSearchParams(window.location.search);
        params.set("id", id);
        window.history.replaceState(null, "", `?${params.toString()}`);
      }
    } catch (e) {
      setSaveErr((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [toDef, name, currentId, wfId]);

  // Launch run
  const run_ = useCallback(async () => {
    setBusy(true);
    try {
      const def = toDef();
      const r = await controlApi.launchRun(def, workspace.trim());
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
        <Button size="sm" variant="outline" onClick={save} disabled={saving}>{saving ? "保存中..." : saveErr ? `错误: ${saveErr}` : "保存"}</Button>
        {saveErr && <span className="text-xs text-destructive">{saveErr}</span>}
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
