import type { Node } from "@xyflow/react";

interface Props {
  node: Node | null;
  onChange: (id: string, data: Record<string, unknown>) => void;
}

export function PropertyPanel({ node, onChange }: Props) {
  if (!node) {
    return (
      <div className="p-4 text-sm text-muted-foreground">
        <p>选中画布上的节点以编辑属性</p>
        <div className="mt-4 space-y-2 text-xs">
          <p><kbd className="rounded border px-1">Backspace</kbd> 删除选中节点</p>
          <p><kbd className="rounded border px-1">Ctrl+Z</kbd> 撤销</p>
        </div>
      </div>
    );
  }

  const data = node.data as Record<string, unknown>;
  const nodeType = (data.type as string) ?? "";

  return (
    <div className="space-y-3 p-3 text-sm">
      <div className="text-xs font-semibold text-muted-foreground uppercase">{nodeType}</div>

      {/* Common fields */}
      <Field label="ID" value={node.id} onChange={(v) => onChange(node.id, { ...data, id: v })} />
      <Field label="Name" value={(data.name as string) ?? ""} onChange={(v) => onChange(node.id, { ...data, name: v })} />

      {/* Type-specific fields */}
      {(nodeType === "agent" || nodeType === "script" || nodeType === "http") && (
        <AgentFields data={data} onChange={(d) => onChange(node.id, { ...data, ...d })} />
      )}
      {nodeType === "human_approval" && (
        <ApprovalFields data={data} onChange={(d) => onChange(node.id, { ...data, ...d })} />
      )}
      {nodeType === "condition" && (
        <ExprField label="Condition JSON Logic" data={data} key_="condition" onChange={(d) => onChange(node.id, { ...data, ...d })} />
      )}
      {nodeType === "transform" && (
        <>
          <ExprField label="Transform JSON Logic" data={data} key_="transform" onChange={(d) => onChange(node.id, { ...data, ...d })} />
          <Field label="Output Artifact" value={(data.output as string) ?? ""} onChange={(v) => onChange(node.id, { ...data, output: v })} />
        </>
      )}
      {nodeType === "loop" && (
        <>
          <Field label="Body Entry Node" value={(data.bodyEntry as string) ?? ""} onChange={(v) => onChange(node.id, { ...data, bodyEntry: v })} />
          <Field label="Max Iterations" value={String((data.maxIterations as number) ?? 10)} onChange={(v) => onChange(node.id, { ...data, maxIterations: Number(v) })} />
        </>
      )}
      {nodeType === "trigger" && (
        <SelectField label="Source" value={(data.source as string) ?? "manual"} options={["manual", "schedule", "webhook"]} onChange={(v) => onChange(node.id, { ...data, source: v })} />
      )}

      {/* onError for agent/script/http nodes */}
      {(nodeType === "agent" || nodeType === "script" || nodeType === "http") && (
        <SelectField label="onError" value={(data.onError as string) ?? "stop"} options={["stop", "continue"]} onChange={(v) => onChange(node.id, { ...data, onError: v })} />
      )}
    </div>
  );
}

function Field({ label, value, onChange }: { label: string; value: string; onChange: (v: string) => void }) {
  return (
    <div>
      <label className="block text-xs text-muted-foreground mb-1">{label}</label>
      <input
        className="w-full rounded border border-border bg-background px-2 py-1 text-xs"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      />
    </div>
  );
}

function SelectField({ label, value, options, onChange }: { label: string; value: string; options: string[]; onChange: (v: string) => void }) {
  return (
    <div>
      <label className="block text-xs text-muted-foreground mb-1">{label}</label>
      <select
        className="w-full rounded border border-border bg-background px-2 py-1 text-xs"
        value={value}
        onChange={(e) => onChange(e.target.value)}
      >
        {options.map((o) => <option key={o} value={o}>{o}</option>)}
      </select>
    </div>
  );
}

function ExprField({ label, data, key_, onChange }: { label: string; data: Record<string, unknown>; key_: string; onChange: (d: Record<string, unknown>) => void }) {
  const obj = (data[key_] as Record<string, unknown>) ?? {};
  const text = JSON.stringify(obj, null, 2);
  return (
    <div>
      <label className="block text-xs text-muted-foreground mb-1">{label}</label>
      <textarea
        className="w-full rounded border border-border bg-background px-2 py-1 text-xs font-mono"
        rows={4}
        value={text}
        onChange={(e) => {
          try { onChange({ [key_]: JSON.parse(e.target.value) }); } catch { /* invalid — don't update */ }
        }}
      />
    </div>
  );
}

function AgentFields({ data, onChange }: { data: Record<string, unknown>; onChange: (d: Record<string, unknown>) => void }) {
  const agent = (data.agent as Record<string, unknown>) ?? {};
  return (
    <>
      <SelectField
        label="Executor"
        value={(agent.executor as string) ?? "claude"}
        options={["claude", "codex", "mastra", "mock", "script", "http"]}
        onChange={(v) => onChange({ agent: { ...agent, executor: v } })}
      />
      <div>
        <label className="block text-xs text-muted-foreground mb-1">System Prompt</label>
        <textarea
          className="w-full rounded border border-border bg-background px-2 py-1 text-xs"
          rows={4}
          value={(agent.systemPrompt as string) ?? ""}
          onChange={(e) => onChange({ agent: { ...agent, systemPrompt: e.target.value } })}
        />
      </div>
    </>
  );
}

function ApprovalFields({ data, onChange }: { data: Record<string, unknown>; onChange: (d: Record<string, unknown>) => void }) {
  const ha = (data.humanApproval as Record<string, unknown>) ?? {};
  return (
    <>
      <Field label="Timeout (ms)" value={String((ha.timeoutMs as number) ?? 600000)} onChange={(v) => onChange({ humanApproval: { ...ha, timeoutMs: Number(v) } })} />
      <SelectField label="onTimeout" value={(ha.onTimeout as string) ?? "pause"} options={["pause", "auto_reject"]} onChange={(v) => onChange({ humanApproval: { ...ha, onTimeout: v } })} />
      <SelectField label="onReject" value={(ha.onReject as string) ?? "pause"} options={["pause", "retry"]} onChange={(v) => onChange({ humanApproval: { ...ha, onReject: v } })} />
    </>
  );
}
