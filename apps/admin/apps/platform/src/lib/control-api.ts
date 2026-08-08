/** 控制面(chaos.plus workflow)API 客户端 —— 经 /control 代理到 :8081。 */

export interface Machine {
  id: string
  name: string
  address: string
  status: string
  online: boolean
  lastHeartbeatAt: number
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
  launchRun: (workflowJSON: unknown, workspace: string) =>
    req<{ runId: string }>("/runs", { method: "POST", body: JSON.stringify({ workflowJSON, workspace }) }),
  approve: (runId: string, nodeId: string, approve: boolean, reason?: string) =>
    req<{ ok: boolean }>(`/runs/${runId}/approvals/${nodeId}`, {
      method: "POST",
      body: JSON.stringify({ approve, reason }),
    }),

  // agents (团队管理)
  agents: () => req<Agent[]>("/agents"),
  createAgent: (a: Omit<Agent, "id" | "createdAt">) =>
    req<Agent>("/agents", { method: "POST", body: JSON.stringify(a) }),
  updateAgent: (id: string, a: Omit<Agent, "id" | "createdAt">) =>
    req<Agent>(`/agents/${id}`, { method: "PUT", body: JSON.stringify(a) }),
  deleteAgent: (id: string) => req<{ ok: boolean }>(`/agents/${id}`, { method: "DELETE" }),

  // channels (会话区)
  channels: () => req<Channel[]>("/channels"),
  createChannel: (name: string) => req<Channel>("/channels", { method: "POST", body: JSON.stringify({ name }) }),
  addMember: (id: string, memberId: string, kind: string) =>
    req<{ ok: boolean }>(`/channels/${id}/members`, { method: "POST", body: JSON.stringify({ memberId, kind }) }),
  members: (id: string) => req<ChannelMember[]>(`/channels/${id}/members`),
  messages: (id: string) => req<ChannelMessage[]>(`/channels/${id}/messages`),
  postMessage: (id: string, text: string) =>
    req<{ ok: boolean }>(`/channels/${id}/messages`, { method: "POST", body: JSON.stringify({ text }) }),
}
