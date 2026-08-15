/**
 * AI 控制面 API 客户端 —— 直连 server-ai 的 Huma REST（同源 /api），
 * 认证与租户上下文复用共享 IAM 会话（Cookie + X-Tenant-Id/X-Entity）。
 */

import { getEntity, getTenant } from "./iam-api";

export interface Machine {
  id: string;
  name: string;
  address: string;
  status: string;
  online: boolean;
  lastHeartbeatAt: number;
  agentCount: number;
  runtimes: string[];
}

export interface MachineDetail {
  id: string;
  name: string;
  address: string;
  status: string;
  online: boolean;
  os: string;
  registeredAt: number;
  lastHeartbeatAt: number;
  runtimes: string[];
  agents: Agent[];
}

export interface Run {
  id: string;
  status: string;
  nodes: number;
  createdAt: string;
}

export interface RunDetailResponse {
  id: string;
  status: string;
  def: {
    nodes?: Array<{ id: string; type?: string }>;
    edges?: Array<{ from: string; to: string; condition?: string }>;
  };
}

export interface Agent {
  id: string;
  name: string;
  kind: string;
  runtime: string;
  model: string;
  provider: string;
  systemPrompt: string;
  description: string;
  machineId: string;
  status: string;
  defaultChannels: string;
  handoverDoc: string;
  retiredAt: number;
  createdAt: number;
}

export interface Channel {
  id: string;
  name: string;
  ownerId: string;
  createdAt: number;
}

export interface ChannelMember {
  channelId: string;
  memberId: string;
  kind: string;
}

export interface WorkItem {
  id: string;
  type: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  dueAt: number;
  parentId: string;
  estimateHours: number;
  spentHours: number;
  progress: number;
  workflowRunId: string;
  assigneeAgent: string;
  assigneeId: string;
  channelId: string;
  requirementId: string;
  acceptanceCriteria: string;
  keyResultIds: string[];
  version: number;
  createdAt: number;
  updatedAt: number;
}

export interface TestStep {
  id?: string;
  position?: number;
  action: string;
  expectedResult: string;
}

export interface TestCase {
  id: string;
  requirementId?: string;
  title: string;
  description: string;
  preconditions: string;
  priority: string;
  status: string;
  steps: TestStep[];
  version: number;
  updatedAt: number;
}

export interface TestRun {
  id: string;
  testCaseId: string;
  environment: string;
  status: string;
  observedResult: string;
  failureSummary: string;
  startedAt: number;
  completedAt: number;
  version: number;
  createdAt: number;
}

export interface Defect {
  id: string;
  requirementId?: string;
  taskId?: string;
  testCaseId?: string;
  testRunId?: string;
  title: string;
  description: string;
  reproductionSteps: string;
  expectedResult: string;
  actualResult: string;
  severity: string;
  priority: string;
  status: string;
  resolution: string;
  resolutionNote: string;
  version: number;
  updatedAt: number;
}

export const FEEDBACK_CATEGORIES = [
  "功能缺陷",
  "样式",
  "需求偏差",
  "其他",
] as const;
export type FeedbackCategory = (typeof FEEDBACK_CATEGORIES)[number];

export interface Feedback {
  category: FeedbackCategory;
  location?: string;
  expected?: string;
  detail: string;
}

export interface Attachment {
  id: string;
  ownerType: string;
  ownerId: string;
  filename: string;
  mime: string;
  sizeBytes: number;
  createdAt: number;
  version: number;
}

export function appendUploadedImages(
  markdown: string,
  attachments: Attachment[],
  contentURL: (id: string) => string,
) {
  const images = attachments
    .filter((value) => value.mime.startsWith("image/"))
    .map((value) => `![${value.filename}](${contentURL(value.id)})`);
  return images.length
    ? [markdown.trim(), ...images].filter(Boolean).join("\n\n")
    : markdown;
}

export interface Okr {
  id: string;
  title: string;
  objective: string;
  period: string;
  keyResults: string;
  keyResultRows: Array<{
    id?: string;
    title: string;
    currentValue: string;
    targetValue: string;
    unit: string;
  }>;
  keyResultOptions: Array<{ id: string; title: string }>;
  status: string;
  periodStart: number;
  periodEnd: number;
  version: number;
  createdAt: number;
  updatedAt: number;
}

