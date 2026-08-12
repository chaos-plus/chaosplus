/* eslint-disable react-refresh/only-export-components */
import type { DragEvent } from "react";
import { Bot, CircleDot, FileText, GitBranch, Globe, Package, Play, Repeat, Users, Workflow, Zap, type LucideIcon } from "lucide-react";

export interface NodeTemplate {
  type: string;
  label: string;
  icon: LucideIcon;
  defaults: Record<string, unknown>;
}

export const DEV_NODES: NodeTemplate[] = [
  { type: "agent",           label: "Agent",         icon: Bot,       defaults: { agent: { id: "", role: "", executor: "claude", systemPrompt: "" } } },
  { type: "group",           label: "Group/Subgraph",icon: Package,   defaults: { group: { entry: "", nodes: [], edges: [] } } },
  { type: "condition",       label: "Condition",     icon: GitBranch, defaults: { condition: { expr: {} } } },
  { type: "transform",       label: "Transform",     icon: Zap,       defaults: { transform: { expr: {}, output: "" } } },
  { type: "loop",            label: "Loop",          icon: Repeat,    defaults: { loop: { bodyEntry: "", condition: {}, maxIterations: 10 } } },
  { type: "parallel_fork",   label: "Parallel Fork", icon: Workflow,  defaults: { fanOut: { itemsExpr: {}, templateNodeId: "" } } },
  { type: "join",            label: "Join",          icon: CircleDot, defaults: {} },
  { type: "human_approval",  label: "Approval",      icon: Users,     defaults: { humanApproval: { approvers: "any_human", timeoutMs: 600000, onTimeout: "pause", onReject: "pause" } } },
];

export const AUTO_NODES: NodeTemplate[] = [
  { type: "script",          label: "Script",        icon: FileText, defaults: { agent: { id: "", role: "", executor: "script", systemPrompt: "" } } },
  { type: "http",            label: "HTTP Request",  icon: Globe,    defaults: { agent: { id: "", role: "", executor: "http", systemPrompt: "" } } },
  { type: "trigger",         label: "Trigger",       icon: Play,     defaults: { trigger: { source: "manual" } } },
];

const ALL_NODES = [...DEV_NODES, ...AUTO_NODES];

export function onDragStart(event: DragEvent, node: NodeTemplate) {
  event.dataTransfer.setData("application/reactflow-type", node.type);
  event.dataTransfer.setData("application/reactflow-defaults", JSON.stringify(node.defaults));
  event.dataTransfer.effectAllowed = "move";
}

export function NodeLibrary() {
  return (
    <div className="space-y-3 p-3">
      <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wide">Developer</div>
      {DEV_NODES.map((n) => (
        <div
          key={n.type}
          draggable
          onDragStart={(e) => onDragStart(e, n)}
          className="flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm cursor-grab hover:bg-accent active:cursor-grabbing"
        >
          <n.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span>{n.label}</span>
        </div>
      ))}
      <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wide mt-4">Automation</div>
      {AUTO_NODES.map((n) => (
        <div
          key={n.type}
          draggable
          onDragStart={(e) => onDragStart(e, n)}
          className="flex items-center gap-2 rounded-md border border-border bg-card px-3 py-2 text-sm cursor-grab hover:bg-accent active:cursor-grabbing"
        >
          <n.icon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
          <span>{n.label}</span>
        </div>
      ))}
    </div>
  );
}

/** Look up a template by type for the drop handler. */
export function templateByType(type: string): NodeTemplate | undefined {
  return ALL_NODES.find((n) => n.type === type);
}
