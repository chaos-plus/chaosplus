# Distributed Execution Layer + Visual Workflow Editor — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expand chaosplus daemon with pluggable non-Agent executors (script/http/cli) + build a visual workflow editor in Web UI. Control-plane scheduling layer is already solid (auth, persistence, NATS gateway, artifact validation).

**Architecture:** Three layers confirmed — Web (React Flow visual editor + monitoring), Server (Go control-plane: schedule/dispatch/persist), Daemon (TS/Bun: pluggable executors over NATS/WS). Executor types: agent (Claude SDK, Codex SDK, Mastra, Gemini CLI), script (shell/python/node), http (REST/GraphQL/gRPC). Node `executor` field in WorkflowDef determines dispatch target.

**Tech Stack:** React 19 + Vite + shadcn/ui + Tailwind + @xyflow/react (Web), Go 1.26 + chi + NATS (Server), TypeScript + Bun + Mastra + Claude/Codex SDK (Daemon).

## Global Constraints

- WorkflowDef JSON format unchanged — visual editor produces the same JSON the API already accepts
- Control-plane API unchanged — new UI pages call existing `POST /api/runs`, `GET /api/runs`, etc.
- Daemon executor backends use existing `BACKENDS` registry pattern — no new framework
- React Flow (`@xyflow/react`) already installed — no new canvas dependency
- shadcn/ui components already in `packages/ui` — reuse, don't add new UI libs
- All new code tested: Go tests for scheduler changes, TS tests (bun test) for executor backends, Playwright smoke for UI

---

## Phase 1: Daemon — Pluggable Non-Agent Executors

### Task 1: Script executor backend

**Files:**
- Create: `apps/daemon/src/backends/script.ts`
- Modify: `apps/daemon/src/backends/index.ts` (register "script" backend)
- Test: `apps/daemon/src/backends/script.test.ts`

**Interfaces:**
- Consumes: `AgentTask` from `../types` (uses `prompt` as script body, `cwd` as working dir, `signal` for abort)
- Produces: `AgentBackend` — function `runScript(task: AgentTask): AsyncGenerator<AgentEvent>`

- [ ] **Step 1: Write the script executor**

```typescript
// apps/daemon/src/backends/script.ts
import { execFile, type ChildProcess } from "node:child_process";
import type { AgentEvent, AgentTask } from "../types";

/** Resolve shebang or extension to an interpreter. */
function interpreter(script: string): { cmd: string; args: string[] } {
  const firstLine = script.split("\n")[0]?.trim();
  if (firstLine?.startsWith("#!")) {
    const parts = firstLine.slice(2).split(/\s+/);
    return { cmd: parts[0], args: [...parts.slice(1), "-"] };
  }
  // Default: detect from common patterns
  if (script.includes("import ") || script.includes("async ")) return { cmd: "bun", args: ["run", "-"] };
  if (script.includes("require(") || script.includes("console.log")) return { cmd: "node", args: ["-e", script] };
  return { cmd: "bash", args: [] }; // shell script
}

export async function* runScript(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };
  const { cmd, args } = interpreter(task.prompt);
  const stdinInput = args.includes("-") ? task.prompt : undefined;

  const proc: ChildProcess = execFile(
    cmd,
    stdinInput ? args.filter(a => a !== "-") : args,
    { cwd: task.cwd, env: { ...process.env, ...task.env }, timeout: 300_000 },
  );

  let stdout = "";
  let stderr = "";
  proc.stdout?.on("data", (d: Buffer) => { stdout += d.toString(); });
  proc.stderr?.on("data", (d: Buffer) => { stderr += d.toString(); });

  // Stream prompt as stdin when using "-" placeholder
  if (stdinInput && proc.stdin) {
    proc.stdin.write(task.prompt);
    proc.stdin.end();
  }

  task.signal?.addEventListener("abort", () => proc.kill(), { once: true });

  const exitCode: number = await new Promise((resolve) => {
    proc.on("close", resolve);
    proc.on("error", () => resolve(1));
  });

  const text = stdout || stderr;
  if (text) yield { type: "message", text };
  yield { type: "done", ok: exitCode === 0, exitCode, costUsd: 0 };
}
```

