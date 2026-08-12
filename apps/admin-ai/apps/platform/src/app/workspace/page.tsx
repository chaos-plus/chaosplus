/* eslint-disable react-hooks/set-state-in-effect */
import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate, useParams } from "react-router"
import {
  ChevronDown,
  ChevronRight,
  CirclePlay,
  FileText,
  Hash,
  LoaderCircle,
  Paperclip,
  Plus,
  RefreshCw,
  Trash2,
} from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Progress } from "@workspace/ui/components/progress"
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@workspace/ui/components/sheet"
import { Textarea } from "@workspace/ui/components/textarea"
import { toast } from "@workspace/ui/components/sonner"
import { controlApi, type Attachment, type WorkItem } from "../../lib/control-api"

/** 统一把失败暴露给用户 —— 静默失败会让人以为操作成功了。 */
function reportError(action: string, e: unknown) {
  toast.error(`${action}失败:${e instanceof Error ? e.message : String(e)}`)
}

const TYPE_LABEL: Record<string, string> = { requirement: "需求", task: "任务", test: "测试", bug: "缺陷" }
const STATUS_LABEL: Record<string, string> = { open: "待办", in_progress: "进行中", review: "评审中", done: "已完成" }
const STATUS_TONE: Record<string, string> = {
  open: "bg-muted text-muted-foreground",
  in_progress: "bg-primary/10 text-primary",
  review: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  done: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
}
const STATUS_OPTIONS = ["open", "in_progress", "review", "done"] as const
/** 路由段(复数)→ 存储类型(单数)。 */
const ROUTE_TYPE: Record<string, string> = { requirements: "requirement", tasks: "task", tests: "test", bugs: "bug" }

