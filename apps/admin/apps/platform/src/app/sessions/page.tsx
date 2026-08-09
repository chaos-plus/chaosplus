import { useCallback, useEffect, useRef, useState } from "react"
import { Bot, Hash, MoreHorizontal, Plus, Send, Trash2, User as UserIcon } from "lucide-react"
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
import { controlApi, type Agent, type Channel, type ChannelMessage, type ProgressEntry } from "../../lib/control-api"

function messageText(m: ChannelMessage): string {
  try {
    return (JSON.parse(m.payloadJson) as { text?: string }).text ?? ""
  } catch {
    return m.payloadJson
  }
}

function timeStr(ts: number): string {
  return new Date(ts).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })
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
        setMessages(list)
        // agent 回执到达 → 结束「执行中」。
        if (waitingReply.current && list.some((m) => m.authorKind === "agent" && m.seq > pendingAgentSeq.current)) {
          setBusy(false)
          waitingReply.current = false
        }
      }).catch(() => setMessages([]))
      void controlApi.members(channelId).then((x) => setMembers(x ?? [])).catch(() => setMembers([]))
    }
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [channelId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  // 执行过程弹窗:打开时每秒轮询 agent 实时活动。
  useEffect(() => {
    if (!channelId || !execOpen) return
    const load = () => void controlApi.execution(channelId).then((x) => setExecution(x ?? [])).catch(() => setExecution([]))
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
    const kind = addTarget.startsWith("ag-") ? "agent" : "human"
    await controlApi.addMember(channelId, addTarget, kind)
    setAddTarget("")
    void controlApi.members(channelId).then((x) => setMembers(x ?? []))
  }

  const removeMember = async (memberId: string, kind: string) => {
    if (!channelId) return
    await controlApi.removeMember(channelId, memberId, kind)
    void controlApi.members(channelId).then((x) => setMembers(x ?? []))
  }

  const send = async () => {
    if (!channelId || !draft.trim() || busy) return
    const text = draft.trim()
    setDraft("") // 发送瞬间即清空
    const hasAgent = members.some((m) => m.kind === "agent")
    if (hasAgent) {
      pendingAgentSeq.current = Math.max(0, ...messages.filter((m) => m.authorKind === "agent").map((m) => m.seq))
      waitingReply.current = true
      setBusy(true) // agent 异步执行,回执到达后由轮询清掉「执行中」
    }
    try {
      await controlApi.postMessage(channelId, text)
      void controlApi.messages(channelId).then((x) => setMessages(x ?? []))
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
                    className={`whitespace-pre-wrap rounded-2xl px-4 py-2.5 text-sm leading-relaxed ${
                      isAgent
                        ? "rounded-tl-sm border border-border bg-muted text-foreground"
                        : "rounded-tr-sm bg-primary text-primary-foreground"
                    }`}
                  >
                    {messageText(m)}
                  </div>
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
      <form
        className="flex shrink-0 items-center gap-2 rounded-2xl border bg-card p-2 focus-within:ring-2 focus-within:ring-ring"
        onSubmit={(e) => {
          e.preventDefault()
          void send()
        }}
      >
        <Input
          placeholder="给 agent 下达任务,如:写一个 hello world"
          className="border-0 bg-transparent shadow-none focus-visible:ring-0"
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
        />
        <Button type="submit" size="icon" className="size-9 shrink-0 rounded-full" disabled={busy || !draft.trim()}>
          <Send className="size-4" aria-hidden="true" />
          <span className="sr-only">发送</span>
        </Button>
      </form>

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
                    <span className="text-primary">🔧 {p.content}</span>
                  ) : p.kind === "error" ? (
                    <span className="text-destructive">⚠️ {p.content}</span>
                  ) : p.kind === "done" ? (
                    <span className="text-green-600">✅ {p.content}</span>
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