- [ ] **Step 2: Register in backend registry**

```typescript
// apps/daemon/src/backends/index.ts — add:
import { runScript } from "./script";

export const BACKENDS: Record<string, AgentBackend> = {
  claude: runClaude,
  codex: runCodex,
  mock: runMock,
  script: runScript,  // <-- add this line
};
```

- [ ] **Step 3: Run tests**

```bash
cd apps/daemon && bun test src/backends/script.test.ts
```

- [ ] **Step 4: Commit**

```bash
git add apps/daemon/src/backends/
git commit -m "feat(daemon): add script executor backend (shell/python/node/bun)"
```

---

### Task 2: HTTP executor backend

**Files:**
- Create: `apps/daemon/src/backends/http.ts`
- Modify: `apps/daemon/src/backends/index.ts` (register "http" backend)
- Test: `apps/daemon/src/backends/http.test.ts`

**Interfaces:**
- Consumes: `AgentTask` — `prompt` as JSON `{ method, url, headers?, body? }`, `signal` for abort
- Produces: `AgentBackend`

- [ ] **Step 1: Write the HTTP executor**

```typescript
// apps/daemon/src/backends/http.ts
import type { AgentEvent, AgentTask } from "../types";

export async function* runHttp(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };
  try {
    const cfg = JSON.parse(task.prompt) as {
      method?: string; url: string; headers?: Record<string, string>; body?: unknown;
    };
    const resp = await fetch(cfg.url, {
      method: cfg.method ?? "GET",
      headers: cfg.headers,
      body: cfg.body ? JSON.stringify(cfg.body) : undefined,
      signal: task.signal,
    });
    const text = await resp.text();
    yield { type: "message", text };
    yield { type: "done", ok: resp.ok, exitCode: resp.ok ? 0 : 1, costUsd: 0 };
  } catch (e) {
    yield { type: "error", message: (e as Error).message };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}
```

- [ ] **Step 2: Register**

```typescript
// apps/daemon/src/backends/index.ts — add:
import { runHttp } from "./http";
// ...
http: runHttp,
```

- [ ] **Step 3: Test**

```bash
cd apps/daemon && bun test src/backends/http.test.ts
```

- [ ] **Step 4: Commit**

```bash
git add apps/daemon/src/backends/http.ts apps/daemon/src/backends/index.ts
git commit -m "feat(daemon): add HTTP executor backend (REST/GraphQL)"
```

---

### Task 3: Mastra executor backend

**Files:**
- Create: `apps/daemon/src/backends/mastra.ts`
- Modify: `apps/daemon/src/backends/index.ts` (register "mastra" backend)
- Test: `apps/daemon/src/backends/mastra.test.ts`

**Interfaces:**
- Consumes: `AgentTask` — uses `prompt`, `systemPrompt`, `model`, `apiKey`
- Produces: `AgentBackend` wrapping Mastra's `Agent.generate()`

- [ ] **Step 1: Write Mastra backend**

```typescript
// apps/daemon/src/backends/mastra.ts
import { Agent } from "@mastra/core/agent";
import type { AgentEvent, AgentTask } from "../types";

export async function* runMastra(task: AgentTask): AsyncGenerator<AgentEvent> {
  yield { type: "session", status: "running" };
  try {
    const agent = new Agent({
      name: "executor",
      instructions: task.systemPrompt ?? "Complete the task described by the user.",
      model: task.model ?? "claude-fable-5",
    });
    const result = await agent.generate(task.prompt, {
      signal: task.signal,
      maxSteps: task.maxTurns ?? 25,
    });
    yield { type: "message", text: result.text };
    yield { type: "done", ok: true, exitCode: 0, costUsd: result.experimental_totalCostUsd ?? 0 };
  } catch (e) {
    yield { type: "error", message: (e as Error).message };
    yield { type: "done", ok: false, exitCode: 1, costUsd: 0 };
  }
}
```

