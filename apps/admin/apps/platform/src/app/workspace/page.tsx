import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type WorkItem } from "../../lib/control-api"

const TYPE_LABEL: Record<string, string> = { requirement: "需求", task: "任务", bug: "缺陷" }
const STATUS_LABEL: Record<string, string> = { open: "待办", in_progress: "进行中", review: "评审中", done: "已完成" }
const STATUS_COLOR: Record<string, "default" | "secondary" | "outline" | "destructive"> = {
  open: "secondary",
  in_progress: "default",
  review: "outline",
  done: "default",
}

export default function WorkspacePage() {
  const navigate = useNavigate()
  const [items, setItems] = useState<WorkItem[]>([])
  const [filterType, setFilterType] = useState("")
  const [filterStatus, setFilterStatus] = useState("")
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ type: "task", title: "", description: "" })

  const load = useCallback(() => {
    void controlApi.workItems(filterType || undefined, filterStatus || undefined).then((x) => setItems(x ?? [])).catch(() => setItems([]))
  }, [filterType, filterStatus])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const create = async () => {
    if (!form.title.trim()) return
    await controlApi.createWorkItem({ type: form.type, title: form.title.trim(), description: form.description, status: "open", assigneeAgent: "", channelId: "" })
    setOpen(false)
    setForm({ type: "task", title: "", description: "" })
    load()
  }

  const setStatus = async (id: string, status: string) => {
    await controlApi.updateWorkItem(id, { status })
    load()
  }

  const remove = async (id: string) => {
    await controlApi.deleteWorkItem(id)
    load()
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">工作区</h1>
          <p className="text-sm text-muted-foreground">需求 / 任务 / 缺陷 —— 任何变化会订阅到关联的群聊频道。</p>
        </div>
        <Button onClick={() => setOpen(true)}>＋ 新建工作项</Button>
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <select value={filterType} onChange={(e) => setFilterType(e.target.value)} className="h-8 rounded-md border border-input bg-transparent px-2">
          <option value="">全部类型</option>
          <option value="requirement">需求</option>
          <option value="task">任务</option>
          <option value="bug">缺陷</option>
        </select>
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
        {items.map((it) => (
          <Card key={it.id} className="p-3">
            <div className="flex flex-wrap items-center gap-2">
              <Badge variant={it.type === "bug" ? "destructive" : "secondary"}>{TYPE_LABEL[it.type] ?? it.type}</Badge>
              <span className="font-medium">{it.title}</span>
              <Badge variant={STATUS_COLOR[it.status] ?? "secondary"}>{STATUS_LABEL[it.status] ?? it.status}</Badge>
              <div className="ml-auto flex items-center gap-2">
                <select
                  value={it.status}
                  onChange={(e) => setStatus(it.id, e.target.value)}
                  className="h-7 rounded-md border border-input bg-transparent px-2 text-xs"
                  aria-label="状态"
                >
                  <option value="open">待办</option>
                  <option value="in_progress">进行中</option>
                  <option value="review">评审中</option>
                  <option value="done">已完成</option>
                </select>
                {it.channelId && (
                  <button className="text-xs text-muted-foreground hover:text-accent-foreground" onClick={() => navigate(`/sessions/${it.channelId}`)}>
                    频道 #
                  </button>
                )}
                <Button size="icon" variant="ghost" className="size-7" aria-label="删除" onClick={() => remove(it.id)}>×</Button>
              </div>
            </div>
            {it.description && <p className="mt-1 text-sm text-muted-foreground">{it.description}</p>}
            <p className="mt-1 text-xs text-muted-foreground">{it.id} · 更新 {new Date(it.updatedAt).toLocaleString()}</p>
          </Card>
        ))}
        {items.length === 0 && <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">还没有工作项,点「新建工作项」创建,或到会话区把消息转为工作项。</p>}
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>新建工作项</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">类型</label>
              <select value={form.type} onChange={(e) => setForm({ ...form, type: e.target.value })} className="h-9 rounded-md border border-input bg-transparent px-2 text-sm">
                <option value="requirement">需求</option>
                <option value="task">任务</option>
                <option value="bug">缺陷</option>
              </select>
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">标题 *</label>
              <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如:实现登录功能" />
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">描述</label>
              <Textarea rows={3} value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>取消</Button>
            <Button onClick={create} disabled={!form.title.trim()}>创建</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