export interface PendingApproval {
  runId: string;
  nodeId: string;
  channelId: string;
  title: string;
}

export interface DashboardStats {
  runsByStatus: Record<string, number>;
  pendingApprovals: PendingApproval[];
  machinesTotal: number;
  machinesOnline: number;
  lastHeartbeatAt: number;
  costTodayUsd: number;
}

export interface ProgressEntry {
  ts: number;
  kind: string;
  content: string;
}

export interface ChannelMessage {
  seq: number;
  id: string;
  channelId: string;
  ts: number;
  authorMemberId: string;
  authorKind: string;
  payloadJson: string;
}

/** 服务端 DTO（字段与 server-ai Huma 契约一致）。 */
interface ServerMachine {
  id: string;
  tenantId: string;
  entityId: string;
  ownerId: string;
  address: string;
  status: string;
  lastHeartbeatAt: number;
  os: string;
  registeredAt: number;
  createdAt: number;
}
interface ServerAgent {
  id: string;
  name: string;
  kind: string;
  runtime: string;
  model: string;
  provider: string;
  systemPrompt: string;
  description: string;
  machineId: string;
  status: string;
  handoverDoc: string;
  retiredAt: number;
  createdAt: number;
}
interface ServerChannel {
  id: string;
  name: string;
  ownerId: string;
  createdAt: number;
}
interface ServerChannelMember {
  channelId?: string;
  memberId: string;
  kind: string;
}
interface ServerMessage {
  seq: number;
  id: string;
  channelId: string;
  createdAt: number;
  authorId: string;
  authorKind: string;
  payload: unknown;
}
interface ServerRequirement {
  id: string;
  tenantId: string;
  entityId: string;
  ownerId: string;
  parentId?: string;
  title: string;
  description: string;
  acceptanceCriteria: string;
  keyResultIds: string[];
  status: string;
  createdAt: number;
  createdBy: string;
  updatedAt: number;
  updatedBy: string;
  version: number;
}
interface ServerTask {
  id: string;
  tenantId: string;
  entityId: string;
  ownerId: string;
  requirementId?: string;
  parentId?: string;
  title: string;
  description: string;
  status: string;
  priority: string;
  dueAt: number;
  estimateMs: number;
  spentMs: number;
  progress: number;
  workflowRunId?: string;
  assigneeId?: string;
  channelId?: string;
  createdAt: number;
  createdBy: string;
  updatedAt: number;
  updatedBy: string;
  version: number;
}
interface ServerObjective {
  id: string;
  title: string;
  description: string;
  status: string;
  periodStart: number;
  periodEnd: number;
  createdAt: number;
  updatedAt: number;
  version: number;
  keyResults: Array<{
    id: string;
    title: string;
    targetValue: string;
    currentValue: string;
    unit: string;
  }>;
}
interface ServerAttachment {
  id: string;
  resourceType: string;
  resourceId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  createdAt: number;
  version: number;
}
interface ServerRun {
  id: string;
  status: string;
  createdAt: number;
  def?: {
    nodes?: Array<{ id: string; type?: string }>;
    edges?: Array<{ from: string; to: string; condition?: string }>;
  };
}

const base = "/control/api";

function headersFor(extra?: Record<string, string>): Headers {
  const headers = new Headers(extra);
  const tenant = getTenant();
  const entity = getEntity();
  if (tenant) headers.set("X-Tenant-Id", tenant);
  if (entity) headers.set("X-Entity-Id", entity);
  return headers;
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const headers = headersFor();
  if (init?.body && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  const res = await fetch(base + path, { ...init, headers });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as {
      error?: string;
      detail?: string;
    };
    throw new Error(body.detail ?? body.error ?? `HTTP ${res.status}`);
  }
  return (await res.json()) as T;
}

async function upload(
  resourceType: string,
  resourceId: string,
  file: File,
): Promise<Attachment> {
  const fd = new FormData();
  fd.append("resourceType", resourceType);
  fd.append("resourceId", resourceId);
  fd.append("file", file);
  const res = await fetch(`${base}/attachments`, {
    method: "POST",
    body: fd,
    headers: headersFor(),
  });
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as {
      error?: string;
      detail?: string;
    };
    throw new Error(
      body.detail ?? body.error ?? `上传失败(HTTP ${res.status})`,
    );
  }
  const created = (await res.json()) as { data?: ServerAttachment };
  return toAttachment(created.data ?? (created as unknown as ServerAttachment));
}