function StatusChip({ status }: { status: string }) {
  return (
    <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${STATUS_TONE[status] ?? STATUS_TONE.open}`}>
      {STATUS_LABEL[status] ?? status}
    </span>
  )
}

export default function WorkspacePage() {
  const { type: routeType = "tasks" } = useParams()
  const type = ROUTE_TYPE[routeType] ?? routeType
  const navigate = useNavigate()

  const [items, setItems] = useState<WorkItem[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [filterStatus, setFilterStatus] = useState("")
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [createFor, setCreateFor] = useState<{ open: boolean; parent: string }>({ open: false, parent: "" })
  const [form, setForm] = useState({ title: "", description: "", estimateHours: "" })
  const [detail, setDetail] = useState<WorkItem | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<WorkItem | null>(null)
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    try {
      const list = await controlApi.workItems(type, filterStatus || undefined)
      setItems(list ?? [])
      setLoadError(false)
    } catch {
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }, [type, filterStatus])

  useEffect(() => {
    setLoading(true)
    void load()
    const t = setInterval(() => void load(), 5000)
    return () => clearInterval(t)
  }, [load])

  const create = async () => {
    if (!form.title.trim()) return
    try {
      await controlApi.createWorkItem({
        type,
        title: form.title.trim(),
        description: form.description,
        status: "open",
        parentId: createFor.parent,
        estimateHours: Number(form.estimateHours) || 0,
      })
      setCreateFor({ open: false, parent: "" })
      setForm({ title: "", description: "", estimateHours: "" })
      void load()
    } catch (e) {
      reportError("创建", e)
    }
  }

  const setStatus = async (id: string, status: string) => {
    try {
      const current = items.find((item) => item.id === id)
      await controlApi.updateWorkItem(id, { type: current?.type, status })
      void load()
    } catch (e) {
      reportError("更新状态", e)
      void load() // 回滚到服务端真实状态
    }
  }

  const remove = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await controlApi.deleteWorkItem(deleteTarget.id, deleteTarget.type)
      if (detail?.id === deleteTarget.id) setDetail(null)
      setDeleteTarget(null)
      await load()
    } catch (e) {
      reportError("删除", e)
    } finally {
      setDeleting(false)
    }
  }

  const execute = async (id: string) => {
    try {
      const r = await controlApi.executeWorkItem(id)
      void load()
      navigate(`/workflow/runs/${r.runId}`)
    } catch (e) {
      reportError("执行", e)
    }
  }

  const childrenOf = (id: string) => items.filter((it) => it.parentId === id)
  const roots = items.filter((it) => !it.parentId || !items.some((p) => p.id === it.parentId))

  // seen 防环:parentId 由 API 可写,自引用或 A→B→A 会让递归爆栈。
  const renderRow = (it: WorkItem, depth: number, seen: ReadonlySet<string> = new Set()): React.ReactNode => {
    if (seen.has(it.id)) return null
    const nextSeen = new Set(seen).add(it.id)
    const kids = childrenOf(it.id).filter((c) => !nextSeen.has(c.id))
    const isCollapsed = collapsed[it.id]
    return (
      <div key={it.id}>
        <Card className="group p-3 transition-colors duration-200 hover:border-primary/40" style={{ marginLeft: depth * 20 }}>
          <div className="flex flex-wrap items-center gap-2">
            {kids.length > 0 ? (
              <button
                aria-label={isCollapsed ? "展开子任务" : "收起子任务"}
                className="grid size-11 shrink-0 cursor-pointer place-items-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                onClick={(e) => {
                  e.stopPropagation()
                  setCollapsed((c) => ({ ...c, [it.id]: !c[it.id] }))
                }}
              >
                {isCollapsed ? <ChevronRight className="size-4" /> : <ChevronDown className="size-4" />}
              </button>
            ) : (
              <span className="inline-block size-11 shrink-0" />
            )}

            <Badge variant={it.type === "bug" ? "destructive" : "secondary"}>{TYPE_LABEL[it.type] ?? it.type}</Badge>
            <button
              type="button"
              className="min-h-11 min-w-0 cursor-pointer rounded px-1 text-left font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              onClick={() => setDetail(it)}
            >
              {it.title}
            </button>
            <StatusChip status={it.status} />

            {it.progress > 0 && (
              <span className="flex items-center gap-1.5 text-xs text-muted-foreground">
                <Progress value={it.progress} className="h-1.5 w-20" />
                {it.progress}%
              </span>
            )}
            {(it.estimateHours > 0 || it.spentHours > 0) && (
              <span className="text-xs tabular-nums text-muted-foreground">
                {it.estimateHours > 0 && `估 ${it.estimateHours.toFixed(1)}h`}
                {it.estimateHours > 0 && it.spentHours > 0 && " · "}
                {it.spentHours > 0 && `实 ${it.spentHours.toFixed(2)}h`}
              </span>
            )}

            <div className="ml-auto flex items-center gap-1.5" onClick={(e) => e.stopPropagation()}>
              <select
                value={it.status}
                onChange={(e) => setStatus(it.id, e.target.value)}
                className="min-h-11 cursor-pointer rounded-md border border-input bg-transparent px-2 text-xs transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                aria-label={`${it.title} 状态`}
              >
                {STATUS_OPTIONS.map((s) => (
                  <option key={s} value={s}>
                    {STATUS_LABEL[s]}
                  </option>
                ))}
              </select>
              <Button
                size="sm"
                variant="outline"
                className="min-h-11 cursor-pointer gap-1"
                disabled={it.status === "in_progress"}
                onClick={() => execute(it.id)}
                title="按工作流执行"
              >
                <CirclePlay className="size-3.5" />
                {it.status === "in_progress" ? "执行中" : "执行"}
              </Button>
              <Button
                size="sm"
                variant="ghost"
                className="min-h-11 cursor-pointer gap-1"
                onClick={() => {
                  setCreateFor({ open: true, parent: it.id })
                  setForm({ title: "", description: "", estimateHours: "" })
                }}
                aria-label={`为 ${it.title} 新建子任务`}
              >
                <Plus className="size-3.5" />
                子任务
              </Button>
              <Button
                size="icon"
                variant="ghost"
                className="size-11 cursor-pointer text-muted-foreground hover:text-destructive"
                aria-label={`删除 ${it.title}`}
                onClick={() => setDeleteTarget(it)}
              >
                <Trash2 className="size-3.5" />
              </Button>
            </div>
          </div>
          {it.description && <p className="mt-1.5 line-clamp-2 pl-7 text-sm text-muted-foreground">{it.description}</p>}
        </Card>
        {!isCollapsed && kids.map((c) => renderRow(c, depth + 1, nextSeen))}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">{TYPE_LABEL[type] ?? type}管理</h1>
          <p className="text-sm text-muted-foreground">执行走工作流;进度、工时自动计算,变化订阅到关联群聊。</p>
        </div>
        <Button
          className="cursor-pointer gap-1.5"
          onClick={() => {
            setCreateFor({ open: true, parent: "" })
            setForm({ title: "", description: "", estimateHours: "" })
          }}
        >
          <Plus className="size-4" />
          新建{TYPE_LABEL[type] ?? type}
        </Button>
      </div>

      <div className="flex items-center gap-2 text-sm">
        <select
          value={filterStatus}
          onChange={(e) => setFilterStatus(e.target.value)}
          className="h-8 cursor-pointer rounded-md border border-input bg-transparent px-2 transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
          aria-label="按状态筛选"
        >
          <option value="">全部状态</option>
          {STATUS_OPTIONS.map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <span className="text-muted-foreground">{items.length} 项</span>
      </div>

      <div className="space-y-2">
        {loading && (
          <div className="flex min-h-24 items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground" role="status">
            <LoaderCircle className="size-4 animate-spin" />
            加载工作项…
          </div>
        )}
        {!loading && loadError && (
          <div className="flex min-h-24 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground" role="alert">
            <span>工作项加载失败,请检查控制面连接。</span>
            <Button variant="outline" className="min-h-11 gap-2" onClick={() => void load()}>
              <RefreshCw className="size-4" />
              重试
            </Button>
          </div>
        )}
        {!loading && !loadError && roots.map((it) => renderRow(it, 0))}
        {!loading && !loadError && roots.length === 0 && (
          <div className="rounded-xl border border-dashed p-8 text-center">
            <FileText className="mx-auto size-6 text-muted-foreground" />
            <p className="mt-2 text-sm text-muted-foreground">
              还没有{TYPE_LABEL[type] ?? type},点「新建」创建,或到会话区把消息转为工作项。
            </p>
          </div>
        )}
      </div>

      <DetailSheet
        item={detail}
        onClose={() => setDetail(null)}
        onChanged={load}
        onExecute={execute}
        onAddSubtask={(parentId) => {
          setDetail(null)
          setCreateFor({ open: true, parent: parentId })
          setForm({ title: "", description: "", estimateHours: "" })
        }}
      />

      <Dialog
        open={createFor.open}
        onOpenChange={(v) => {
          if (!v) setCreateFor({ open: false, parent: "" })
        }}
      >
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{createFor.parent ? "新建子任务" : `新建${TYPE_LABEL[type] ?? type}`}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="wi-title" className="text-sm font-medium">
                标题 <span className="text-destructive">*</span>
              </label>
              <Input
                id="wi-title"
                autoFocus
                value={form.title}
                onChange={(e) => setForm({ ...form, title: e.target.value })}
                placeholder="如:实现登录接口"
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="wi-desc" className="text-sm font-medium">
                描述
              </label>
              <Textarea
                id="wi-desc"
                rows={3}
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
                placeholder="验收标准、上下文、相关文件…"
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="wi-est" className="text-sm font-medium">
                估时(小时)
              </label>
              <Input
                id="wi-est"
                type="number"
                min="0"
                step="0.5"
                value={form.estimateHours}
                onChange={(e) => setForm({ ...form, estimateHours: e.target.value })}
                placeholder="留空则按首次实际耗时自动校准"
              />
            </div>
            {createFor.parent && <p className="text-xs text-muted-foreground">父级:{createFor.parent}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" className="cursor-pointer" onClick={() => setCreateFor({ open: false, parent: "" })}>
              取消
            </Button>
            <Button className="cursor-pointer" onClick={create} disabled={!form.title.trim()}>
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && !deleting && setDeleteTarget(null)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>删除工作项</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            将删除「{deleteTarget?.title}」。已有子任务不会自动删除,但会变为顶层工作项。
          </p>
          <DialogFooter>
            <Button variant="outline" className="min-h-11" disabled={deleting} onClick={() => setDeleteTarget(null)}>
              取消
            </Button>
            <Button variant="destructive" className="min-h-11 gap-2" disabled={deleting} onClick={() => void remove()}>
              {deleting && <LoaderCircle className="size-4 animate-spin" />}
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function DetailSheet({
  item,
  onClose,
  onChanged,
  onExecute,
  onAddSubtask,
}: {
  item: WorkItem | null
  onClose: () => void
  onChanged: () => void | Promise<void>
  onExecute: (id: string) => void
  onAddSubtask: (parentId: string) => void
}) {
  const [atts, setAtts] = useState<Attachment[]>([])
  const [desc, setDesc] = useState("")
  const [estimate, setEstimate] = useState("")
  const [uploading, setUploading] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  useEffect(() => {
    if (!item) return
    setDesc(item.description)
    setEstimate(item.estimateHours ? String(item.estimateHours) : "")
    void controlApi
      .attachments(item.type, item.id)
      .then((x) => setAtts(x ?? []))
      .catch(() => setAtts([]))
  }, [item])

  if (!item) return null

  const save = async () => {
    try {
      await controlApi.updateWorkItem(item.id, {
        type: item.type,
        title: item.title,
        description: desc,
        status: item.status,
        channelId: item.channelId,
        parentId: item.parentId,
        estimateHours: Number(estimate) || 0,
      })
      await onChanged()
      onClose()
    } catch (e) {
      reportError("保存", e)
    }
  }

  const upload = async (files: FileList | null) => {
    if (!files?.length) return
    setUploading(true)
    try {
      for (const f of Array.from(files)) await controlApi.uploadAttachment(item.type, item.id, f)
    } catch (e) {
      reportError("上传附件", e)
    } finally {
      // 无论成功与否都刷新:部分成功的文件也要显示出来。
      setAtts((await controlApi.attachments(item.type, item.id).catch(() => [])) ?? [])
      setUploading(false)
      if (fileRef.current) fileRef.current.value = ""
    }
  }

  return (
    <Sheet open onOpenChange={(v) => !v && onClose()}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2 pr-6">
            <Badge variant={item.type === "bug" ? "destructive" : "secondary"}>{TYPE_LABEL[item.type] ?? item.type}</Badge>
            <span className="truncate">{item.title}</span>
          </SheetTitle>
        </SheetHeader>

        <div className="space-y-5 px-4 pb-6">
          <div className="flex flex-wrap items-center gap-2 text-sm">
            <StatusChip status={item.status} />
            <span className="flex items-center gap-1 text-xs text-muted-foreground">
              <Hash className="size-3" />
              {item.id}
            </span>
          </div>

          <div className="grid gap-1.5">
            <div className="flex items-center justify-between text-sm">
              <span className="font-medium">进度</span>
              <span className="tabular-nums text-muted-foreground">{item.progress}%</span>
            </div>
            <Progress value={item.progress} className="h-2" />
            <p className="text-xs text-muted-foreground">
              估 {item.estimateHours.toFixed(1)}h · 实 {item.spentHours.toFixed(2)}h
              {item.estimateHours > 0 && item.spentHours > 0 && (
                <> · 偏差 {(((item.spentHours - item.estimateHours) / item.estimateHours) * 100).toFixed(0)}%</>
              )}
            </p>
          </div>

          <div className="grid gap-1.5">
            <label htmlFor="d-desc" className="text-sm font-medium">
              描述
            </label>
            <Textarea id="d-desc" rows={5} value={desc} onChange={(e) => setDesc(e.target.value)} />
          </div>

          <div className="grid gap-1.5">
            <label htmlFor="d-est" className="text-sm font-medium">
              估时(小时)
            </label>
            <Input id="d-est" type="number" min="0" step="0.5" value={estimate} onChange={(e) => setEstimate(e.target.value)} />
          </div>

          <div className="grid gap-2">
            <div className="flex items-center justify-between">
              <span className="text-sm font-medium">附件</span>
              <Button
                size="sm"
                variant="outline"
                className="cursor-pointer gap-1.5"
                disabled={uploading}
                onClick={() => fileRef.current?.click()}
              >
                <Paperclip className="size-3.5" />
                {uploading ? "上传中…" : "上传文件"}
              </Button>
              <input
                ref={fileRef}
                type="file"
                multiple
                className="hidden"
                onChange={(e) => upload(e.target.files)}
                aria-label="选择要上传的文件"
              />
            </div>
            {atts.length === 0 && (
              <p className="text-xs text-muted-foreground">还没有附件。图片、视频、文档都可上传,聊天中也能引用。</p>
            )}
            <div className="grid grid-cols-2 gap-2">
              {atts.map((a) => {
                const url = `/api/attachments/${a.id}/content`
                const isImage = a.mime.startsWith("image/")
                const isVideo = a.mime.startsWith("video/")
                return (
                  <a
                    key={a.id}
                    href={url}
                    target="_blank"
                    rel="noreferrer"
                    className="cursor-pointer overflow-hidden rounded-lg border transition-colors hover:border-primary/40 hover:bg-accent/40 focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                  >
                    {isImage ? (
                      <img src={url} alt={a.filename} loading="lazy" className="h-24 w-full object-cover" />
                    ) : isVideo ? (
                      <video src={url} className="h-24 w-full object-cover" muted preload="metadata" />
                    ) : (
                      <div className="flex h-24 items-center justify-center bg-muted">
                        <FileText className="size-6 text-muted-foreground" />
                      </div>
                    )}
                    <div className="px-2 py-1.5">
                      <p className="truncate text-xs font-medium">{a.filename}</p>
                      <p className="text-[11px] text-muted-foreground">{(a.sizeBytes / 1024).toFixed(1)} KB</p>
                    </div>
                  </a>
                )
              })}
            </div>
          </div>

          <div className="flex flex-wrap gap-2 border-t pt-4">
            <Button className="cursor-pointer" onClick={save}>
              保存
            </Button>
            <Button
              variant="outline"
              className="cursor-pointer gap-1.5"
              disabled={item.status === "in_progress"}
              onClick={() => onExecute(item.id)}
            >
              <CirclePlay className="size-4" />
              执行工作流
            </Button>
            <Button variant="outline" className="cursor-pointer gap-1.5" onClick={() => onAddSubtask(item.id)}>
              <Plus className="size-4" />
              新建子任务
            </Button>
            <Button variant="ghost" className="cursor-pointer" onClick={onClose}>
              关闭
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  )
}
