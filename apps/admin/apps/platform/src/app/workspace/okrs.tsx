import { useCallback, useEffect, useState } from "react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Textarea } from "@workspace/ui/components/textarea"
import { controlApi, type Okr } from "../../lib/control-api"

interface KR { title: string; target: number; progress: number; unit: string }

function parseKR(s: string): KR[] {
  try { return JSON.parse(s) as KR[] } catch { return [] }
}

export default function OkrsPage() {
  const [okrs, setOkrs] = useState<Okr[]>([])
  const [open, setOpen] = useState(false)
  const [form, setForm] = useState({ title: "", objective: "", period: "", krs: "" })

  const load = useCallback(() => {
    void controlApi.okrs().then((x) => setOkrs(x ?? [])).catch(() => setOkrs([]))
  }, [])
  useEffect(() => { load() }, [load])

  const create = async () => {
    if (!form.title.trim()) return
    const krs = form.krs.trim() ? JSON.parse(form.krs) : []
    await controlApi.createOkr({ title: form.title.trim(), objective: form.objective, period: form.period, keyResults: JSON.stringify(krs) })
    setOpen(false); setForm({ title: "", objective: "", period: "", krs: "" }); load()
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">OKR 管理</h1>
          <p className="text-sm text-muted-foreground">目标 + 关键结果,进度汇总。</p>
        </div>
        <Button onClick={() => setOpen(true)}>＋ 新建 OKR</Button>
      </div>

      <div className="space-y-2">
        {okrs.map((o) => {
          const krs = parseKR(o.keyResults)
          const overall = krs.length ? Math.round(krs.reduce((a, k) => a + k.progress, 0) / krs.length) : 0
          return (
            <Card key={o.id} className="p-3">
              <div className="flex items-center gap-2">
                <span className="font-medium">{o.title}</span>
                <Badge variant="secondary">{o.period}</Badge>
                <span className="ml-auto text-sm text-muted-foreground">{overall}%</span>
              </div>
              {o.objective && <p className="mt-1 text-sm text-muted-foreground">{o.objective}</p>}
              <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
                <div className="h-full rounded-full bg-primary" style={{ width: `${overall}%` }} />
              </div>
              <div className="mt-2 space-y-1">
                {krs.map((k, i) => (
                  <div key={i} className="flex items-center gap-2 text-xs text-muted-foreground">
                    <span className="w-40 truncate">{k.title}</span>
                    <span>{k.progress}/{k.target}{k.unit}</span>
                    <div className="h-1.5 w-32 overflow-hidden rounded-full bg-muted">
                      <div className="h-full bg-accent" style={{ width: `${Math.min(100, (k.progress / (k.target || 1)) * 100)}%` }} />
                    </div>
                  </div>
                ))}
              </div>
            </Card>
          )
        })}
        {okrs.length === 0 && <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">还没有 OKR,点「新建 OKR」。</p>}
      </div>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader><DialogTitle>新建 OKR</DialogTitle></DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">标题 *</label>
              <Input value={form.title} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如:Q3 增长" />
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">目标 Objective</label>
              <Textarea rows={2} value={form.objective} onChange={(e) => setForm({ ...form, objective: e.target.value })} />
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">周期</label>
              <Input value={form.period} onChange={(e) => setForm({ ...form, period: e.target.value })} placeholder="如:2026-Q3" />
            </div>
            <div className="grid gap-1.5">
              <label className="text-sm font-medium">关键结果(JSON)</label>
              <Textarea rows={3} value={form.krs} onChange={(e) => setForm({ ...form, krs: e.target.value })} placeholder='[{"title":"MAU","target":100,"progress":40,"unit":"万"}]' />
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
