import { useCallback, useEffect, useRef, useState } from "react"
import { Bot, CircleCheck, GitBranch, Hash, MoreHorizontal, Paperclip, Plus, RotateCw, Send, ShieldQuestion, Trash2, TriangleAlert, User as UserIcon, Wrench } from "lucide-react"
import ReactMarkdown from "react-markdown"
import { useNavigate, useParams } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@workspace/ui/components/dropdown-menu"
import { Input } from "@workspace/ui/components/input"
import { ScrollArea } from "@workspace/ui/components/scroll-area"
import { Textarea } from "@workspace/ui/components/textarea"
import { toast } from "@workspace/ui/components/sonner"
import { notify } from "../../lib/notify"
import {
  controlApi,
  FEEDBACK_CATEGORIES,
  type Agent,
  type Channel,
  type ChannelMessage,
  type FeedbackCategory,
  type ProgressEntry,
} from "../../lib/control-api"

function messageText(m: ChannelMessage): string {
  try {
    return (JSON.parse(m.payloadJson) as { text?: string }).text ?? ""
  } catch {
    return m.payloadJson
  }
}

interface MsgAttachment { id: string; filename: string; mime: string }

/** 工作流消息载荷(PRD D.5:工作流事件 / 审批卡片)。 */
interface WorkflowPayload {
  kind?: "run" | "approval"
  runId?: string
  nodeId?: string
  summary?: string
  artifacts?: string[]
}

function workflowPayload(m: ChannelMessage): WorkflowPayload | null {
  if (m.authorKind !== "workflow") return null
  try {
    return JSON.parse(m.payloadJson) as WorkflowPayload
  } catch {
    return null
  }
}

function messageAttachments(m: ChannelMessage): MsgAttachment[] {
  try {
    return (JSON.parse(m.payloadJson) as { attachments?: MsgAttachment[] }).attachments ?? []
  } catch {
    return []
  }
}

function messageTask(m: ChannelMessage): string {
  try {
    return (JSON.parse(m.payloadJson) as { task?: string }).task ?? ""
  } catch {
    return ""
  }
}

function timeStr(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
}