async function attachmentContent(id: string): Promise<Blob> {
  const res = await fetch(
    `${base}/attachments/${encodeURIComponent(id)}/content`,
    { headers: headersFor() },
  );
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as {
      error?: string;
      detail?: string;
    };
    throw new Error(
      body.detail ?? body.error ?? `下载失败(HTTP ${res.status})`,
    );
  }
  return res.blob();
}

function toAttachment(a: ServerAttachment): Attachment {
  return {
    id: a.id,
    ownerType: a.resourceType,
    ownerId: a.resourceId,
    filename: a.filename,
    mime: a.contentType,
    sizeBytes: a.sizeBytes,
    createdAt: a.createdAt,
    version: a.version,
  };
}

function toMachine(m: ServerMachine): Machine {
  return {
    id: m.id,
    name: m.id,
    address: m.address,
    status: m.status,
    online: m.status === "confirmed",
    lastHeartbeatAt: m.lastHeartbeatAt,
    agentCount: 0,
    runtimes: [],
  };
}

function toAgent(a: ServerAgent): Agent {
  return {
    id: a.id,
    name: a.name,
    kind: a.kind,
    runtime: a.runtime,
    model: a.model,
    provider: a.provider,
    systemPrompt: a.systemPrompt,
    description: a.description,
    machineId: a.machineId,
    status: a.status,
    defaultChannels: "",
    handoverDoc: a.handoverDoc,
    retiredAt: a.retiredAt,
    createdAt: a.createdAt,
  };
}

function toChannel(c: ServerChannel): Channel {
  return { id: c.id, name: c.name, ownerId: c.ownerId, createdAt: c.createdAt };
}

function toMember(m: ServerChannelMember): ChannelMember {
  return { channelId: m.channelId ?? "", memberId: m.memberId, kind: m.kind };
}

function toMessage(m: ServerMessage): ChannelMessage {
  return {
    seq: m.seq,
    id: m.id,
    channelId: m.channelId,
    ts: m.createdAt,
    authorMemberId: m.authorId,
    authorKind: m.authorKind,
    payloadJson:
      typeof m.payload === "string"
        ? m.payload
        : JSON.stringify(m.payload ?? {}),
  };
}

function toWorkItem(type: string, r: ServerRequirement | ServerTask): WorkItem {
  const task = type === "task" ? (r as ServerTask) : undefined;
  const requirement =
    type === "requirement" ? (r as ServerRequirement) : undefined;
  return {
    id: r.id,
    type,
    title: r.title,
    description: r.description,
    status: r.status,
    priority: task?.priority ?? "",
    dueAt: task?.dueAt ?? 0,
    parentId: r.parentId ?? "",
    estimateHours: (task?.estimateMs ?? 0) / 3_600_000,
    spentHours: (task?.spentMs ?? 0) / 3_600_000,
    progress: task?.progress ?? 0,
    workflowRunId: task?.workflowRunId ?? "",
    assigneeAgent: task?.assigneeId ?? r.ownerId,
    assigneeId: task?.assigneeId ?? "",
    channelId: task?.channelId ?? "",
    requirementId: task?.requirementId ?? "",
    acceptanceCriteria: requirement?.acceptanceCriteria ?? "",
    keyResultIds: requirement?.keyResultIds ?? [],
    version: r.version,
    createdAt: r.createdAt,
    updatedAt: r.updatedAt,
  };
}

function toOkr(o: ServerObjective): Okr {
  return {
    id: o.id,
    title: o.title,
    objective: o.description,
    period: `${new Date(o.periodStart).toISOString().slice(0, 10)} / ${new Date(o.periodEnd).toISOString().slice(0, 10)}`,
    keyResults: o.keyResults
      .map((k) => `${k.title}|${k.currentValue}|${k.targetValue}|${k.unit}`)
      .join("\n"),
    keyResultRows: o.keyResults.map((keyResult) => ({
      id: keyResult.id,
      title: keyResult.title,
      currentValue: keyResult.currentValue,
      targetValue: keyResult.targetValue,
      unit: keyResult.unit,
    })),
    keyResultOptions: o.keyResults.map((keyResult) => ({
      id: keyResult.id,
      title: keyResult.title,
    })),
    status: o.status,
    periodStart: o.periodStart,
    periodEnd: o.periodEnd,
    version: o.version,
    createdAt: o.createdAt,
    updatedAt: o.updatedAt,
  };
}

