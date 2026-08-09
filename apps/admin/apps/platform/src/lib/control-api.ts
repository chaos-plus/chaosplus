/** 控制面(chaos.plus workflow)API 客户端 —— 经 /control 代理到 :8081。 */

export interface Machine {
  id: string
  name: string
  address: string
  status: string
  online: boolean
  lastHeartbeatAt: number
  /** 该机托管的数字人数(PRD D.3)。 */
  agentCount: number
  /** 该机检测到的执行器,供数字人 runtime 下拉。 */
  runtimes: string[]
}

export interface Run {
  id: string
  status: string
  nodes: number
  createdAt: string
}

export interface Agent {
  id: string
  name: string
  kind: string
  runtime: string
  model: string
  provider: string
  systemPrompt: string
  description: string
  machineId: string
  status: string // running | stopped | retired
  defaultChannels: string
  handoverDoc: string
  retiredAt: number
  createdAt: number
}

export interface Channel {
  id: string
  name: string
  createdAt: number
}

export interface ChannelMember {
  channelId: string
  memberId: string
  kind: string
}

export interface WorkItem {
  id: string
  type: string // requirement | task | test | bug
  title: string
  description: string
  status: string // open | in_progress | review | done
  parentId: string
  estimateHours: number
  spentHours: number
  progress: number
  workflowRunId: string
  assigneeAgent: string
  channelId: string
  createdAt: number
  updatedAt: number
}

/** 结构化拒绝反馈(PRD §13):category 与 detail 必填。 */
export const FEEDBACK_CATEGORIES = ["功能缺陷", "样式", "需求偏差", "其他"] as const
export type FeedbackCategory = (typeof FEEDBACK_CATEGORIES)[number]

export interface Feedback {
  category: FeedbackCategory
  location?: string
  expected?: string
  detail: string
}

export interface Attachment {
  id: string
  ownerType: string
  ownerId: string
  filename: string
  mime: string
  sizeBytes: number
  createdAt: number
}

export interface Okr {
  id: string
  title: string
  objective: string
  period: string
  keyResults: string
  createdAt: number
  updatedAt: number
}

export interface PendingApproval {
  runId: string
  nodeId: string
  channelId: string
  title: string
}

/** PRD D.1 仪表盘数据源。 */
export interface DashboardStats {
  runsByStatus: Record<string, number>
  pendingApprovals: PendingApproval[]
  machinesTotal: number
  machinesOnline: number
  lastHeartbeatAt: number
  costTodayUsd: number
}

export interface ProgressEntry {
  ts: number
  kind: string // message | tool | spawn | done | error
  content: string
}

export interface ChannelMessage {
  seq: number
  id: string
  channelId: string
  ts: number
  authorMemberId: string
  authorKind: string
  payloadJson: string
}

const base = "/control/api"

/** 上传走 FormData(不能带 JSON header),但错误信息要和 req() 一样能看见。 */
async function upload(path: string, file: File): Promise<Attachment> {
  const fd = new FormData()
  fd.append("file", file)
  const res = await fetch(base + path, { method: "POST", body: fd })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `上传失败(HTTP ${res.status})`)
  }
  return res.json() as Promise<Attachment>
}

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(base + path, {
    headers: { "Content-Type": "application/json" },
    ...init,
  })
  if (!res.ok) {
    const body = (await res.json().catch(() => ({}))) as { error?: string }
    throw new Error(body.error ?? `HTTP ${res.status}`)
  }
  return res.json() as Promise<T>
}

