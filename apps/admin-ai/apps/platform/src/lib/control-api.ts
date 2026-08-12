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
  parentId: string;
  estimateHours: number;
  spentHours: number;
  progress: number;
  workflowRunId: string;
  assigneeAgent: string;
  channelId: string;
  createdAt: number;
  updatedAt: number;
}

export const FEEDBACK_CATEGORIES = ["功能缺陷", "样式", "需求偏差", "其他"] as const;
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
}

export interface Okr {
  id: string;
  title: string;
  objective: string;
  period: string;
  keyResults: string;
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
  parentId?: string;
  title: string;
  description: string;
  status: string;
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
  keyResults: Array<{ id: string; title: string; targetValue: string; currentValue: string }>;
}
interface ServerAttachment {
  id: string;
  resourceType: string;
  resourceId: string;
  filename: string;
  contentType: string;
  sizeBytes: number;
  createdAt: number;
}
interface ServerRun {
  id: string;
  status: string;
  createdAt: number;
  def?: { nodes?: Array<{ id: string; type?: string }>; edges?: Array<{ from: string; to: string; condition?: string }> };
}

const base = "/api";

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
    const body = (await res.json().catch(() => ({}))) as { error?: string; detail?: string };
    throw new Error(body.detail ?? body.error ?? `HTTP ${res.status}`);
  }
  return (await res.json()) as T;
}

async function upload(resourceType: string, resourceId: string, file: File): Promise<Attachment> {
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
    const body = (await res.json().catch(() => ({}))) as { error?: string; detail?: string };
    throw new Error(body.detail ?? body.error ?? `上传失败(HTTP ${res.status})`);
  }
  const created = (await res.json()) as { data?: ServerAttachment };
  return toAttachment(created.data ?? (created as unknown as ServerAttachment));
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
    payloadJson: typeof m.payload === "string" ? m.payload : JSON.stringify(m.payload ?? {}),
  };
}

function toWorkItem(type: string, r: ServerRequirement | ServerTask): WorkItem {
  return {
    id: r.id,
    type,
    title: r.title,
    description: r.description,
    status: r.status,
    parentId: r.parentId ?? "",
    estimateHours: 0,
    spentHours: 0,
    progress: 0,
    workflowRunId: "",
    assigneeAgent: r.ownerId,
    channelId: "",
    createdAt: r.createdAt,
    updatedAt: r.updatedAt,
  };
}

