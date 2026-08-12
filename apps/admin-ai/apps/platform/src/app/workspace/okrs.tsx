import { useCallback, useEffect, useState } from "react"
import { LoaderCircle, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { toast } from "@workspace/ui/components/sonner"
import { controlApi, type Okr } from "../../lib/control-api"

interface KR { title: string; target: number; progress: number; unit: string }

function parseKR(s: string): KR[] {
  try { return JSON.parse(s) as KR[] } catch { return [] }
}

export default function OkrsPage() {
  const [okrs, setOkrs] = useState<Okr[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Okr | null>(null)
  const [deleteTarget, setDeleteTarget] = useState<Okr | null>(null)
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [form, setForm] = useState({ title: "", objective: "", period: "", krs: "" })

  const load = useCallback(async () => {
    try {
      setOkrs((await controlApi.okrs()) ?? [])
      setLoadError(false)
    } catch {
      setLoadError(true)
    } finally {
      setLoading(false)
    }
  }, [])
  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0)
    return () => window.clearTimeout(timer)
  }, [load])

  const [krError, setKrError] = useState("")

  const create = async () => {
    if (!form.title.trim()) return
    let krs: unknown = []
    if (form.krs.trim()) {
      try {
        krs = JSON.parse(form.krs)
      } catch {
        setKrError("关键结果不是合法 JSON,示例:[{\"title\":\"MAU\",\"target\":100,\"progress\":40,\"unit\":\"万\"}]")
        return
      }
    }
    setSaving(true)
    try {
      const payload = {
        title: form.title.trim(),
        objective: form.objective,
        period: form.period,
        keyResults: JSON.stringify(krs),
      }
      if (editing) await controlApi.updateOkr(editing.id, payload)
      else await controlApi.createOkr(payload)
      setOpen(false)
      setEditing(null)
      setForm({ title: "", objective: "", period: "", krs: "" })
      setKrError("")
      await load()
    } catch (e) {
      setKrError(e instanceof Error ? e.message : "保存失败")
    } finally {
      setSaving(false)
    }
  }

  const beginCreate = () => {
    setEditing(null)
    setForm({ title: "", objective: "", period: "", krs: "" })
    setKrError("")
    setOpen(true)
  }

  const beginEdit = (okr: Okr) => {
    setEditing(okr)
    setForm({ title: okr.title, objective: okr.objective, period: okr.period, krs: okr.keyResults })
    setKrError("")
    setOpen(true)
  }

  const remove = async () => {
    if (!deleteTarget) return
    setDeleting(true)
    try {
      await controlApi.deleteOkr(deleteTarget.id)
      setDeleteTarget(null)
      await load()
    } catch (e) {
      toast.error(`删除 OKR 失败:${e instanceof Error ? e.message : String(e)}`)
    } finally {
      setDeleting(false)
    }
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">OKR 管理</h1>
          <p className="text-sm text-muted-foreground">目标 + 关键结果,进度汇总。</p>
        </div>
        <Button className="min-h-11 gap-2" onClick={beginCreate}>
          <Plus className="size-4" />
          新建 OKR
        </Button>
      </div>

      <div className="space-y-2">
        {loading && (
          <div className="flex min-h-24 items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground" role="status">
            <LoaderCircle className="size-4 animate-spin" />
            加载 OKR…
          </div>
        )}
        {!loading && loadError && (
          <div className="flex min-h-24 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground" role="alert">
            <span>OKR 加载失败,请检查控制面连接。</span>
            <Button variant="outline" className="min-h-11 gap-2" onClick={() => void load()}>
              <RefreshCw className="size-4" />
              重试
            </Button>
          </div>
        )}
        {!loading && !loadError && okrs.map((o) => {
          const krs = parseKR(o.keyResults)
          const overall = krs.length
            ? Math.round(krs.reduce((sum, kr) => sum + Math.min(100, (kr.progress / (kr.target || 1)) * 100), 0) / krs.length)
            : 0
          return (
            <Card key={o.id} className="p-3">
              <div className="flex items-center gap-2">
                <span className="font-medium">{o.title}</span>
                <Badge variant="secondary">{o.period}</Badge>
                <span className="ml-auto text-sm tabular-nums text-muted-foreground">{overall}%</span>
                <Button size="icon" variant="ghost" className="size-11" aria-label={`编辑 ${o.title}`} onClick={() => beginEdit(o)}>
                  <Pencil className="size-4" />
                </Button>
                <Button size="icon" variant="ghost" className="size-11 text-muted-foreground hover:text-destructive" aria-label={`删除 ${o.title}`} onClick={() => setDeleteTarget(o)}>
                  <Trash2 className="size-4" />
                </Button>
              </div>
              {o.objective && <p className="mt-1 text-sm text-muted-foreground">{o.objective}</p>}
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-primary" style={{ width: `${overall}%` }} />
              </div>
              <div className="mt-2 space-y-1">
                {krs.map((k, i) => (
                  <div key={i} className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="min-w-0 flex-1" title={k.title}>{k.title}</span>
                    <span className="shrink-0 tabular-nums">{k.progress}/{k.target}{k.unit}</span>
                    <div className="h-1.5 w-24 shrink-0 overflow-hidden rounded-full bg-muted sm:w-32">
                      <div className="h-full bg-accent" style={{ width: `${Math.min(100, (k.progress / (k.target || 1)) * 100)}%` }} />
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          )
        })}
        {!loading && !loadError && okrs.length === 0 && <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">还没有 OKR,点「新建 OKR」。</p>}
      </div>

      <Dialog open={open} onOpenChange={(value) => { if (!saving) setOpen(value) }}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>{editing ? "编辑 OKR" : "新建 OKR"}</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="okr-title" className="text-sm font-medium">标题 *</label>
              <Input id="okr-title" value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如:Q3 增长" />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="okr-objective" className="text-sm font-medium">目标 Objective</label>
              <Textarea id="okr-objective" rows={2} value={form.objective} onChange={(e) => setForm({ ...form, objective: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="okr-period" className="text-sm font-medium">周期</label>
              <Input id="okr-period" value={form.period} onChange={(e) => setForm({ ...form, period: e.target.value })} placeholder="如:2026-Q3" />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="okr-krs" className="text-sm font-medium">关键结果(JSON)</label>
              <Textarea id="okr-krs" aria-describedby={krError ? "okr-krs-error" : undefined} aria-invalid={Boolean(krError)} rows={3} value={form.krs} onChange={(e) => setForm({ ...form, krs: e.target.value })} placeholder='[{"title":"MAU","target":100,"progress":40,"unit":"万"}]' />
            </div>
            {krError && <p id="okr-krs-error" className="text-sm text-destructive" role="alert">{krError}</p>}
          </div>
          <DialogFooter>
            <Button variant="outline" className="min-h-11" disabled={saving} onClick={() => setOpen(false)}>取消</Button>
            <Button className="min-h-11 gap-2" onClick={create} disabled={saving || !form.title.trim()}>
              {saving && <LoaderCircle className="size-4 animate-spin" />}
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={deleteTarget !== null} onOpenChange={(value) => !value && !deleting && setDeleteTarget(null)}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader><DialogTitle>删除 OKR</DialogTitle></DialogHeader>
          <p className="text-sm text-muted-foreground">将永久删除「{deleteTarget?.title}」及其关键结果。</p>
          <DialogFooter>
            <Button variant="outline" className="min-h-11" disabled={deleting} onClick={() => setDeleteTarget(null)}>取消</Button>
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