/** 聊天内审批卡片:通过 / 拒绝(拒绝必填结构化反馈,PRD §13)。 */
function ApprovalCard({ payload, onDone }: { payload: WorkflowPayload; onDone: () => void }) {
  const [open, setOpen] = useState(false)
  const [fb, setFb] = useState<{ category: FeedbackCategory; location: string; expected: string; detail: string }>({
    category: "功能缺陷",
    location: "",
    expected: "",
    detail: "",
  })
  const [error, setError] = useState("")
  const [done, setDone] = useState("")

  if (!payload.runId || !payload.nodeId) return null

  const approve = async () => {
    try {
      await controlApi.approve(payload.runId!, payload.nodeId!, true)
      setDone("已通过")
      onDone()
    } catch (e) {
      toast.error(`通过失败:${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const reject = async () => {
    if (!fb.detail.trim()) {
      setError("请填写问题描述")
      return
    }
    try {
      await controlApi.approve(payload.runId!, payload.nodeId!, false, fb.detail, {
        category: fb.category,
        location: fb.location || undefined,
        expected: fb.expected || undefined,
        detail: fb.detail.trim(),
      })
      setOpen(false)
      setDone("已拒绝")
      onDone()
    } catch (e) {
      setError(e instanceof Error ? e.message : "提交失败")
    }
  }

  return (
    <div className="mt-2 rounded-lg border border-amber-500/40 bg-amber-500/5 p-3">
      <div className="flex items-start gap-2">
        <ShieldQuestion className="mt-0.5 size-4 shrink-0 text-amber-600 dark:text-amber-400" />
        <div className="min-w-0 flex-1">
          <p className="text-sm font-medium">等待人工审批:{payload.nodeId}</p>
          {payload.summary && <p className="mt-0.5 text-xs text-muted-foreground">{payload.summary}</p>}
          {payload.artifacts && payload.artifacts.length > 0 && (
            <ul className="mt-1 space-y-0.5">
              {payload.artifacts.map((a) => (
                <li key={a} className="text-xs text-muted-foreground">
                  产出:{a}
                </li>
              ))}
            </ul>
          )}
          {done ? (
            <p className="mt-2 text-xs font-medium text-muted-foreground">{done}</p>
          ) : (
            <div className="mt-2 flex gap-2">
              <Button size="sm" className="cursor-pointer" onClick={approve}>
                通过
              </Button>
              <Button size="sm" variant="outline" className="cursor-pointer" onClick={() => { setOpen(true); setError("") }}>
                拒绝
              </Button>
            </div>
          )}
        </div>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>拒绝审批 —— 填写反馈</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="cfb-cat" className="text-sm font-medium">
                问题类别 <span className="text-destructive">*</span>
              </label>
              <select
                id="cfb-cat"
                value={fb.category}
                onChange={(e) => setFb({ ...fb, category: e.target.value as FeedbackCategory })}
                className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
              >
                {FEEDBACK_CATEGORIES.map((c) => (
                  <option key={c} value={c}>
                    {c}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="cfb-loc" className="text-sm font-medium">位置</label>
              <Input id="cfb-loc" value={fb.location} onChange={(e) => setFb({ ...fb, location: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="cfb-exp" className="text-sm font-medium">期望结果</label>
              <Input id="cfb-exp" value={fb.expected} onChange={(e) => setFb({ ...fb, expected: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="cfb-detail" className="text-sm font-medium">
                问题描述 <span className="text-destructive">*</span>
              </label>
              <Textarea id="cfb-detail" rows={3} value={fb.detail} onChange={(e) => setFb({ ...fb, detail: e.target.value })} />
            </div>
            {error && <p className="text-sm text-destructive">{error}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" className="cursor-pointer" onClick={() => setOpen(false)}>取消</Button>
            <Button variant="destructive" className="cursor-pointer" onClick={reject} disabled={!fb.detail.trim()}>
              提交拒绝
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

export default function SessionsPage() {
  const { channelId } = useParams()
  const navigate = useNavigate()
  const [channels, setChannels] = useState<Channel[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [messages, setMessages] = useState<ChannelMessage[]>([])
  const [members, setMembers] = useState<{ memberId: string; kind: string }[]>([])
  const [draft, setDraft] = useState("")
  const [addTarget, setAddTarget] = useState("")
  const [busy, setBusy] = useState(false)
  const [createOpen, setCreateOpen] = useState(false)
  const [createName, setCreateName] = useState("")
  const [membersOpen, setMembersOpen] = useState(false)
  const [infoOpen, setInfoOpen] = useState(false)
  const [execOpen, setExecOpen] = useState(false)
  const [execution, setExecution] = useState<ProgressEntry[]>([])
  const bottomRef = useRef<HTMLDivElement>(null)
  const waitingReply = useRef(false)      // 正在等待 agent 回执
  const chatFileRef = useRef<HTMLInputElement>(null)
  const notifiedSeq = useRef(0)
  const [dissolveOpen, setDissolveOpen] = useState(false)
  const [pendingFiles, setPendingFiles] = useState<MsgAttachment[]>([])
  const [uploading, setUploading] = useState(false)
  const pendingAgentSeq = useRef(0)       // 发送时已知的最大 agent 消息 seq

  const loadChannels = useCallback(() => {
    void controlApi.channels().then((x) => setChannels(x ?? [])).catch(() => setChannels([]))
  }, [])
  const loadAgents = useCallback(() => {
    void controlApi.agents().then((x) => setAgents(x ?? [])).catch(() => setAgents([]))
  }, [])

  useEffect(() => {
    loadChannels()
    loadAgents()
    const t = setInterval(loadChannels, 5000)
    return () => clearInterval(t)
  }, [loadChannels, loadAgents])

  useEffect(() => {
    if (!channelId) return
    const load = () => {
      void controlApi.messages(channelId).then((x) => {
        const list = x ?? []
        // 按 id 合并,避免覆盖 WS 实时推送的消息(M1:fetch 与 WS 竞态)。
        setMessages((prev) => {
          const byId = new Map(prev.map((m) => [m.id, m]))
          list.forEach((m) => byId.set(m.id, m))
          return [...byId.values()]
        })
        // agent 回执到达 → 结束「执行中」。
        if (waitingReply.current && list.some((m) => m.authorKind === "agent" && m.seq > pendingAgentSeq.current)) {
          setBusy(false)
          waitingReply.current = false
        }
      }).catch(() => setMessages([]))
      void controlApi.members(channelId).then((x) => setMembers(x ?? [])).catch(() => setMembers([]))
    }
    load()
    notifiedSeq.current = 0 // 换频道重新建立基线

    // PRD §9.1 实时:频道消息 WS 推送;按 id 去重合并,避免与轮询重复。
    const proto = location.protocol === "https:" ? "wss:" : "ws:"
    const ws = new WebSocket(`${proto}//${location.host}/api/channels/${channelId}/events`)
    ws.onmessage = (e) => {
      try {
        const msg = JSON.parse(e.data) as ChannelMessage
        setMessages((prev) => (prev.some((m) => m.id === msg.id) ? prev : [...prev, msg]))
      } catch {
        // 忽略坏帧;轮询兜底。
      }
    }
    ws.onerror = () => ws.close()

    // 轮询降为兜底(断线/WS 不可用时仍能收消息)。
    const t = setInterval(load, 15000)
    return () => {
      clearInterval(t)
      ws.close()
    }
  }, [channelId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  // 页面不在前台时,新的 agent 回复 / 工作流事件走浏览器通知(自己发的不提醒)。
  useEffect(() => {
    if (messages.length === 0) return
    const maxSeq = Math.max(...messages.map((m) => m.seq))
    if (notifiedSeq.current === 0) {
      notifiedSeq.current = maxSeq // 首次加载不补发历史
      return
    }
    const fresh = messages.filter((m) => m.seq > notifiedSeq.current && m.authorKind !== "human")
    notifiedSeq.current = maxSeq
    if (fresh.length === 0) return

    const last = fresh[fresh.length - 1]!
    const wf = workflowPayload(last)
    const channelName = channels.find((c) => c.id === channelId)?.name ?? "频道"
    notify({
      title: wf?.kind === "approval" ? `待审批 · ${channelName}` : `新消息 · ${channelName}`,
      body: messageText(last).slice(0, 120) || "有新的工作流事件",
      tag: `channel-${channelId}`,
      url: `/sessions/${channelId}`,
    })
  }, [messages, channelId, channels])

  // 执行过程弹窗:打开时每秒轮询 agent 实时活动。
  useEffect(() => {
    if (!channelId || !execOpen) return
    const load = () => void controlApi.execution().then((x) => setExecution(x ?? [])).catch(() => setExecution([]))
    load()
    const t = setInterval(load, 1000)
    return () => clearInterval(t)
  }, [channelId, execOpen])

  const createChannel = async () => {
    if (!createName.trim()) return
    const c = await controlApi.createChannel(createName.trim())
    setCreateName("")
    setCreateOpen(false)
    navigate(`/sessions/${c.id}`)
  }

  const addMember = async () => {
    if (!channelId || !addTarget) return
    const kind = addTarget === "human" ? "human" : "agent"
    await controlApi.addMember(channelId, addTarget, kind)
    setAddTarget("")
    void controlApi.members(channelId).then((x) => setMembers(x ?? []))
  }

  // 重试失败的任务(agent 报错时,消息 payload 带回了 task)。
  const retryTask = async (task: string) => {
    if (!channelId || busy) return
    const hasAgent = members.some((x) => x.kind === "agent")
    if (hasAgent) {
      pendingAgentSeq.current = Math.max(0, ...messages.filter((x) => x.authorKind === "agent").map((x) => x.seq))
      waitingReply.current = true
      setBusy(true)
    }
    try {
      await controlApi.postMessage(channelId, task)
      void controlApi.messages(channelId).then((x) => setMessages(x ?? []))
    } finally {
      if (!hasAgent) setBusy(false)
    }
  }

  const removeMember = async (memberId: string, kind: string) => {
    if (!channelId) return
    await controlApi.removeMember(channelId, memberId, kind)
    void controlApi.members(channelId).then((x) => setMembers(x ?? []))
  }

  const send = async () => {
    if (!channelId || busy) return
    if (!draft.trim() && pendingFiles.length === 0) return
    const text = draft.trim()
    const files = pendingFiles
    setDraft("") // 发送瞬间即清空
    setPendingFiles([])
    const hasAgent = members.some((m) => m.kind === "agent")
    if (hasAgent) {
      pendingAgentSeq.current = Math.max(0, ...messages.filter((m) => m.authorKind === "agent").map((m) => m.seq))
      waitingReply.current = true
      setBusy(true) // agent 异步执行,回执到达后由轮询清掉「执行中」
    }
    try {
      await controlApi.postMessage(channelId, text, files.length ? files : undefined)
      void controlApi.messages(channelId).then((x) => setMessages(x ?? []))
    } catch (e) {
      toast.error(`发送失败:${e instanceof Error ? e.message : String(e)}`)
      setDraft(text) // 把内容还给用户,别让他重打
      setPendingFiles(files)
      waitingReply.current = false
      setBusy(false)
    } finally {
      if (!hasAgent) setBusy(false)
    }
  }

  const channelDialog = (
    <Dialog open={createOpen} onOpenChange={setCreateOpen}>
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>新建频道</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <label className="text-sm font-medium">频道名称</label>
          <Input
            value={createName}
            onChange={(e) => setCreateName(e.target.value)}
            placeholder="如:dev-讨论"
            onKeyDown={(e) => e.key === "Enter" && void createChannel()}
          />
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setCreateOpen(false)}>
            取消
          </Button>
          <Button onClick={createChannel} disabled={!createName.trim()}>
            创建
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )

  // ---- 频道列表视图 ----
  if (!channelId) {
    return (
      <>
      <div className="mx-auto max-w-3xl space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <h1 className="text-2xl font-semibold tracking-tight">会话区</h1>
            <p className="text-sm text-muted-foreground">创建 room/channel,邀请 agent 或 human,用聊天完成任务。</p>
          </div>
          <Button onClick={() => setCreateOpen(true)}>
            <Plus className="size-4" aria-hidden="true" />
            新建频道
          </Button>
        </div>
        <div className="space-y-2">
          {channels.map((c) => (
            <button
              key={c.id}
              onClick={() => navigate(`/sessions/${c.id}`)}
              className="group flex w-full items-center gap-3 rounded-xl border border-transparent bg-card px-4 py-3 text-left transition-all duration-200 hover:border-primary/40 hover:bg-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <span className="grid size-10 shrink-0 place-items-center rounded-lg bg-primary/10 text-primary">
                <Hash className="size-5" aria-hidden="true" />
              </span>
              <span className="min-w-0">
                <span className="block truncate font-medium">{c.name}</span>
                <span className="block truncate text-xs text-muted-foreground">{c.id}</span>
              </span>
            </button>
          ))}
          {channels.length === 0 && (
            <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">
              还没有频道,点「新建频道」创建一个。
            </p>
          )}
        </div>
      </div>
      {channelDialog}
      </>
    )
  }

  // ---- 频道聊天视图 ----
  const channel = channels.find((c) => c.id === channelId)
  const agentNames = new Map(agents.map((a) => [a.id, a.name]))

  return (
    <div className="mx-auto flex h-[calc(100svh-8rem)] max-w-4xl flex-col gap-4">
      {/* 频道头 */}
      <header className="flex flex-wrap items-center gap-2 border-b pb-3">
        <h1 className="flex items-center gap-2 text-lg font-semibold tracking-tight">
          <Hash className="size-5 text-primary" aria-hidden="true" />
          {channel?.name ?? channelId}
        </h1>
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          {members.map((m) => (
            <span
              key={m.memberId}
              className="inline-flex items-center gap-1 rounded-full border border-primary/20 bg-primary/5 px-2.5 py-1 text-xs text-foreground"
            >
              {m.kind === "agent" ? <Bot className="size-3.5 text-primary" aria-hidden="true" /> : <UserIcon className="size-3.5" aria-hidden="true" />}
              {m.kind === "agent" ? (agentNames.get(m.memberId) ?? m.memberId) : "human"}
              {m.kind === "human" && (
                <span className="rounded-full bg-primary px-1.5 py-px text-[9px] font-semibold leading-tight text-primary-foreground">owner</span>
              )}
            </span>
          ))}
          <DropdownMenu>
            <DropdownMenuTrigger
              aria-label="频道菜单"
              className="grid size-9 place-items-center rounded-md text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            >
              <MoreHorizontal className="size-4" aria-hidden="true" />
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuLabel>#{channel?.name ?? channelId}</DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onClick={() => setInfoOpen(true)}>频道信息</DropdownMenuItem>
              <DropdownMenuItem onClick={() => setMembersOpen(true)}>成员管理</DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem disabled>机器人(规划中)</DropdownMenuItem>
              <DropdownMenuItem disabled>勾子 Webhook(规划中)</DropdownMenuItem>
              {channel?.ownerId === "human" && (
                <DropdownMenuItem className="text-destructive" onClick={() => setDissolveOpen(true)}>
                  解散频道
                </DropdownMenuItem>
              )}
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </header>

      {/* 无 agent 成员提示 */}
      {!members.some((m) => m.kind === "agent") && (
        <div className="flex items-center gap-2 rounded-lg border border-dashed border-amber-500/40 bg-amber-500/5 px-3 py-2 text-xs text-amber-600">
          <Bot className="size-3.5 shrink-0" aria-hidden="true" />
          当前频道还没有 Agent 成员 —— 从右上角添加一个 Agent 后,发消息才会由它执行并回复。
        </div>
      )}

      {/* 消息区(内部滚动,占满剩余高度) */}
      <ScrollArea className="min-h-0 flex-1 rounded-xl border bg-card/40">
        <div className="space-y-4 p-4">
          {messages.map((m) => {
            const wf = workflowPayload(m)
            // 工作流消息不是"你"发的:单独走系统样式,不加头像/owner 徽章/转工作项。
            if (wf) {
              return (
                <div key={m.id} className="flex justify-center">
                  <div className="w-full max-w-[92%] rounded-lg border border-dashed bg-muted/40 px-3 py-2">
                    <div className="flex items-center gap-2 text-xs text-muted-foreground">
                      <GitBranch className="size-3.5" aria-hidden="true" />
                      <span>工作流</span>
                      <span className="ml-auto">{timeStr(m.ts)}</span>
                    </div>
                    <p className="mt-1 text-sm">{messageText(m)}</p>
                    {wf.kind === "approval" && (
                      <ApprovalCard
                        payload={wf}
                        onDone={() => void controlApi.messages(channelId!).then((x) => setMessages(x ?? []))}
                      />
                    )}
                  </div>
                </div>
              )
            }
            const isAgent = m.authorKind === "agent"
            const name = isAgent ? (agentNames.get(m.authorMemberId) ?? "agent") : "你"
            return (
              <div key={m.id} className={`flex items-start gap-2.5 ${isAgent ? "" : "flex-row-reverse"}`}>
                <span className="relative shrink-0">
                  <span
                    className={`grid size-8 place-items-center rounded-full ${
                      isAgent ? "bg-primary text-primary-foreground" : "bg-secondary text-secondary-foreground"
                    }`}
                  >
                    {isAgent ? <Bot className="size-4" aria-hidden="true" /> : <UserIcon className="size-4" aria-hidden="true" />}
                  </span>
                  {!isAgent && (
                    <span className="absolute -bottom-1 left-1/2 -translate-x-1/2 whitespace-nowrap rounded-full bg-primary px-1.5 py-px text-[9px] font-semibold leading-tight text-primary-foreground">
                      owner
                    </span>
                  )}
                </span>
                <div className={`max-w-[72%] ${isAgent ? "" : "text-right"}`}>
                  <div className={`mb-1 flex items-center gap-2 text-xs text-muted-foreground ${isAgent ? "" : "flex-row-reverse"}`}>
                    <span className="font-medium">{name}</span>
                    <span>{timeStr(m.ts)}</span>
                  </div>
                  <div
                    className={`rounded-2xl px-4 py-2.5 text-sm leading-relaxed [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-md [&_pre]:bg-black/30 [&_pre]:p-2 [&_pre]:text-xs ${
                      isAgent
                        ? "rounded-tl-sm border border-border bg-muted text-foreground"
                        : "rounded-tr-sm bg-primary text-primary-foreground"
                    }`}
                  >
                    <ReactMarkdown>{messageText(m)}</ReactMarkdown>
                    {messageAttachments(m).length > 0 && (
                      <div className="mt-2 flex flex-wrap gap-2">
                        {messageAttachments(m).map((a) => {
                          const url = `/api/attachments/${a.id}/content`
                          return (
                            <a
                              key={a.id}
                              href={url}
                              target="_blank"
                              rel="noreferrer"
                              className="block cursor-pointer overflow-hidden rounded-lg border bg-background/60 transition-colors hover:border-primary/40"
                            >
                              {a.mime.startsWith("image/") ? (
                                <img src={url} alt={a.filename} loading="lazy" className="max-h-40 w-auto object-cover" />
                              ) : a.mime.startsWith("video/") ? (
                                <video src={url} controls className="max-h-40 w-auto" />
                              ) : (
                                <span className="flex items-center gap-1.5 px-2.5 py-1.5 text-xs">
                                  <Paperclip className="size-3.5" />
                                  {a.filename}
                                </span>
                              )}
                            </a>
                          )
                        })}
                      </div>
                    )}
                  </div>
                  {!isAgent && (
                    <button
                      onClick={() => {
                        void controlApi.createWorkItemFromChannel(channelId!, { type: "task", title: messageText(m).slice(0, 60) })
                          .then(() => void controlApi.messages(channelId!).then((x) => setMessages(x ?? [])))
                      }}
                      className="mt-1 inline-flex items-center gap-1 rounded-full border border-input px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
                    >
                      转为工作项
                    </button>
                  )}
                  {isAgent && messageText(m).startsWith("⚠️") && messageTask(m) && (
                    <div className={`mt-1 ${isAgent ? "" : "hidden"}`}>
                      <button
                        onClick={() => void retryTask(messageTask(m))}
                        className="inline-flex items-center gap-1 rounded-full border border-input px-2.5 py-1 text-xs text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground"
                      >
                        <RotateCw className="size-3" aria-hidden="true" />
                        重试
                      </button>
                    </div>
                  )}
                </div>
              </div>
            )
          })}
          {busy && (
            <div className="flex items-center gap-2.5">
              <span className="grid size-8 shrink-0 place-items-center rounded-full bg-primary text-primary-foreground">
                <Bot className="size-4" aria-hidden="true" />
              </span>
              <span className="flex items-center gap-1 rounded-2xl rounded-tl-sm border bg-card px-4 py-2.5 text-sm text-muted-foreground">
                <span className="size-1.5 animate-pulse rounded-full bg-primary" />
                <span className="size-1.5 animate-pulse rounded-full bg-primary [animation-delay:150ms]" />
                <span className="size-1.5 animate-pulse rounded-full bg-primary [animation-delay:300ms]" />
                agent 执行中…
              </span>
              <Button size="sm" variant="outline" onClick={() => setExecOpen(true)}>查看执行过程</Button>
            </div>
          )}
          <div ref={bottomRef} />
        </div>
      </ScrollArea>

      {/* 输入区:底部固定,不随消息滚动 */}
      {pendingFiles.length > 0 && (
        <div className="flex shrink-0 flex-wrap gap-2 px-1 pb-1">
          {pendingFiles.map((f) => (
            <span key={f.id} className="inline-flex items-center gap-1.5 rounded-full border bg-muted px-2.5 py-1 text-xs">
              <Paperclip className="size-3" />
              {f.filename}
              <button
                type="button"
                aria-label={`移除 ${f.filename}`}
                className="cursor-pointer text-muted-foreground hover:text-destructive"
                onClick={() => setPendingFiles((prev) => prev.filter((x) => x.id !== f.id))}
              >
                ×
              </button>
            </span>
          ))}
        </div>
      )}
      <form
        className="flex shrink-0 items-center gap-2 rounded-2xl border bg-card p-2 focus-within:ring-2 focus-within:ring-ring"
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
      >
        <input
          ref={chatFileRef}
          type="file"
          multiple
          className="hidden"
          aria-label="选择要发送的文件"
          onChange={async (e) => {
            const list = e.target.files
            if (!list?.length || !channelId) return
            setUploading(true)
            try {
              // 逐个提交并即时入列:中途失败时,已成功的文件不能丢(否则它们
              // 留在服务端却没有任何消息引用,成为孤儿)。
              for (const f of Array.from(list)) {
                try {
                  const a = await controlApi.uploadChannelAttachment(channelId, f)
                  setPendingFiles((prev) => [...prev, { id: a.id, filename: a.filename, mime: a.mime }])
                } catch (err) {
                  toast.error(`${f.name} 上传失败:${err instanceof Error ? err.message : String(err)}`)
                }
              }
            } finally {
              setUploading(false)
              if (chatFileRef.current) chatFileRef.current.value = ""
            }
          }}
        />
        <Button
          type="button"
          size="icon"
          variant="ghost"
          className="size-9 shrink-0 cursor-pointer rounded-full"
          disabled={uploading}
          onClick={() => chatFileRef.current?.click()}
          title="发送文件/图片/视频"
        >
          <Paperclip className="size-4" aria-hidden="true" />
          <span className="sr-only">添加附件</span>
        </Button>
        <Input
          placeholder="给 agent 下达任务;多个 agent 时 @agent名 指定执行者"
          className="border-0 bg-transparent shadow-none focus-visible:ring-0"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <Button
          type="submit"
          size="icon"
          className="size-9 shrink-0 cursor-pointer rounded-full"
          disabled={busy || (!draft.trim() && pendingFiles.length === 0)}
        >
          <Send className="size-4" aria-hidden="true" />
          <span className="sr-only">发送</span>
        </Button>
      </form>

      {/* 解散频道:不可撤销,连同消息与成员一起删除 */}
      <Dialog open={dissolveOpen} onOpenChange={setDissolveOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle className="text-destructive">解散频道</DialogTitle>
          </DialogHeader>
          <p className="py-2 text-sm text-muted-foreground">
            将删除「{channel?.name}」的全部消息与成员,且不可撤销。
          </p>
          <DialogFooter>
            <Button variant="outline" className="cursor-pointer" onClick={() => setDissolveOpen(false)}>
              取消
            </Button>
            <Button
              variant="destructive"
              className="cursor-pointer"
              onClick={async () => {
                if (!channelId) return
                try {
                  await controlApi.deleteChannel(channelId)
                  setDissolveOpen(false)
                  navigate("/sessions")
                  void controlApi.channels().then((x) => setChannels(x ?? []))
                } catch (e) {
                  toast.error(`解散失败:${e instanceof Error ? e.message : String(e)}`)
                }
              }}
            >
              确认解散
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 成员管理 */}
      <Dialog open={membersOpen} onOpenChange={setMembersOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>成员管理</DialogTitle>
          </DialogHeader>
          <div className="space-y-3 py-2">
            <div className="flex gap-2">
              <select
                value={addTarget}
                onChange={(e) => setAddTarget(e.target.value)}
                className="h-9 flex-1 rounded-md border border-input bg-transparent px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring"
                aria-label="添加成员"
              >
                <option value="">添加成员…</option>
                {agents.map((a) => (
                  <option key={a.id} value={a.id}>
                    {a.name} (agent)
                  </option>
                ))}
                <option value="human">human (当前用户)</option>
              </select>
              <Button onClick={addMember} disabled={!addTarget}>添加</Button>
            </div>
            <div className="space-y-1">
              {members.map((m) => (
                <div key={m.memberId + m.kind} className="flex items-center justify-between rounded-md border px-3 py-2 text-sm">
                  <span className="inline-flex items-center gap-1.5">
                    {m.kind === "agent" ? <Bot className="size-4 text-primary" aria-hidden="true" /> : <UserIcon className="size-4" aria-hidden="true" />}
                    {m.kind === "agent" ? (agentNames.get(m.memberId) ?? m.memberId) : "human"}
                    {m.kind === "human" && <span className="rounded-full bg-primary px-1.5 py-px text-[9px] font-semibold text-primary-foreground">owner</span>}
                  </span>
                  <Button size="icon" variant="ghost" aria-label={`移除 ${m.memberId}`} onClick={() => removeMember(m.memberId, m.kind)}>
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              ))}
              {members.length === 0 && <p className="py-4 text-center text-sm text-muted-foreground">暂无成员。</p>}
            </div>
          </div>
        </DialogContent>
      </Dialog>

      {/* 执行过程 */}
      <Dialog open={execOpen} onOpenChange={setExecOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Agent 执行过程</DialogTitle>
          </DialogHeader>
          <ScrollArea className="h-72 rounded-md border p-3">
            <div className="space-y-2 text-sm">
              {execution.map((p, i) => (
                <div key={i} className="flex gap-2">
                  <span className="shrink-0 text-muted-foreground">{new Date(p.ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</span>
                  {p.kind === "tool" ? (
                    <span className="inline-flex items-center gap-1 text-primary"><Wrench className="size-3.5 shrink-0" aria-hidden="true" />{p.content}</span>
                  ) : p.kind === "error" ? (
                    <span className="inline-flex items-center gap-1 text-destructive"><TriangleAlert className="size-3.5 shrink-0" aria-hidden="true" />{p.content}</span>
                  ) : p.kind === "done" ? (
                    <span className="inline-flex items-center gap-1 text-green-600"><CircleCheck className="size-3.5 shrink-0" aria-hidden="true" />{p.content}</span>
                  ) : (
                    <span className="whitespace-pre-wrap text-foreground/90">{p.content}</span>
                  )}
                </div>
              ))}
              {execution.length === 0 && <p className="text-muted-foreground">暂无执行记录。</p>}
            </div>
          </ScrollArea>
        </DialogContent>
      </Dialog>

      {/* 频道信息 */}
      <Dialog open={infoOpen} onOpenChange={setInfoOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>频道信息</DialogTitle>
          </DialogHeader>
          <div className="space-y-1.5 py-2 text-sm">
            <div>名称:{channel?.name ?? channelId}</div>
            <div>ID:{channelId}</div>
            <div>创建:{channel ? new Date(channel.createdAt).toLocaleString() : "—"}</div>
          </div>
        </DialogContent>
      </Dialog>

      {channelDialog}
    </div>
  )
}