function toOkr(o: ServerObjective): Okr {
  return {
    id: o.id,
    title: o.title,
    objective: o.description,
    period: `${o.periodStart}-${o.periodEnd}`,
    keyResults: o.keyResults.map((k) => `${k.title}: ${k.currentValue}/${k.targetValue}`).join("\n"),
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
  return type === "task" ? "task" : "requirement";
}

export function machineConnectCommand(token: string): string {
  const origin = typeof window === "undefined" ? "http://127.0.0.1:8080" : window.location.origin;
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
    req<{ token: string; machineId: string; longTerm: false; expiresAt: number }>("/machines/tokens", {
      method: "POST",
    }),
  onboardingStatus: (id: string, token: string) =>
    req<{ state: "waiting" | "connected" | "confirmed" | "expired" | "invalid"; expiresAt?: number }>(
      `/machines/${id}/onboarding-status`,
      { method: "POST", body: JSON.stringify({ token }) },
    ),
  confirmMachine: (id: string, token: string) =>
    req<{ ok: boolean }>(`/machines/${id}/confirm`, { method: "POST", body: JSON.stringify({ token }) }),
  cancelMachine: (id: string) => req<{ ok: boolean }>(`/machines/${id}`, { method: "DELETE" }),
  refreshToken: (id: string) =>
    req<{ token: string; longTerm: boolean }>(`/machines/${id}/refresh-token`, { method: "POST" }),
  machineToken: async (id: string) => {
    const r = await req<{ data?: { token: string } }>(`/machines/${id}/refresh-token`, { method: "POST" });
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
    return { id: run.id, status: run.status, def: run.def ?? { nodes: [], edges: [] } };
  },
  dashboard: async (): Promise<DashboardStats> => {
    const [runs, machines] = await Promise.all([controlApi.runs().catch(() => []), controlApi.machines().catch(() => [])]);
    const runsByStatus: Record<string, number> = {};
    for (const run of runs) runsByStatus[run.status] = (runsByStatus[run.status] ?? 0) + 1;
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
    req<{ runId: string }>("/runs", { method: "POST", body: JSON.stringify({ workflowJSON, workspace }) }),
  approve: (runId: string, nodeId: string, approve: boolean, reason?: string, feedback?: Feedback) =>
    req<{ ok: boolean }>(`/runs/${runId}/approvals/${nodeId}`, {
      method: "POST",
      body: JSON.stringify({ approve, reason, feedback }),
    }),
  pauseRun: (runId: string) => req<{ ok: boolean }>(`/runs/${runId}/pause`, { method: "POST" }),
  resumeRun: (runId: string) => req<{ ok: boolean }>(`/runs/${runId}/resume`, { method: "POST" }),
  cancelRun: (runId: string) => req<{ ok: boolean }>(`/runs/${runId}/cancel`, { method: "POST" }),

  agents: async () => {
    const r = await req<{ data: ServerAgent[] }>("/agents");
    return (r.data ?? (r as unknown as ServerAgent[])).map(toAgent);
  },
  createAgent: (a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>("/agents", { method: "POST", body: JSON.stringify(a) }),
  updateAgent: (id: string, a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>(`/agents/${id}`, { method: "PATCH", body: JSON.stringify(a) }),
  deleteAgent: (id: string) => req<{ ok: boolean }>(`/agents/${id}`, { method: "DELETE" }),
  setAgentStatus: (id: string, status: "running" | "stopped") =>
    req<{ ok: boolean; status: string }>(`/agents/${id}/status`, { method: "POST", body: JSON.stringify({ status }) }),
  retireAgent: (id: string, body: { force?: boolean; confirm?: string; successor?: string; reason?: string }) =>
    req<{ ok: boolean; handoverDoc: string }>(`/agents/${id}/retire`, { method: "POST", body: JSON.stringify(body) }),

  channels: async () => {
    const r = await req<{ data: ServerChannel[] }>("/channels");
    return (r.data ?? (r as unknown as ServerChannel[])).map(toChannel);
  },
  createChannel: async (name: string) => {
    const r = await req<{ data: ServerChannel }>("/channels", { method: "POST", body: JSON.stringify({ name }) });
    return toChannel(r.data ?? (r as unknown as ServerChannel));
  },
  deleteChannel: (id: string) => req<{ ok: boolean }>(`/channels/${id}`, { method: "DELETE" }),
  addMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members`, { method: "POST", body: JSON.stringify({ memberId, kind }) }),
  removeMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members/${memberId}/${kind}`, { method: "DELETE" }),
  members: async (id: string) => {
    const r = await req<{ data: ServerChannelMember[] }>(`/channels/${id}/members`);
    return (r.data ?? (r as unknown as ServerChannelMember[])).map((m) => toMember({ channelId: id, ...m }));
  },
  messages: async (id: string) => {
    const r = await req<{ data: ServerMessage[] }>(`/channels/${id}/messages`);
    return (r.data ?? (r as unknown as ServerMessage[])).map(toMessage);
  },
  execution: async () => [] as ProgressEntry[],
  postMessage: (id: string, text: string, attachments?: Array<{ id: string; filename: string; mime: string }>) =>
    req<{ ok: boolean }>(`/channels/${id}/messages`, { method: "POST", body: JSON.stringify({ text, attachments }) }),
  uploadChannelAttachment: (channelId: string, file: File) => upload("conversation", channelId, file),

  workItems: async (type?: string, status?: string): Promise<WorkItem[]> => {
    const kinds = type ? [workItemKind(type)] : (["requirement", "task"] as const);
    const q = status ? `?status=${encodeURIComponent(status)}` : "";
    const result: WorkItem[] = [];
    for (const kind of kinds) {
      const r = await req<{ data: Array<ServerRequirement | ServerTask> }>(`/${kind === "requirement" ? "requirements" : "tasks"}${q}`);
      for (const item of r.data ?? (r as unknown as Array<ServerRequirement | ServerTask>)) {
        result.push(toWorkItem(kind === "requirement" ? "requirement" : "task", item));
      }
    }
    return result;
  },
  createWorkItem: async (w: Partial<Omit<WorkItem, "id" | "createdAt" | "updatedAt">>) => {
    const kind = workItemKind(w.type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    const r = await req<{ data: ServerRequirement | ServerTask }>(path, {
      method: "POST",
      body: JSON.stringify({ title: w.title, description: w.description, parentId: w.parentId || undefined }),
    });
    return toWorkItem(kind === "requirement" ? "requirement" : "task", r.data ?? (r as unknown as ServerRequirement));
  },
  updateWorkItem: async (id: string, w: Partial<WorkItem>) => {
    const kind = workItemKind(w.type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    const r = await req<{ data: ServerRequirement | ServerTask }>(`${path}/${id}`, {
      method: "PATCH",
      body: JSON.stringify({ status: w.status, title: w.title, description: w.description }),
    });
    return toWorkItem(kind === "requirement" ? "requirement" : "task", r.data ?? (r as unknown as ServerRequirement));
  },
  deleteWorkItem: async (id: string, type?: string) => {
    const kind = workItemKind(type ?? "requirement");
    const path = kind === "requirement" ? "/requirements" : "/tasks";
    return req<{ ok: boolean }>(`${path}/${id}`, { method: "DELETE" });
  },
  createWorkItemFromChannel: async (channelId: string, w: { type: string; title: string; description?: string }) =>
    controlApi.createWorkItem({ ...w, channelId }),
  executeWorkItem: (id: string) => req<{ runId: string }>(`/tasks/${id}/executions`, { method: "POST" }),
  attachments: async (ownerType: string, ownerId: string) => {
    const kind = workItemKind(ownerType);
    const q = new URLSearchParams({ resourceType: kind === "task" ? "task" : "requirement", resourceId: ownerId });
    const r = await req<{ data: ServerAttachment[] }>(`/attachments?${q.toString()}`);
    return (r.data ?? (r as unknown as ServerAttachment[])).map(toAttachment);
  },
  uploadAttachment: (ownerType: string, ownerId: string, file: File) =>
    upload(workItemKind(ownerType) === "task" ? "task" : "requirement", ownerId, file),
  okrs: async () => {
    const r = await req<{ data: ServerObjective[] }>("/objectives");
    return (r.data ?? (r as unknown as ServerObjective[])).map(toOkr);
  },
  createOkr: async (o: Omit<Okr, "id" | "createdAt" | "updatedAt">) => {
    const r = await req<{ data: ServerObjective }>("/objectives", {
      method: "POST",
      body: JSON.stringify({ title: o.title, description: o.objective }),
    });
    return toOkr(r.data ?? (r as unknown as ServerObjective));
  },
  updateOkr: (id: string, o: Partial<Okr>) =>
    req<Okr>(`/objectives/${id}`, { method: "PATCH", body: JSON.stringify({ title: o.title, description: o.objective }) }),
  deleteOkr: (id: string) => req<{ ok: boolean }>(`/objectives/${id}`, { method: "DELETE" }),
};
