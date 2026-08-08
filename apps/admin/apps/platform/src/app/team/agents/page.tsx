import { useCallback, useEffect, useState } from "react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type Agent } from "../../../lib/control-api"

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [editing, setEditing] = useState<Agent | null>(null)
  const [form, setForm] = useState({ name: "", runtime: "claude", model: "", systemPrompt: "" })

  const load = useCallback(() => {
    void controlApi.agents().then(setAgents).catch(() => setAgents([]))
  }, [])

  useEffect(load, [load])

  const save = async () => {
    if (!form.name.trim()) return
    if (editing) {
      await controlApi.updateAgent(editing.id, { ...editing, ...form })
    } else {
      await controlApi.createAgent({ ...form, kind: "digital_human", provider: "" })
    }
    setEditing(null)
    setForm({ name: "", runtime: "claude", model: "", systemPrompt: "" })
    load()
  }

  const remove = async (id: string) => {
    await controlApi.deleteAgent(id)
    load()
  }

  const edit = (a: Agent) => {
    setEditing(a)
    setForm({ name: a.name, runtime: a.runtime, model: a.model, systemPrompt: a.systemPrompt })
  }

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">Agent(数字人)</h1>

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">{editing ? `编辑 ${editing.name}` : "新建 Agent"}</CardTitle>
        </CardHeader>
        <CardContent className="grid gap-3">
          <Input placeholder="名称" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} />
          <div className="flex gap-3">
            <Input placeholder="runtime(claude/codex/mock)" value={form.runtime} onChange={(e) => setForm({ ...form, runtime: e.target.value })} />
            <Input placeholder="模型(可选)" value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} />
          </div>
          <Textarea placeholder="系统提示词" value={form.systemPrompt} onChange={(e) => setForm({ ...form, systemPrompt: e.target.value })} />
          <div className="flex gap-2">
            <Button onClick={save}>{editing ? "保存" : "创建"}</Button>
            {editing && (
              <Button variant="outline" onClick={() => { setEditing(null); setForm({ name: "", runtime: "claude", model: "", systemPrompt: "" }) }}>
                取消
              </Button>
            )}
          </div>
        </CardContent>
      </Card>

      <div className="grid gap-3 sm:grid-cols-2">
        {agents.map((a) => (
          <Card key={a.id}>
            <CardHeader>
              <CardTitle className="flex items-center justify-between text-base">
                <span>{a.name}</span>
                <Badge variant="secondary">{a.runtime}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-1 text-sm text-muted-foreground">
              <div>id: {a.id}</div>
              {a.systemPrompt && <div className="line-clamp-2">提示词: {a.systemPrompt}</div>}
              <div className="flex gap-2 pt-1">
                <Button size="sm" variant="outline" onClick={() => edit(a)}>
                  编辑
                </Button>
                <Button size="sm" variant="destructive" onClick={() => remove(a.id)}>
                  删除
                </Button>
              </div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
