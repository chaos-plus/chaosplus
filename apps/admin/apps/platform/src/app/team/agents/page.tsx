import { useCallback, useEffect, useState } from "react"
import { Pencil, Plus, Trash2 } from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@workspace/ui/components/table"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type Agent } from "../../../lib/control-api"

const empty = { name: "", runtime: "claude", model: "", systemPrompt: "" }

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Agent | null>(null)
  const [form, setForm] = useState(empty)

  const load = useCallback(() => {
    void controlApi.agents().then((x) => setAgents(x ?? [])).catch(() => setAgents([]))
  }, [])

  useEffect(load, [load])

  const openCreate = () => {
    setEditing(null)
    setForm(empty)
    setOpen(true)
  }

  const openEdit = (a: Agent) => {
    setEditing(a)
    setForm({ name: a.name, runtime: a.runtime, model: a.model, systemPrompt: a.systemPrompt })
    setOpen(true)
  }

  const save = async () => {
    if (!form.name.trim()) return
    if (editing) {
      await controlApi.updateAgent(editing.id, { ...editing, ...form })
    } else {
      await controlApi.createAgent({ ...form, kind: "digital_human", provider: "" })
    }
    setOpen(false)
    load()
  }

  const remove = async (id: string) => {
    await controlApi.deleteAgent(id)
    load()
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">Agent(数字人)</h1>
          <p className="text-sm text-muted-foreground">团队的 Agent 数字人,可加入会话区频道执行任务。</p>
        </div>
        <Button onClick={openCreate}>
          <Plus className="size-4" aria-hidden="true" />
          新建 Agent
        </Button>
      </div>

      <div className="rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>Runtime</TableHead>
              <TableHead>模型</TableHead>
              <TableHead>系统提示词</TableHead>
              <TableHead className="w-24 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {agents.map((a) => (
              <TableRow key={a.id}>
                <TableCell className="font-medium">{a.name}</TableCell>
                <TableCell>
                  <Badge variant="secondary">{a.runtime}</Badge>
                </TableCell>
                <TableCell className="text-muted-foreground">{a.model || "—"}</TableCell>
                <TableCell className="max-w-xs truncate text-muted-foreground">{a.systemPrompt || "—"}</TableCell>
                <TableCell className="text-right">
                  <div className="inline-flex gap-1">
                    <Button size="icon" variant="ghost" onClick={() => openEdit(a)} aria-label={`编辑 ${a.name}`}>
                      <Pencil className="size-4" />
                    </Button>
                    <Button size="icon" variant="ghost" onClick={() => remove(a.id)} aria-label={`删除 ${a.name}`}>
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {agents.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-sm text-muted-foreground">
                  还没有 Agent,点「新建 Agent」创建一个。
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{editing ? `编辑 ${editing.name}` : "新建 Agent"}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">名称 *</label>
              <Input value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="如:hello-agent" />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div className="grid gap-1.5">
                <label className="text-sm font-medium">Runtime</label>
                <Input value={form.runtime} onChange={(e) => setForm({ ...form, runtime: e.target.value })} placeholder="claude/codex/mock" />
              </div>
              <div className="grid gap-1.5">
                <label className="text-sm font-medium">模型</label>
                <Input value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="可选" />
              </div>
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">系统提示词</label>
              <Textarea rows={3} value={form.systemPrompt} onChange={(e) => setForm({ ...form, systemPrompt: e.target.value })} />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button onClick={save} disabled={!form.name.trim()}>
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