- [ ] **Step 2: Register in BACKENDS**

```typescript
// apps/daemon/src/backends/index.ts — add:
import { runMastra } from "./mastra";
// ...
mastra: runMastra,
```

- [ ] **Step 3: Auto-detect Mastra on daemon startup**

```typescript
// apps/daemon/src/serve.ts — in runtimes detection, add:
"mastra",  // always available (pure API, no binary needed)
```

- [ ] **Step 4: Test and commit**

```bash
cd apps/daemon && bun test src/backends/mastra.test.ts
git add apps/daemon/src/backends/mastra.ts apps/daemon/src/backends/index.ts apps/daemon/src/serve.ts
git commit -m "feat(daemon): add Mastra executor backend (LLM API)"
```

---

## Phase 2: Web — Visual Workflow Editor

### Task 4: Workflow list page

**Files:**
- Create: `apps/admin/apps/platform/src/app/workflow/list/page.tsx`
- Create: `apps/admin/apps/platform/src/lib/workflow-store.ts` (localStorage persistence)
- Modify: `apps/admin/apps/platform/src/lib/control-api.ts` (add workflow CRUD if API exists)

**Interfaces:**
- Consumes: existing `controlApi` from `../lib/control-api`
- Produces: `<WorkflowListPage />` — list of saved workflows with create/edit/delete/run actions

- [ ] **Step 1: Create localStorage workflow store**

```typescript
// apps/admin/apps/platform/src/lib/workflow-store.ts
const KEY = "chaosplus-workflows";

export interface SavedWorkflow {
  id: string;
  name: string;
  def: unknown; // WorkflowDef JSON
  updatedAt: string;
}

export function loadWorkflows(): SavedWorkflow[] {
  try { return JSON.parse(localStorage.getItem(KEY) ?? "[]"); } catch { return []; }
}
export function saveWorkflow(wf: SavedWorkflow): void {
  const list = loadWorkflows().filter(w => w.id !== wf.id);
  list.push(wf);
  localStorage.setItem(KEY, JSON.stringify(list));
}
export function deleteWorkflow(id: string): void {
  localStorage.setItem(KEY, JSON.stringify(loadWorkflows().filter(w => w.id !== id)));
}
```

- [ ] **Step 2: Build the list page**

```tsx
// apps/admin/apps/platform/src/app/workflow/list/page.tsx
import { useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "@workspace/ui/components/button";
import { Card, CardContent } from "@workspace/ui/components/card";
import { Badge } from "@workspace/ui/components/badge";
import { loadWorkflows, deleteWorkflow, type SavedWorkflow } from "../../../lib/workflow-store";

export default function WorkflowListPage() {
  const navigate = useNavigate();
  const [workflows, setWorkflows] = useState<SavedWorkflow[]>(loadWorkflows());

  const remove = (id: string) => {
    deleteWorkflow(id);
    setWorkflows(loadWorkflows());
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">工作流 Workflows</h1>
        <Button onClick={() => navigate("/workflow/editor")}>+ 新建</Button>
      </div>
      {workflows.length === 0 ? (
        <p className="text-sm text-muted-foreground">还没有工作流，点击「新建」开始。</p>
      ) : (
        workflows.map((wf) => (
          <Card key={wf.id} className="p-3 text-sm">
            <div className="flex items-center gap-2">
              <span className="font-medium">{wf.name}</span>
              <Badge variant="secondary">{(wf.def as {nodes?: unknown[]}).nodes?.length ?? 0} 节点</Badge>
              <span className="text-muted-foreground text-xs">{wf.updatedAt}</span>
              <div className="ml-auto flex gap-1">
                <Button size="sm" variant="outline" onClick={() => navigate(`/workflow/editor/${wf.id}`)}>编辑</Button>
                <Button size="sm" onClick={() => navigate(`/workflow/runs?launch=${wf.id}`)}>▶ 运行</Button>
                <Button size="sm" variant="destructive" onClick={() => remove(wf.id)}>删除</Button>
              </div>
            </div>
          </Card>
        ))
      )}
    </div>
  );
}
```