function toRun(r: ServerRun): Run {
  return {
    id: r.id,
    status: r.status,
    nodes: r.def?.nodes?.length ?? 0,
    createdAt: new Date(r.createdAt).toISOString(),
  };
}

function workItemKind(type: string): "requirement" | "task" {
  if (type === "requirement" || type === "task") return type;
  throw new Error(`不支持的工作项类型:${type}`);
}

function parsePeriod(period: string): [number, number] {
  const [start, end] = period.split("/").map((value) => value.trim());
  const periodStart = Date.parse(`${start}T00:00:00Z`);
  const periodEnd = Date.parse(`${end}T23:59:59.999Z`);
  if (
    !Number.isFinite(periodStart) ||
    !Number.isFinite(periodEnd) ||
    periodEnd < periodStart
  )
    throw new Error("周期格式应为 YYYY-MM-DD / YYYY-MM-DD");
  return [periodStart, periodEnd];
}

export function machineConnectCommand(token: string): string {
  const origin =
    typeof window === "undefined"
      ? "http://127.0.0.1:8080"
      : window.location.origin;
  // Pass the token via env (RUNNER_TOKEN) so it never lands on argv, where any
  // local process could read it via /proc/<pid>/cmdline (PRD §17.2).
  return `RUNNER_TOKEN=${token} bun run src/serve.ts --server ${origin}/api/machines/ws`;
}

