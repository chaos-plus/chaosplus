import { useCallback, useEffect, useState } from "react"
import { Bot, CirclePlay, CircleStop, Plus, UserMinus } from "lucide-react"
import { Button } from "@workspace/ui/components/button"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@workspace/ui/components/table"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type Agent, type Machine } from "../../../lib/control-api"

const STATUS_LABEL: Record<string, string> = { running: "运行中", stopped: "已停止", retired: "已注销" }
const STATUS_TONE: Record<string, string> = {
  running: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
  stopped: "bg-muted text-muted-foreground",
  retired: "bg-destructive/10 text-destructive",
}

const EMPTY_FORM = {
  name: "",
  description: "",
  systemPrompt: "",
  machineId: "",
  runtime: "",
  model: "",
  provider: "",
  defaultChannels: "# ALL",
}

function fail(action: string, e: unknown) {
  toast.error(`${action}失败:${e instanceof Error ? e.message : String(e)}`)
}

export default function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [machines, setMachines] = useState<Machine[]>([])
  const [editing, setEditing] = useState<Agent | null>(null)
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ ...EMPTY_FORM })
  const [retiring, setRetiring] = useState<Agent | null>(null)

  const load = useCallback(() => {
    void controlApi.agents().then((x) => setAgents(x ?? [])).catch(() => setAgents([]))
    void controlApi.machines().then((x) => setMachines(x ?? [])).catch(() => setMachines([]))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  const machineName = (id: string) => machines.find((m) => m.id === id)?.name ?? (id || "未指定")
  /** runtime 选项来自所选 machine 的检测结果(PRD D.4);离线时退回已知运行时。 */
  const runtimeOptions = (() => {
    const m = machines.find((x) => x.id === form.machineId)
    return m?.runtimes?.length ? m.runtimes : ["claude", "codex", "mock"]
  })()

  const openCreate = () => {
    setEditing(null)
    setForm({ ...EMPTY_FORM })
    setOpen(true)
  }

  const openEdit = (a: Agent) => {
    setEditing(a)
    setForm({
      name: a.name,
      description: a.description,
      systemPrompt: a.systemPrompt,
      machineId: a.machineId,
      runtime: a.runtime,
      model: a.model,
      provider: a.provider,
      defaultChannels: a.defaultChannels || "# ALL",
    })
    setOpen(true)
  }

  const save = async () => {
    if (!form.name.trim() || !form.systemPrompt.trim() || !form.machineId || !form.runtime) return
    try {
      if (editing) await controlApi.updateAgent(editing.id, form)
      else await controlApi.createAgent(form)
      setOpen(false)
      load()
    } catch (e) {
      fail(editing ? "保存" : "创建", e)
    }
  }

  const toggle = async (a: Agent) => {
    try {
      await controlApi.setAgentStatus(a.id, a.status === "running" ? "stopped" : "running")
      load()
    } catch (e) {
      fail(a.status === "running" ? "停止" : "启动", e)
    }
  }

  const active = agents.filter((a) => a.status !== "retired")
  const retired = agents.filter((a) => a.status === "retired")

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">数字人</h1>
          <p className="text-sm text-muted-foreground">每个数字人绑定一台 machine 上的执行器;注销会产出交接文档。</p>
        </div>
        <Button className="cursor-pointer gap-1.5" onClick={openCreate}>
          <Plus className="size-4" />
          新建数字人
        </Button>
      </div>

      <div className="overflow-x-auto rounded-lg border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>所属 machine</TableHead>
              <TableHead>运行时</TableHead>
              <TableHead className="text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {active.map((a) => (
              <TableRow key={a.id}>
                <TableCell>
                  <div className="flex items-center gap-2">
                    <Bot className="size-4 text-muted-foreground" aria-hidden="true" />
                    <div className="min-w-0">
                      <p className="truncate font-medium">{a.name}</p>
                      {a.description && <p className="truncate text-xs text-muted-foreground">{a.description}</p>}
                    </div>
                  </div>
                </TableCell>
                <TableCell>
                  <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${STATUS_TONE[a.status] ?? STATUS_TONE.stopped}`}>
                    {STATUS_LABEL[a.status] ?? a.status}
                  </span>
                </TableCell>
                <TableCell className="text-sm">{machineName(a.machineId)}</TableCell>
                <TableCell className="text-sm">
                  {a.runtime}
                  {a.model && <span className="text-muted-foreground"> · {a.model}</span>}
                </TableCell>
                <TableCell>
                  <div className="flex justify-end gap-1.5">
                    <Button size="sm" variant="outline" className="cursor-pointer gap-1" onClick={() => toggle(a)}>
                      {a.status === "running" ? <CircleStop className="size-3.5" /> : <CirclePlay className="size-3.5" />}
                      {a.status === "running" ? "停止" : "启动"}
                    </Button>
                    <Button size="sm" variant="ghost" className="cursor-pointer" onClick={() => openEdit(a)}>
                      详情
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      className="cursor-pointer gap-1 text-muted-foreground hover:text-destructive"
                      onClick={() => setRetiring(a)}
                    >
                      <UserMinus className="size-3.5" />
                      注销
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
            {active.length === 0 && (
              <TableRow>
                <TableCell colSpan={5} className="py-10 text-center text-sm text-muted-foreground">
                  还没有数字人。先在「机器 Machines」接入一台 machine,再来这里创建。
                </TableCell>
              </TableRow>
            )}
          </TableBody>
        </Table>
      </div>

      {retired.length > 0 && (
        <details className="rounded-lg border p-3">
          <summary className="cursor-pointer text-sm font-medium">已注销({retired.length})</summary>
          <ul className="mt-2 space-y-2">
            {retired.map((a) => (
              <li key={a.id} className="text-sm">
                <span className="font-medium">{a.name}</span>
                <span className="ml-2 text-xs text-muted-foreground">
                  {a.retiredAt ? new Date(a.retiredAt).toLocaleString() : ""}
                </span>
                {a.handoverDoc && (
                  <pre className="mt-1 max-h-40 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">{a.handoverDoc}</pre>
                )}
              </li>
            ))}
          </ul>
        </details>
      )}

      <AgentFormDialog
        open={open}
        editing={editing}
        form={form}
        setForm={setForm}
        machines={machines}
        runtimeOptions={runtimeOptions}
        onClose={() => setOpen(false)}
        onSave={save}
      />
      <RetireDialog agent={retiring} agents={active} onClose={() => setRetiring(null)} onDone={load} />
    </div>
  )
}

function AgentFormDialog({
  open,
  editing,
  form,
  setForm,
  machines,
  runtimeOptions,
  onClose,
  onSave,
}: {
  open: boolean
  editing: Agent | null
  form: typeof EMPTY_FORM
  setForm: (f: typeof EMPTY_FORM) => void
  machines: Machine[]
  runtimeOptions: string[]
  onClose: () => void
  onSave: () => void
}) {
  const invalid = !form.name.trim() || !form.systemPrompt.trim() || !form.machineId || !form.runtime
  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="max-h-[85vh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{editing ? `编辑数字人 · ${editing.name}` : "新建数字人"}</DialogTitle>
        </DialogHeader>
        <div className="grid gap-3 py-2">
          <div className="grid gap-1.5">
            <label htmlFor="ag-name" className="text-sm font-medium">
              名称 <span className="text-destructive">*</span>
            </label>
            <Input id="ag-name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="如:be-dev" />
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="ag-desc" className="text-sm font-medium">描述</label>
            <Input id="ag-desc" value={form.description} onChange={(e) => setForm({ ...form, description: e.target.value })} />
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="ag-prompt" className="text-sm font-medium">
              系统提示词 <span className="text-destructive">*</span>
            </label>
            <Textarea
              id="ag-prompt"
              rows={4}
              value={form.systemPrompt}
              onChange={(e) => setForm({ ...form, systemPrompt: e.target.value })}
              placeholder="职责与边界,例如:你是后端开发,负责 Go/API/数据库。"
            />
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="ag-machine" className="text-sm font-medium">
              所属 machine <span className="text-destructive">*</span>
            </label>
            <select
              id="ag-machine"
              value={form.machineId}
              onChange={(e) => setForm({ ...form, machineId: e.target.value, runtime: "" })}
              className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
            >
              <option value="">请选择</option>
              {machines.map((m) => (
                <option key={m.id} value={m.id}>
                  {m.name} {m.online ? "(在线)" : "(离线)"}
                </option>
              ))}
            </select>
            {machines.length === 0 && <p className="text-xs text-muted-foreground">还没有 machine,请先到「机器 Machines」接入。</p>}
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="ag-runtime" className="text-sm font-medium">
              运行时 <span className="text-destructive">*</span>
            </label>
            <select
              id="ag-runtime"
              value={form.runtime}
              onChange={(e) => setForm({ ...form, runtime: e.target.value })}
              className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
              disabled={!form.machineId}
            >
              <option value="">{form.machineId ? "请选择" : "请先选择 machine"}</option>
              {runtimeOptions.map((rt) => (
                <option key={rt} value={rt}>
                  {rt}
                </option>
              ))}
            </select>
            <p className="text-xs text-muted-foreground">选项来自该机上报的检测结果。</p>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <div className="grid gap-1.5">
              <label htmlFor="ag-model" className="text-sm font-medium">模型</label>
              <Input id="ag-model" value={form.model} onChange={(e) => setForm({ ...form, model: e.target.value })} placeholder="留空用默认" />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="ag-provider" className="text-sm font-medium">供应商</label>
              <Input id="ag-provider" value={form.provider} onChange={(e) => setForm({ ...form, provider: e.target.value })} placeholder="anthropic / bedrock…" />
            </div>
          </div>
          <div className="grid gap-1.5">
            <label htmlFor="ag-channels" className="text-sm font-medium">默认频道</label>
            <Input id="ag-channels" value={form.defaultChannels} onChange={(e) => setForm({ ...form, defaultChannels: e.target.value })} />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" className="cursor-pointer" onClick={onClose}>取消</Button>
          <Button className="cursor-pointer" onClick={onSave} disabled={invalid}>
            {editing ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** 注销向导(§6.2.1):正常注销走交接;强制注销需输入名称二次确认。 */
function RetireDialog({
  agent,
  agents,
  onClose,
  onDone,
}: {
  agent: Agent | null
  agents: Agent[]
  onClose: () => void
  onDone: () => void
}) {
  const [force, setForce] = useState(false)
  const [successor, setSuccessor] = useState("")
  const [reason, setReason] = useState("")
  const [confirm, setConfirm] = useState("")
  const [doc, setDoc] = useState("")

  useEffect(() => {
    setForce(false)
    setSuccessor("")
    setReason("")
    setConfirm("")
    setDoc("")
  }, [agent])

  if (!agent) return null

  const submit = async () => {
    try {
      const res = await controlApi.retireAgent(agent.id, { force, confirm, successor, reason })
      setDoc(res.handoverDoc || "(强制注销,未生成交接文档)")
      onDone()
    } catch (e) {
      fail("注销", e)
    }
  }

  return (
    <Dialog open onOpenChange={(v) => !v && onClose()}>
      <DialogContent className={`sm:max-w-md ${force ? "border-destructive" : ""}`}>
        <DialogHeader>
          <DialogTitle className={force ? "text-destructive" : ""}>
            {force ? "强制注销" : "注销数字人"} · {agent.name}
          </DialogTitle>
        </DialogHeader>

        {doc ? (
          <div className="grid gap-2 py-2">
            <p className="text-sm">已注销。交接文档:</p>
            <pre className="max-h-64 overflow-auto rounded bg-muted p-2 text-xs whitespace-pre-wrap">{doc}</pre>
          </div>
        ) : (
          <div className="grid gap-3 py-2">
            <label className="flex cursor-pointer items-center gap-2 text-sm">
              <input type="checkbox" checked={force} onChange={(e) => setForce(e.target.checked)} className="cursor-pointer" />
              强制注销(跳过交接,立即下线)
            </label>

            {force ? (
              <div className="grid gap-1.5 rounded-md border border-destructive/50 bg-destructive/5 p-3">
                <p className="text-sm text-destructive">此操作不生成交接文档,且不可撤销。</p>
                <label htmlFor="rt-confirm" className="text-sm font-medium">
                  输入数字人名称「{agent.name}」以确认
                </label>
                <Input id="rt-confirm" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
              </div>
            ) : (
              <>
                <div className="grid gap-1.5">
                  <label htmlFor="rt-successor" className="text-sm font-medium">接手人</label>
                  <select
                    id="rt-successor"
                    value={successor}
                    onChange={(e) => setSuccessor(e.target.value)}
                    className="h-9 cursor-pointer rounded-md border border-input bg-transparent px-2 text-sm"
                  >
                    <option value="">稍后指定</option>
                    {agents
                      .filter((a) => a.id !== agent.id)
                      .map((a) => (
                        <option key={a.id} value={a.name}>
                          {a.name}
                        </option>
                      ))}
                  </select>
                </div>
                <div className="grid gap-1.5">
                  <label htmlFor="rt-reason" className="text-sm font-medium">注销原因</label>
                  <Textarea id="rt-reason" rows={2} value={reason} onChange={(e) => setReason(e.target.value)} />
                </div>
                <p className="text-xs text-muted-foreground">将生成交接文档并入库,数字人退出所有频道。</p>
              </>
            )}
          </div>
        )}

        <DialogFooter>
          <Button variant="outline" className="cursor-pointer" onClick={onClose}>
            {doc ? "关闭" : "取消"}
          </Button>
          {!doc && (
            <Button
              variant={force ? "destructive" : "default"}
              className="cursor-pointer"
              onClick={submit}
              disabled={force && confirm !== agent.name}
            >
              确认注销
            </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