- [ ] **Step 3: Wire navigation**

```typescript
// Add route in the platform app's router:
// /workflow          → WorkflowListPage
// /workflow/editor   → WorkflowEditorPage (new, empty)
// /workflow/editor/:id → WorkflowEditorPage (load existing)
```

- [ ] **Step 4: Test smoke — page renders, can create/save/delete**

- [ ] **Step 5: Commit**

---

### Task 5: Visual workflow editor — canvas + node library

**Files:**
- Create: `apps/admin/apps/platform/src/app/workflow/editor/page.tsx`
- Create: `apps/admin/apps/platform/src/components/workflow-editor/node-library.tsx`
- Create: `apps/admin/apps/platform/src/components/workflow-editor/editor-canvas.tsx`
- Modify: `apps/admin/apps/platform/src/components/workflow-canvas.tsx` (extend `FlowNode`)

**Interfaces:**
- Consumes: `@xyflow/react` (React Flow), `WorkflowDef` types, workflow-store
- Produces: `<WorkflowEditorPage />` — 3-panel layout: node library | canvas | property inspector

- [ ] **Step 1: Build the node library panel**

Left sidebar showing all 8 node types grouped into sections:

```
🖥 Developer Nodes
  🤖 Agent (Claude/Codex/Mastra/Gemini)
  🔀 Condition (JSON Logic branch)
  ⚡ Transform (JSON Logic map)
  🔄 Loop (repeat subgraph)
  ⑃ Parallel Fork (fan-out)
  🔗 Subworkflow

⚙️ Automation Nodes
  📜 Script (shell/python/node)
  🌐 HTTP Request (REST/GraphQL)
  ▶️ Trigger (manual/schedule/webhook)
  👥 Human Approval
  ⏺ Join (AND gate)
```

Each node type has:
- `icon`: emoji
- `label`: Chinese name
- `color`: border/header color
- `defaults`: default JSON for the node spec
- **Drag source**: `onDragStart` sets the node type in `dataTransfer`

- [ ] **Step 2: Build the editor canvas**

Extends existing `WorkflowCanvas` with:
- **Drop handler**: `onDrop` creates a new React Flow node at drop position
- **Connect handler**: `onConnect` creates an edge between two handles
- **Node click**: select node → property panel shows its config
- **Delete key**: `onKeyDown` Backspace/Delete removes selected nodes/edges
- **Undo/redo**: simple history stack (push state on each mutation)

Default canvas starts with one `trigger` node (type: manual) pre-placed.

- [ ] **Step 3: Render custom nodes by type**

Each node type gets a distinct visual:
```typescript
const NODE_STYLE: Record<string, { icon: string; color: string }> = {
  agent:           { icon: "🤖", color: "#4aa3e8" },
  human_approval:  { icon: "👥", color: "#d9a13c" },
  condition:       { icon: "🔀", color: "#3fb96f" },
  transform:       { icon: "⚡", color: "#8b5cf6" },
  trigger:         { icon: "▶️", color: "#5b6270" },
  loop:            { icon: "🔄", color: "#f59e0b" },
  parallel_fork:   { icon: "⑃", color: "#06b6d4" },
  join:            { icon: "⏺", color: "#5b6270" },
  script:          { icon: "📜", color: "#ec4899" },
  http:            { icon: "🌐", color: "#10b981" },
};
```

Each node renders: icon + label + status dot (color-coded per execution state).

- [ ] **Step 4: "Export to JSON" button**

Toolbar button serializes canvas nodes+edges → `WorkflowDef` JSON → copy to clipboard.

- [ ] **Step 5: Test — drag node to canvas, connect two nodes, delete a node, export JSON**

- [ ] **Step 6: Commit**

---

### Task 6: Property inspector panel

**Files:**
- Create: `apps/admin/apps/platform/src/components/workflow-editor/property-panel.tsx`
- Modify: `apps/admin/apps/platform/src/app/workflow/editor/page.tsx` (wire selection → panel)