export const controlApi = {
  machines: async () => {
    const r = await req<{ data: ServerMachine[] }>("/machines");
    return (r.data ?? (r as unknown as ServerMachine[])).map(toMachine);
  },
  machineDetail: async (id: string) => {
    const r = await req<{ data: ServerMachine }>(`/machines/${id}`);
    const machine = r.data ?? (r as unknown as ServerMachine);
    const agents = await controlApi.agents().catch(() => []);
    const detail: MachineDetail = {
      ...toMachine(machine),
      os: machine.os,
      registeredAt: machine.registeredAt,
      agents: agents.filter((a) => a.machineId === machine.id),
    };
    return detail;
  },
  issueToken: () =>
    req<{
      token: string;
      machineId: string;
      longTerm: false;
      expiresAt: number;
    }>("/machines/tokens", {
      method: "POST",
    }),
  onboardingStatus: (id: string, token: string) =>
    req<{
      state: "waiting" | "connected" | "confirmed" | "expired" | "invalid";
      expiresAt?: number;
    }>(`/machines/${id}/onboarding-status`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  confirmMachine: (id: string, token: string) =>
    req<{ ok: boolean }>(`/machines/${id}/confirm`, {
      method: "POST",
      body: JSON.stringify({ token }),
    }),
  cancelMachine: (id: string) =>
    req<{ ok: boolean }>(`/machines/${id}`, { method: "DELETE" }),
  refreshToken: (id: string) =>
    req<{ token: string; longTerm: boolean }>(`/machines/${id}/refresh-token`, {
      method: "POST",
    }),
  machineToken: async (id: string) => {
    const r = await req<{ data?: { token: string } }>(
      `/machines/${id}/refresh-token`,
      { method: "POST" },
    );
    const body = r.data ?? (r as unknown as { token: string });
    return { token: body.token };
  },

  runs: async () => {
    const r = await req<{ data: ServerRun[] }>("/runs");
    return (r.data ?? (r as unknown as ServerRun[])).map(toRun);
  },
  runDetail: async (runId: string): Promise<RunDetailResponse> => {
    const r = await req<{ data: ServerRun }>(`/runs/${runId}`);
    const run = r.data ?? (r as unknown as ServerRun);
    return {
      id: run.id,
      status: run.status,
      def: run.def ?? { nodes: [], edges: [] },
    };
  },
  dashboard: async (): Promise<DashboardStats> => {
    const [runs, machines] = await Promise.all([
      controlApi.runs().catch(() => []),
      controlApi.machines().catch(() => []),
    ]);
    const runsByStatus: Record<string, number> = {};
    for (const run of runs)
      runsByStatus[run.status] = (runsByStatus[run.status] ?? 0) + 1;
    return {
      runsByStatus,
      pendingApprovals: [],
      machinesTotal: machines.length,
      machinesOnline: machines.filter((m) => m.online).length,
      lastHeartbeatAt: Math.max(0, ...machines.map((m) => m.lastHeartbeatAt)),
      costTodayUsd: 0,
    };
  },
  launchRun: (workflowJSON: unknown, workspace: string) =>
    req<{ runId: string }>("/runs", {
      method: "POST",
      body: JSON.stringify({ workflowJSON, workspace }),
    }),
  approve: (
    runId: string,
    nodeId: string,
    approve: boolean,
    reason?: string,
    feedback?: Feedback,
  ) =>
    req<{ ok: boolean }>(`/runs/${runId}/approvals/${nodeId}`, {
      method: "POST",
      body: JSON.stringify({ approve, reason, feedback }),
    }),
  pauseRun: (runId: string) =>
    req<{ ok: boolean }>(`/runs/${runId}/pause`, { method: "POST" }),
  resumeRun: (runId: string) =>
    req<{ ok: boolean }>(`/runs/${runId}/resume`, { method: "POST" }),
  cancelRun: (runId: string) =>
    req<{ ok: boolean }>(`/runs/${runId}/cancel`, { method: "POST" }),

  agents: async () => {
    const r = await req<{ data: ServerAgent[] }>("/agents");
    return (r.data ?? (r as unknown as ServerAgent[])).map(toAgent);
  },
  createAgent: (a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>("/agents", { method: "POST", body: JSON.stringify(a) }),
  updateAgent: (id: string, a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>(`/agents/${id}`, { method: "PATCH", body: JSON.stringify(a) }),
  deleteAgent: (id: string) =>
    req<{ ok: boolean }>(`/agents/${id}`, { method: "DELETE" }),
  setAgentStatus: (id: string, status: "running" | "stopped") =>
    req<{ ok: boolean; status: string }>(`/agents/${id}/status`, {
      method: "POST",
      body: JSON.stringify({ status }),
    }),
  retireAgent: (
    id: string,
    body: {
      force?: boolean;
      confirm?: string;
      successor?: string;
      reason?: string;
    },
  ) =>
    req<{ ok: boolean; handoverDoc: string }>(`/agents/${id}/retire`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  channels: async () => {
    const r = await req<{ data: ServerChannel[] }>("/channels");
    return (r.data ?? (r as unknown as ServerChannel[])).map(toChannel);
  },
  createChannel: async (name: string) => {
    const r = await req<{ data: ServerChannel }>("/channels", {
      method: "POST",
      body: JSON.stringify({ name }),
    });
    return toChannel(r.data ?? (r as unknown as ServerChannel));
  },
  deleteChannel: (id: string) =>
    req<{ ok: boolean }>(`/channels/${id}`, { method: "DELETE" }),
  addMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members`, {
      method: "POST",
      body: JSON.stringify({ memberId, kind }),
    }),
  removeMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members/${memberId}/${kind}`, {
      method: "DELETE",
    }),
  members: async (id: string) => {
    const r = await req<{ data: ServerChannelMember[] }>(
      `/channels/${id}/members`,
    );
    return (r.data ?? (r as unknown as ServerChannelMember[])).map((m) =>
      toMember({ channelId: id, ...m }),
    );
  },
  messages: async (id: string) => {
    const r = await req<{ data: ServerMessage[] }>(`/channels/${id}/messages`);
    return (r.data ?? (r as unknown as ServerMessage[])).map(toMessage);
  },
  execution: async () => [] as ProgressEntry[],
  postMessage: (
    id: string,
    text: string,
    attachments?: Array<{ id: string; filename: string; mime: string }>,
  ) =>
    req<{ ok: boolean }>(`/channels/${id}/messages`, {
      method: "POST",
      body: JSON.stringify({ text, attachments }),
    }),
  uploadChannelAttachment: (channelId: string, file: File) =>
    upload("conversation", channelId, file),

  workItems: async (type?: string, status?: string): Promise<WorkItem[]> => {
    const kinds = type
      ? [workItemKind(type)]
      : (["requirement", "task"] as const);
    const q = status ? `?status=${encodeURIComponent(status)}` : "";
    const result: WorkItem[] = [];
    for (const kind of kinds) {
      const r = await req<{ data: Array<ServerRequirement | ServerTask> }>(
        `/${kind === "requirement" ? "requirements" : "tasks"}${q}`,
      );
      for (const item of r.data ??
        (r as unknown as Array<ServerRequirement | ServerTask>)) {
        result.push(
          toWorkItem(kind === "requirement" ? "requirement" : "task", item),
        );
      }
    }
    return result;
  },
  createWorkItem: async (
    w: Partial<Omit<WorkItem, "id" | "createdAt" | "updatedAt">>,
  ) => {
    const kind = workItemKind(w.type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    const r = await req<{ data: ServerRequirement | ServerTask }>(path, {
      method: "POST",
      body: JSON.stringify(
        kind === "requirement"
          ? {
              title: w.title,
              description: w.description,
              acceptanceCriteria: w.acceptanceCriteria ?? "",
              parentId: w.parentId || undefined,
              keyResultIds: w.keyResultIds ?? [],
            }
          : {
              title: w.title,
              description: w.description,
              parentId: w.parentId || undefined,
              requirementId: w.requirementId || undefined,
              estimateMs: Math.round((w.estimateHours ?? 0) * 3_600_000),
              priority: w.priority || "medium",
              dueAt: w.dueAt || 0,
              assigneeId: w.assigneeId || undefined,
              channelId: w.channelId || undefined,
            },
      ),
    });
    return toWorkItem(
      kind === "requirement" ? "requirement" : "task",
      r.data ?? (r as unknown as ServerRequirement),
    );
  },
  updateWorkItem: async (id: string, w: Partial<WorkItem>) => {
    const kind = workItemKind(w.type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    const r = await req<{ data: ServerRequirement | ServerTask }>(
      `${path}/${id}`,
      {
        method: "PATCH",
        body: JSON.stringify({
          status: w.status,
          title: w.title,
          description: w.description,
          acceptanceCriteria:
            kind === "requirement" ? w.acceptanceCriteria : undefined,
          keyResultIds: kind === "requirement" ? w.keyResultIds : undefined,
          estimateMs:
            kind === "task" && w.estimateHours !== undefined
              ? Math.round(w.estimateHours * 3_600_000)
              : undefined,
          priority: kind === "task" ? w.priority : undefined,
          dueAt: kind === "task" ? w.dueAt : undefined,
          assigneeId: kind === "task" ? w.assigneeId || undefined : undefined,
          version: w.version,
        }),
      },
    );
    return toWorkItem(
      kind === "requirement" ? "requirement" : "task",
      r.data ?? (r as unknown as ServerRequirement),
    );
  },
  deleteWorkItem: async (id: string, type: string, version: number) => {
    const kind = workItemKind(type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    return req<{ ok: boolean }>(`${path}/${id}?version=${version}`, {
      method: "DELETE",
    });
  },
  createWorkItemFromChannel: async (
    channelId: string,
    w: { type: string; title: string; description?: string },
  ) => controlApi.createWorkItem({ ...w, channelId }),
  executeWorkItem: (id: string, version: number) =>
    req<{ runId: string }>(`/tasks/${id}/executions`, {
      method: "POST",
      body: JSON.stringify({ version }),
    }),
  attachments: async (ownerType: string, ownerId: string) => {
    if (
      !["requirement", "task", "objective", "testcase", "defect"].includes(
        ownerType,
      )
    )
      throw new Error(`不支持的附件类型:${ownerType}`);
    const q = new URLSearchParams({
      resourceType: ownerType,
      resourceId: ownerId,
    });
    const r = await req<{ data: ServerAttachment[] }>(
      `/attachments?${q.toString()}`,
    );
    return (r.data ?? (r as unknown as ServerAttachment[])).map(toAttachment);
  },
  uploadAttachment: (ownerType: string, ownerId: string, file: File) =>
    upload(ownerType, ownerId, file),
  attachmentContentUrl: (id: string) =>
    `${base}/attachments/${encodeURIComponent(id)}/content`,
  attachmentContent,
  deleteAttachment: (id: string, version: number) =>
    req<{ ok: boolean }>(
      `/attachments/${encodeURIComponent(id)}?version=${version}`,
      { method: "DELETE" },
    ),
  testCases: async (status?: string) => {
    const q = status ? `?status=${encodeURIComponent(status)}` : "";
    const r = await req<{ data: TestCase[] }>(`/test-cases${q}`);
    return r.data ?? (r as unknown as TestCase[]);
  },
  createTestCase: async (
    value: Omit<TestCase, "id" | "status" | "version" | "updatedAt">,
  ) => {
    const r = await req<{ data: TestCase }>("/test-cases", {
      method: "POST",
      body: JSON.stringify(value),
    });
    return r.data ?? (r as unknown as TestCase);
  },
  updateTestCase: async (
    id: string,
    value: Partial<TestCase> & { version: number },
  ) => {
    const r = await req<{ data: TestCase }>(`/test-cases/${id}`, {
      method: "PATCH",
      body: JSON.stringify(value),
    });
    return r.data ?? (r as unknown as TestCase);
  },
  deleteTestCase: (id: string, version: number) =>
    req<{ ok: boolean }>(`/test-cases/${id}?version=${version}`, {
      method: "DELETE",
    }),
  testRuns: async (testCaseId?: string) => {
    const q = testCaseId ? `?testCaseId=${encodeURIComponent(testCaseId)}` : "";
    const r = await req<{ data: TestRun[] }>(`/test-runs${q}`);
    return r.data ?? (r as unknown as TestRun[]);
  },
  createTestRun: async (testCaseId: string, environment: string) => {
    const r = await req<{ data: TestRun }>("/test-runs", {
      method: "POST",
      body: JSON.stringify({ testCaseId, environment }),
    });
    return r.data ?? (r as unknown as TestRun);
  },
  updateTestRun: async (
    id: string,
    value: {
      status: string;
      observedResult?: string;
      failureSummary?: string;
      version: number;
    },
  ) => {
    const r = await req<{ data: TestRun }>(`/test-runs/${id}`, {
      method: "PATCH",
      body: JSON.stringify(value),
    });
    return r.data ?? (r as unknown as TestRun);
  },
  defects: async (status?: string) => {
    const q = status ? `?status=${encodeURIComponent(status)}` : "";
    const r = await req<{ data: Defect[] }>(`/defects${q}`);
    return r.data ?? (r as unknown as Defect[]);
  },
  createDefect: async (
    value: Omit<
      Defect,
      | "id"
      | "status"
      | "resolution"
      | "resolutionNote"
      | "version"
      | "updatedAt"
    >,
  ) => {
    const r = await req<{ data: Defect }>("/defects", {
      method: "POST",
      body: JSON.stringify(value),
    });
    return r.data ?? (r as unknown as Defect);
  },
  updateDefect: async (
    id: string,
    value: Partial<Defect> & { version: number },
  ) => {
    const r = await req<{ data: Defect }>(`/defects/${id}`, {
      method: "PATCH",
      body: JSON.stringify(value),
    });
    return r.data ?? (r as unknown as Defect);
  },
  deleteDefect: (id: string, version: number) =>
    req<{ ok: boolean }>(`/defects/${id}?version=${version}`, {
      method: "DELETE",
    }),
  okrs: async () => {
    const r = await req<{ data: ServerObjective[] }>("/objectives");
    return (r.data ?? (r as unknown as ServerObjective[])).map(toOkr);
  },
  createOkr: async (
    o: Pick<Okr, "title" | "objective" | "period" | "keyResultRows">,
  ) => {
    const [periodStart, periodEnd] = parsePeriod(o.period);
    const r = await req<{ data: ServerObjective }>("/objectives", {
      method: "POST",
      body: JSON.stringify({
        title: o.title,
        description: o.objective,
        periodStart,
        periodEnd,
        keyResults: o.keyResultRows,
      }),
    });
    return toOkr(r.data ?? (r as unknown as ServerObjective));
  },
  updateOkr: async (
    id: string,
    o: Pick<
      Okr,
      "title" | "objective" | "period" | "keyResultRows" | "version"
    >,
  ) => {
    const [periodStart, periodEnd] = parsePeriod(o.period);
    const r = await req<{ data: ServerObjective }>(`/objectives/${id}`, {
      method: "PATCH",
      body: JSON.stringify({
        title: o.title,
        description: o.objective,
        periodStart,
        periodEnd,
        keyResults: o.keyResultRows,
        version: o.version,
      }),
    });
    return toOkr(r.data ?? (r as unknown as ServerObjective));
  },
  deleteOkr: (id: string, version: number) =>
    req<{ ok: boolean }>(`/objectives/${id}?version=${version}`, {
      method: "DELETE",
    }),
};
