import type { DragEvent } from "react";

export interface NodeTemplate {
  type: string;
  label: string;
  icon: string;
  defaults: Record<string, unknown>;
}

export const DEV_NODES: NodeTemplate[] = [
  { type: "agent",           label: "Agent",         icon: "\u{1F9E0}", defaults: { agent: { id: "", role: "", executor: "claude", systemPrompt: "" } } },
  { type: "condition",       label: "Condition",     icon: "\u{1F500}", defaults: { condition: { expr: {} } } },
  { type: "transform",       label: "Transform",     icon: "⚡",     defaults: { transform: { expr: {}, output: "" } } },
  { type: "loop",            label: "Loop",          icon: "\u{1F504}", defaults: { loop: { bodyEntry: "", condition: {}, maxIterations: 10 } } },
  { type: "parallel_fork",   label: "Parallel Fork", icon: "⑃",     defaults: { fanOut: { itemsExpr: {}, templateNodeId: "" } } },
  { type: "join",            label: "Join",          icon: "⏺",     defaults: {} },
  { type: "human_approval",  label: "Approval",      icon: "\u{1F465}", defaults: { humanApproval: { approvers: "any_human", timeoutMs: 600000, onTimeout: "pause", onReject: "pause" } } },
];

export const AUTO_NODES: NodeTemplate[] = [
  { type: "script",          label: "Script",        icon: "\u{1F4DC}", defaults: { agent: { id: "", role: "", executor: "script", systemPrompt: "" } } },
  { type: "http",            label: "HTTP Request",  icon: "\u{1F310}", defaults: { agent: { id: "", role: "", executor: "http", systemPrompt: "" } } },
  { type: "trigger",         label: "Trigger",       icon: "▶️", defaults: { trigger: { source: "manual" } } },
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
          <span className="text-base">{n.icon}</span>
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
          <span className="text-base">{n.icon}</span>
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