**Interfaces:**
- Consumes: selected node ID from React Flow `onNodeClick`
- Produces: `<PropertyPanel />` — context-sensitive form for the selected node's config

- [ ] **Step 1: Agent node properties**

When an agent node is selected, show:
```
┌─ Agent Node ──────────────────────┐
│ ID:          [w_______________]    │
│ Name:        [Write code_______]   │
│ Executor:    [claude    ▼]         │  ← dropdown: claude|codex|mastra|mock|script|http
│ System Prompt:                     │
│ ┌────────────────────────────────┐ │
│ │ 你是资深开发工程师...            │ │
│ └────────────────────────────────┘ │
│ onError:     [stop ▼]              │  ← stop | continue
│                                     │
│ ▶ Output Spec                      │
│   produces: [+ add artifact]       │
│   validator: [cmd:...]             │
│                                     │
│ ▶ Retry (advanced)                 │
│   maxAttempts: [3]                 │
└─────────────────────────────────────┘
```

- [ ] **Step 2: Condition/Transform/Trigger/Loop properties**

Each node type gets a tailored form:
- **Condition**: JSON Logic expression textarea
- **Transform**: JSON Logic expr + output artifact ID
- **Trigger**: source dropdown (manual | schedule | webhook) + cron input
- **Loop**: bodyEntry node ID + condition expr + maxIterations
- **Script**: script body textarea (with syntax highlighting), interpreter dropdown
- **HTTP**: method + URL + headers + body inputs
- **Human Approval**: approvers (any_human | member list), timeoutMs, onTimeout, onReject

- [ ] **Step 3: Two-way binding**

Property changes update the React Flow node's `data` immediately. On save/export, serialize all node data back to WorkflowDef JSON.

- [ ] **Step 4: Smoke test — select node, change property, verify JSON export includes change**

- [ ] **Step 5: Commit**

---

### Task 7: Run launcher integration

**Files:**
- Modify: `apps/admin/apps/platform/src/app/workflow/editor/page.tsx` (add ▶ Run button)
- Modify: `apps/admin/apps/platform/src/lib/control-api.ts` (reuse `launchRun`)

**Interfaces:**
- Consumes: `controlApi.launchRun(def, workspace)` from `control-api.ts`
- Produces: Run button that serializes canvas → WorkflowDef → POST /api/runs → navigate to run detail

- [ ] **Step 1: Add ▶ Run button to editor toolbar**

Button flow:
1. Serialize canvas nodes+edges to WorkflowDef JSON
2. Prompt for workspace directory (or reuse last used)
3. `POST /api/runs` with `{ workflowJSON, workspace }`
4. Navigate to `/workflow/runs/:runId` (existing run detail page with live DAG status)

- [ ] **Step 2: Handle "no runner" error**

If all machines are offline, show toast: "没有在线 machine，请先启动 daemon" with a link to the machines page.

- [ ] **Step 3: Smoke test — create simple workflow (trigger→agent), click run, see run in list**

- [ ] **Step 4: Commit**

---

## Phase 3: Polish & Integration

### Task 8: Workflow editor — navigation & layout

**Files:**
- Modify: `apps/admin/apps/platform/src/app/` (add sidebar nav item "工作流")
- Modify: existing navigation component

- [ ] **Step 1: Add sidebar link to /workflow**

- [ ] **Step 2: Layout refinement — editor takes full viewport height, canvas fills center**

- [ ] **Step 3: Commit**

---

### Task 9: Full integration test (Playwright smoke)

**Files:**
- Create: `apps/admin/e2e/workflow-editor.spec.ts`

- [ ] **Step 1: Write Playwright test**

```typescript
test("create and run a workflow", async ({ page }) => {
  await page.goto("/workflow/editor");
  // Drag agent node to canvas
  // Connect trigger → agent
  // Set system prompt
  // Click Run
  // Verify redirected to run detail page
});
```

- [ ] **Step 2: Run and verify**

```bash
cd apps/admin && bunx playwright test e2e/workflow-editor.spec.ts
```

- [ ] **Step 3: Commit**

---

