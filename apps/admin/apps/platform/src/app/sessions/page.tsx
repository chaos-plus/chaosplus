import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { ScrollArea } from "@workspace/ui/components/scroll-area"
import { controlApi, type Agent, type Channel, type ChannelMessage } from "../../lib/control-api"

function messageText(m: ChannelMessage): string {
  try {
    return (JSON.parse(m.payloadJson) as { text?: string }).text ?? ""
  } catch {
    return m.payloadJson
  }
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
  const bottomRef = useRef<HTMLDivElement>(null)

  const loadChannels = useCallback(() => {
    void controlApi.channels().then(setChannels).catch(() => setChannels([]))
  }, [])
  const loadAgents = useCallback(() => {
    void controlApi.agents().then(setAgents).catch(() => setAgents([]))
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
      void controlApi.messages(channelId).then(setMessages).catch(() => setMessages([]))
      void controlApi.members(channelId).then(setMembers).catch(() => setMembers([]))
    }
    load()
    const t = setInterval(load, 3000)
    return () => clearInterval(t)
  }, [channelId])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  const createChannel = async () => {
    const name = prompt("频道名称")
    if (!name?.trim()) return
    const c = await controlApi.createChannel(name.trim())
    navigate(`/sessions/${c.id}`)
  }

  const addMember = async () => {
    if (!channelId || !addTarget) return
    const kind = addTarget.startsWith("ag-") ? "agent" : "human"
    await controlApi.addMember(channelId, addTarget, kind)
    setAddTarget("")
    void controlApi.members(channelId).then(setMembers)
  }

  const send = async () => {
    if (!channelId || !draft.trim() || busy) return
    setBusy(true)
    try {
      await controlApi.postMessage(channelId, draft.trim())
      setDraft("")
      await new Promise((r) => setTimeout(r, 600))
      void controlApi.messages(channelId).then(setMessages)
    } finally {
      setBusy(false)
    }
  }

  if (!channelId) {
    return (
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h1 className="text-xl font-semibold">会话区</h1>
          <Button onClick={createChannel}>＋ 新建频道</Button>
        </div>
        <div className="space-y-2">
          {channels.map((c) => (
            <Card key={c.id} className="cursor-pointer p-3 text-sm hover:bg-accent" onClick={() => navigate(`/sessions/${c.id}`)}>
              <span className="font-medium"># {c.name}</span>
              <span className="ml-2 text-muted-foreground">{c.id}</span>
            </Card>
          ))}
          {channels.length === 0 && <p className="text-sm text-muted-foreground">还没有频道,点「新建频道」。</p>}
        </div>
      </div>
    )
  }

  return (
    <div className="flex h-full flex-col gap-3">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">#{channels.find((c) => c.id === channelId)?.name ?? channelId}</h1>
        <Button variant="outline" size="sm" onClick={createChannel}>＋ 新建频道</Button>
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-muted-foreground">成员:</span>
        {members.map((m) => (
          <span key={m.memberId} className="rounded-md bg-muted px-2 py-0.5">
            {m.kind === "agent" ? "🤖" : "👤"} {m.memberId}
          </span>
        ))}
        <select
          value={addTarget}
          onChange={(e) => setAddTarget(e.target.value)}
          className="h-8 rounded-md border border-input bg-transparent px-2 text-sm"
        >
          <option value="">添加成员…</option>
          {agents.map((a) => (
            <option key={a.id} value={a.id}>
              🤖 {a.name} (agent)
            </option>
          ))}
          <option value="human">👤 human (当前用户)</option>
        </select>
        <Button size="sm" variant="outline" onClick={addMember}>添加</Button>
      </div>

      <Card className="flex min-h-0 flex-1 flex-col">
        <ScrollArea className="flex-1 p-4">
          <div className="space-y-3">
            {messages.map((m) => {
              const isAgent = m.authorKind === "agent"
              return (
                <div key={m.id} className={`flex ${isAgent ? "justify-start" : "justify-end"}`}>
                  <div className={`max-w-[80%] whitespace-pre-wrap rounded-lg px-3 py-2 text-sm ${isAgent ? "bg-muted" : "bg-primary text-primary-foreground"}`}>
                    <div className="mb-0.5 text-xs opacity-70">{isAgent ? "🤖 " + m.authorMemberId : "你"}</div>
                    {messageText(m)}
                  </div>
                </div>
              )
            })}
            <div ref={bottomRef} />
          </div>
        </ScrollArea>
        <div className="flex gap-2 border-t p-3">
          <Input
            placeholder="给 agent 下达任务,如:写一个 hello world"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && send()}
          />
          <Button onClick={send} disabled={busy || !draft.trim()}>
            {busy ? "执行中…" : "发送"}
          </Button>
        </div>
      </Card>
    </div>
  )
}
