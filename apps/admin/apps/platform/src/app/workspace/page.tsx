import { useCallback, useEffect, useState } from "react"
import { useNavigate, useParams } from "react-router"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type WorkItem } from "../../lib/control-api"

const TYPE_LABEL: Record<string, string> = { requirement: "需求", task: "任务", test: "测试", bug: "缺陷" }
const STATUS_LABEL: Record<string, string> = { open: "待办", in_progress: "进行中", review: "评审中", done: "已完成" }
const STATUS_COLOR: Record<string, "default" | "secondary" | "outline" | "destructive"> = {
  open: "secondary",
  in_progress: "default",
  review: "outline",
  done: "default",
}

export default function WorkspacePage() {
  const { type = "task" } = useParams()
  const navigate = useNavigate()
  const [items, setItems] = useState<WorkItem[]>([])
  const [filterStatus, setFilterStatus] = useState("")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ title: "", description: "", parent: "" })

  const load = useCallback(() => {
    void controlApi.workItems(type, filterStatus || undefined).then((x) => setItems(x ?? [])).catch(() => setItems([]))
  }, [type, filterStatus])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const create = async () => {
    if (!form.title.trim()) return
    await controlApi.createWorkItem({ type, title: form.title.trim(), description: form.description, status: "open", assigneeAgent: "", channelId: "", ...(form.parent ? { parentId: form.parent } : {}) })
    setOpen(false)
    setForm({ title: "", description: "", parent: "" })
    load()
  }

  const setStatus = async (id: string, status: string) => {
    await controlApi.updateWorkItem(id, { status })
    load()
  }

  const execute = async (id: string) => {
    const r = await controlApi.executeWorkItem(id)
    navigate(`/workflow/runs/${r.runId}`)
  }

  const childrenOf = (id: string) => items.filter((it) => it.parentId === id)
  const roots = items.filter((it) => !it.parentId)

  const renderItem = (it: WorkItem, depth: number): React.ReactNode => (
    <div key={it.id}>
      <Card className="p-3" style={{ marginLeft: depth * 16 }}>
        <div className="flex flex-wrap items-center gap-2">
          <Badge variant={it.type === "bug" ? "destructive" : "secondary"}>{TYPE_LABEL[it.type] ?? it.type}</Badge>
          <span className="font-medium">{it.title}</span>
          <Badge variant={STATUS_COLOR[it.status] ?? "secondary"}>{STATUS_LABEL[it.status] ?? it.status}</Badge>
          {it.progress > 0 && <span className="text-xs text-muted-foreground">{it.progress}%</span>}
          {it.estimateHours > 0 && <span className="text-xs text-muted-foreground">估 {it.estimateHours}h</span>}
          {it.spentHours > 0 && <span className="text-xs text-muted-foreground">耗 {it.spentHours.toFixed(1)}h</span>}
          <div className="ml-auto flex items-center gap-2">
            <select value={it.status} onChange={(e) => setStatus(it.id, e.target.value)} className="h-7 rounded-md border border-input bg-transparent px-2 text-xs" aria-label="状态">
              <option value="open">待办</option>
              <option value="in_progress">进行中</option>
              <option value="review">评审中</option>
              <option value="done">已完成</option>
            </select>
            {it.status !== "done" && (
              <Button size="sm" variant="outline" onClick={() => execute(it.id)}>执行</Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => navigate(`/sessions/${it.channelId}`)} disabled={!it.channelId} title="关联频道">
              频道
            </Button>
          </div>
        </div>
        {it.description && <p className="mt-1 text-sm text-muted-foreground">{it.description}</p>}
        <div className="mt-1 flex gap-3 text-xs text-muted-foreground">
          <span>{it.id}</span>
          <button className="hover:text-accent-foreground" onClick={() => setForm({ title: "", description: "", parent: it.id })}>
            + 子任务
          </button>
          <button className="hover:text-accent-foreground" onClick={() => navigate(`/workflow/runs/${it.workflowRunId}`)} disabled={!it.workflowRunId}>
            最近 run
          </button>
        </div>
      </Card>
      {childrenOf(it.id).map((c) => renderItem(c, depth + 1))}
    </div>
  )

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{TYPE_LABEL[type] ?? type}管理</h1>
          <p className="text-sm text-muted-foreground">工作项执行走工作流;变化订阅到关联群聊。</p>
        </div>
        <Button onClick={() => setOpen(true)}>＋ 新建{TYPE_LABEL[type] ?? type}</Button>
      </div>

      <div className="flex items-center gap-2 text-sm">
        <select value={filterStatus} onChange={(e) => setFilterStatus(e.target.value)} className="h-8 rounded-md border border-input bg-transparent px-2">
          <option value="">全部状态</option>
          <option value="open">待办</option>
          <option value="in_progress">进行中</option>
          <option value="review">评审中</option>
          <option value="done">已完成</option>
        </select>
        <span className="text-muted-foreground">{items.length} 项</span>
      </div>

      <div className="space-y-2">
        {roots.map((it) => renderItem(it, 0))}
        {roots.length === 0 && <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">还没有{TYPE_LABEL[type]},点「新建」创建,或到会话区把消息转为工作项。</p>}
      </div>

      <Dialog open={open || !!form.parent} onOpenChange={(v) => { setOpen(v); if (!v) setForm({ title: "", description: "", parent: "" }) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>{form.parent ? "新建子任务" : `新建${TYPE_LABEL[type]}`}</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">标题 *</label>
              <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder={`如:实现登录功能`} />
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">描述</label>
              <Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
            {form.parent && <p className="text-xs text-muted-foreground">父级:{form.parent}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setOpen(false); setForm({ title: "", description: "", parent: "" }) }}>取消</Button>
            <Button onClick={create} disabled={!form.title.trim()}>创建</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