export const controlApi = {
  // machines (PRD §5.3.1)
  machines: () => req<Machine[]>("/machines"),
  issueToken: () => req<{ token: string; machineId: string; expiresIn: number }>("/machines/tokens", { method: "POST" }),
  confirmMachine: (id: string, token: string) =>
    req<{ ok: boolean }>(`/machines/${id}/confirm`, { method: "POST", body: JSON.stringify({ token }) }),
  cancelMachine: (id: string) => req<{ ok: boolean }>(`/machines/${id}`, { method: "DELETE" }),
  refreshToken: (id: string) => req<{ token: string; longTerm: boolean }>(`/machines/${id}/refresh-token`, { method: "POST" }),

  // runs + approvals
  runs: () => req<Run[]>("/runs"),
  dashboard: () => req<DashboardStats>("/stats/dashboard"),
  launchRun: (workflowJSON: unknown, workspace: string) =>
    req<{ runId: string }>("/runs", { method: "POST", body: JSON.stringify({ workflowJSON, workspace }) }),
  approve: (runId: string, nodeId: string, approve: boolean, reason?: string, feedback?: Feedback) =>
    req<{ ok: boolean }>(`/runs/${runId}/approvals/${nodeId}`, {
      method: "POST",
      body: JSON.stringify({ approve, reason, feedback }),
    }),

  // agents (团队管理)
  agents: () => req<Agent[]>("/agents"),
  createAgent: (a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>("/agents", { method: "POST", body: JSON.stringify(a) }),
  updateAgent: (id: string, a: Partial<Omit<Agent, "id" | "createdAt">>) =>
    req<Agent>(`/agents/${id}`, { method: "PUT", body: JSON.stringify(a) }),
  deleteAgent: (id: string) => req<{ ok: boolean }>(`/agents/${id}`, { method: "DELETE" }),
  setAgentStatus: (id: string, status: "running" | "stopped") =>
    req<{ ok: boolean; status: string }>(`/agents/${id}/status`, { method: "POST", body: JSON.stringify({ status }) }),
  retireAgent: (id: string, body: { force?: boolean; confirm?: string; successor?: string; reason?: string }) =>
    req<{ ok: boolean; handoverDoc: string }>(`/agents/${id}/retire`, { method: "POST", body: JSON.stringify(body) }),

  // channels (会话区)
  channels: () => req<Channel[]>("/channels"),
  createChannel: (name: string) => req<Channel>("/channels", { method: "POST", body: JSON.stringify({ name }) }),
  addMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members`, { method: "POST", body: JSON.stringify({ memberId, kind }) }),
  removeMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members/${memberId}/${kind}`, { method: "DELETE" }),
  members: (id: string) => req<ChannelMember[]>(`/channels/${id}/members`),
  messages: (id: string) => req<ChannelMessage[]>(`/channels/${id}/messages`),
  execution: (id: string) => req<ProgressEntry[]>(`/channels/${id}/execution`),
  postMessage: (id: string, text: string, attachments?: Array<{ id: string; filename: string; mime: string }>) =>
    req<{ ok: boolean }>(`/channels/${id}/messages`, { method: "POST", body: JSON.stringify({ text, attachments }) }),
  uploadChannelAttachment: (channelId: string, file: File) => upload(`/channels/${channelId}/attachments`, file),

  // 工作区 work-items(需求/任务/缺陷)
  workItems: (type?: string, status?: string) => {
    const q = new URLSearchParams()
    if (type) q.set("type", type)
    if (status) q.set("status", status)
    const s = q.toString()
    return req<WorkItem[]>(`/work-items${s ? `?${s}` : ""}`)
  },
  createWorkItem: (w: Partial<Omit<WorkItem, "id" | "createdAt" | "updatedAt">>) =>
    req<WorkItem>("/work-items", { method: "POST", body: JSON.stringify(w) }),
  updateWorkItem: (id: string, w: Partial<WorkItem>) =>
    req<WorkItem>(`/work-items/${id}`, { method: "PUT", body: JSON.stringify(w) }),
  deleteWorkItem: (id: string) => req<{ ok: boolean }>(`/work-items/${id}`, { method: "DELETE" }),
  createWorkItemFromChannel: (channelId: string, w: { type: string; title: string; description?: string }) =>
    req<WorkItem>(`/channels/${channelId}/work-items`, { method: "POST", body: JSON.stringify(w) }),
  executeWorkItem: (id: string) => req<{ runId: string }>(`/work-items/${id}/execute`, { method: "POST" }),
  attachments: (id: string) => req<Attachment[]>(`/work-items/${id}/attachments`),
  uploadAttachment: (id: string, file: File) => upload(`/work-items/${id}/attachments`, file),
  okrs: () => req<Okr[]>("/okrs"),
  createOkr: (o: Omit<Okr, "id" | "createdAt" | "updatedAt">) => req<Okr>("/okrs", { method: "POST", body: JSON.stringify(o) }),
  updateOkr: (id: string, o: Partial<Okr>) => req<Okr>(`/okrs/${id}`, { method: "PUT", body: JSON.stringify(o) }),
  deleteOkr: (id: string) => req<{ ok: boolean }>(`/okrs/${id}`, { method: "DELETE" }),
}
