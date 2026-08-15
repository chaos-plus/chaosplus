import { useCallback, useEffect, useState } from "react";
import { LoaderCircle, Pencil, Plus, RefreshCw, Trash2 } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Card } from "@workspace/ui/components/card";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { toast } from "@workspace/ui/components/sonner";
import {
  appendUploadedImages,
  controlApi,
  type Attachment,
  type Okr,
} from "../../lib/control-api";
import {
  AttachmentQueue,
  MarkdownView,
  RichContentEditor,
} from "./rich-content";

interface KR {
  title: string;
  target: number;
  progress: number;
  unit: string;
}

interface KRRow {
  id?: string;
  title: string;
  currentValue: string;
  targetValue: string;
  unit: string;
}

const emptyKR = (): KRRow => ({
  title: "",
  currentValue: "0",
  targetValue: "100",
  unit: "%",
});

function parseKR(s: string): KR[] {
  return s
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [title, progress = "0", target = "100", unit = "%"] =
        line.split("|");
      return {
        title,
        progress: Number(progress),
        target: Number(target),
        unit,
      };
    })
    .filter(
      (item) =>
        item.title &&
        Number.isFinite(item.progress) &&
        Number.isFinite(item.target),
    );
}

export default function OkrsPage() {
  const [okrs, setOkrs] = useState<Okr[]>([]);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Okr | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Okr | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [saving, setSaving] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [files, setFiles] = useState<File[]>([]);
  const [form, setForm] = useState({
    title: "",
    objective: "",
    periodStart: "",
    periodEnd: "",
    keyResults: [emptyKR()],
  });

  const load = useCallback(async () => {
    try {
      setOkrs((await controlApi.okrs()) ?? []);
      setLoadError(false);
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  const [krError, setKrError] = useState("");

  const create = async () => {
    if (!form.title.trim()) return;
    if (
      !form.periodStart ||
      !form.periodEnd ||
      form.periodEnd < form.periodStart
    ) {
      setKrError("请选择有效的开始和结束日期");
      return;
    }
    if (
      form.keyResults.some(
        (result) =>
          !result.title.trim() ||
          !result.currentValue ||
          !result.targetValue ||
          !Number.isFinite(Number(result.currentValue)) ||
          !Number.isFinite(Number(result.targetValue)) ||
          Number(result.currentValue) < 0 ||
          Number(result.targetValue) < 0,
      )
    ) {
      setKrError("请填写有效的关键结果标题和非负数值");
      return;
    }
    setSaving(true);
    try {
      const payload = {
        title: form.title.trim(),
        objective: form.objective,
        period: `${form.periodStart} / ${form.periodEnd}`,
        keyResultRows: form.keyResults.map((result) => ({
          ...result,
          title: result.title.trim(),
          unit: result.unit.trim(),
        })),
      };
      let saved: Okr;
      if (editing)
        saved = await controlApi.updateOkr(editing.id, {
          ...payload,
          version: editing.version,
        });
      else saved = await controlApi.createOkr(payload);
      const uploaded: Attachment[] = [];
      let uploadError: unknown;
      for (const file of files) {
        try {
          uploaded.push(
            await controlApi.uploadAttachment("objective", saved.id, file),
          );
        } catch (error) {
          uploadError ??= error;
        }
      }
      const objective = appendUploadedImages(
        payload.objective,
        uploaded,
        controlApi.attachmentContentUrl,
      );
      if (objective !== payload.objective)
        saved = await controlApi.updateOkr(saved.id, {
          ...payload,
          objective,
          version: saved.version,
        });
      setOpen(false);
      setEditing(null);
      setFiles([]);
      setForm({
        title: "",
        objective: "",
        periodStart: "",
        periodEnd: "",
        keyResults: [emptyKR()],
      });
      setKrError("");
      await load();
      if (uploadError)
        toast.error(
          `部分 OKR 附件上传失败:${uploadError instanceof Error ? uploadError.message : String(uploadError)}`,
        );
    } catch (e) {
      setKrError(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const beginCreate = () => {
    setEditing(null);
    setFiles([]);
    setForm({
      title: "",
      objective: "",
      periodStart: "",
      periodEnd: "",
      keyResults: [emptyKR()],
    });
    setKrError("");
    setOpen(true);
  };

  const beginEdit = (okr: Okr) => {
    setEditing(okr);
    setFiles([]);
    setForm({
      title: okr.title,
      objective: okr.objective,
      periodStart: new Date(okr.periodStart).toISOString().slice(0, 10),
      periodEnd: new Date(okr.periodEnd).toISOString().slice(0, 10),
      keyResults: okr.keyResultRows,
    });
    setKrError("");
    setOpen(true);
  };

  const remove = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await controlApi.deleteOkr(deleteTarget.id, deleteTarget.version);
      setDeleteTarget(null);
      await load();
    } catch (e) {
      toast.error(
        `删除 OKR 失败:${e instanceof Error ? e.message : String(e)}`,
      );
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">OKR 管理</h1>
          <p className="text-sm text-muted-foreground">
            目标 + 关键结果,进度汇总。
          </p>
        </div>
        <Button className="min-h-11 gap-2" onClick={beginCreate}>
          <Plus className="size-4" />
          新建 OKR
        </Button>
      </div>

      <div className="space-y-2">
        {loading && (
          <div
            className="flex min-h-24 items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground"
            role="status"
          >
            <LoaderCircle className="size-4 animate-spin" />
            加载 OKR…
          </div>
        )}
        {!loading && loadError && (
          <div
            className="flex min-h-24 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground"
            role="alert"
          >
            <span>OKR 加载失败,请检查控制面连接。</span>
            <Button
              variant="outline"
              className="min-h-11 gap-2"
              onClick={() => void load()}
            >
              <RefreshCw className="size-4" />
              重试
            </Button>
          </div>
        )}
        {!loading &&
          !loadError &&
          okrs.map((o) => {
            const krs = parseKR(o.keyResults);
            const overall = krs.length
              ? Math.round(
                  krs.reduce(
                    (sum, kr) =>
                      sum +
                      Math.min(100, (kr.progress / (kr.target || 1)) * 100),
                    0,
                  ) / krs.length,
                )
              : 0;
            return (
              <Card key={o.id} className="p-3">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="min-w-0 flex-1 basis-40 break-words font-medium">
                    {o.title}
                  </span>
                  <Badge
                    variant="secondary"
                    className="shrink-0 whitespace-nowrap"
                  >
                    {o.period}
                  </Badge>
                  <span className="ml-auto text-sm tabular-nums text-muted-foreground">
                    {overall}%
                  </span>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-11"
                    aria-label={`编辑 ${o.title}`}
                    onClick={() => beginEdit(o)}
                  >
                    <Pencil className="size-4" />
                  </Button>
                  <Button
                    size="icon"
                    variant="ghost"
                    className="size-11 text-muted-foreground hover:text-destructive"
                    aria-label={`删除 ${o.title}`}
                    onClick={() => setDeleteTarget(o)}
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </div>
                {o.objective && (
                  <div className="mt-1 text-muted-foreground">
                    <MarkdownView value={o.objective} />
                  </div>
                )}
                <div className="mt-2 h-2 overflow-hidden rounded-full bg-muted">
                  <div
                    className="h-full rounded-full bg-primary"
                    style={{ width: `${overall}%` }}
                  />
                </div>
                <div className="mt-2 space-y-1">
                  {krs.map((k, i) => (
                    <div
                      key={i}
                      className="flex items-center gap-2 text-xs text-muted-foreground"
                    >
                      <span className="min-w-0 flex-1" title={k.title}>
                        {k.title}
                      </span>
                      <span className="shrink-0 tabular-nums">
                        {k.progress}/{k.target}
                        {k.unit}
                      </span>
                      <div className="h-1.5 w-24 shrink-0 overflow-hidden rounded-full bg-muted sm:w-32">
                        <div
                          className="h-full bg-accent"
                          style={{
                            width: `${Math.min(100, (k.progress / (k.target || 1)) * 100)}%`,
                          }}
                        />
                      </div>
                    </div>
                  ))}
                </div>
              </Card>
            );
          })}
        {!loading && !loadError && okrs.length === 0 && (
          <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">
            还没有 OKR,点「新建 OKR」。
          </p>
        )}
      </div>

      <Dialog
        open={open}
        onOpenChange={(value) => {
          if (!saving) setOpen(value);
        }}
      >
        <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑 OKR" : "新建 OKR"}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="okr-title" className="text-sm font-medium">
                标题 *
              </label>
              <Input
                id="okr-title"
                value={form.title}
                onChange={(e) => setForm({ ...form, title: e.target.value })}
                placeholder="如:Q3 增长"
              />
            </div>
            <RichContentEditor
              id="okr-objective"
              label="目标 Objective"
              rows={5}
              value={form.objective}
              onChange={(objective) => setForm({ ...form, objective })}
            />
            <div className="grid grid-cols-2 gap-3">
              <div className="grid gap-1.5">
                <label
                  htmlFor="okr-period-start"
                  className="text-sm font-medium"
                >
                  开始日期
                </label>
                <Input
                  id="okr-period-start"
                  type="date"
                  value={form.periodStart}
                  onChange={(e) =>
                    setForm({ ...form, periodStart: e.target.value })
                  }
                />
              </div>
              <div className="grid gap-1.5">
                <label htmlFor="okr-period-end" className="text-sm font-medium">
                  结束日期
                </label>
                <Input
                  id="okr-period-end"
                  type="date"
                  min={form.periodStart}
                  value={form.periodEnd}
                  onChange={(e) =>
                    setForm({ ...form, periodEnd: e.target.value })
                  }
                />
              </div>
            </div>
            <div className="grid gap-2">
              <div className="flex items-center justify-between gap-2">
                <span className="text-sm font-medium">关键结果</span>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  className="min-h-11 gap-1"
                  onClick={() =>
                    setForm({
                      ...form,
                      keyResults: [...form.keyResults, emptyKR()],
                    })
                  }
                >
                  <Plus className="size-4" />
                  添加
                </Button>
              </div>
              {form.keyResults.map((result, index) => (
                <div
                  key={result.id ?? index}
                  className="grid gap-2 rounded-md border p-2 sm:grid-cols-[minmax(0,1fr)_6rem_6rem_4rem_2.75rem]"
                >
                  <Input
                    id={`okr-kr-title-${index}`}
                    aria-label={`关键结果 ${index + 1} 标题`}
                    value={result.title}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        keyResults: form.keyResults.map((value, position) =>
                          position === index
                            ? { ...value, title: event.target.value }
                            : value,
                        ),
                      })
                    }
                    placeholder="提升覆盖率"
                  />
                  <Input
                    aria-label={`关键结果 ${index + 1} 当前值`}
                    type="number"
                    min="0"
                    step="any"
                    value={result.currentValue}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        keyResults: form.keyResults.map((value, position) =>
                          position === index
                            ? { ...value, currentValue: event.target.value }
                            : value,
                        ),
                      })
                    }
                  />
                  <Input
                    aria-label={`关键结果 ${index + 1} 目标值`}
                    type="number"
                    min="0"
                    step="any"
                    value={result.targetValue}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        keyResults: form.keyResults.map((value, position) =>
                          position === index
                            ? { ...value, targetValue: event.target.value }
                            : value,
                        ),
                      })
                    }
                  />
                  <Input
                    aria-label={`关键结果 ${index + 1} 单位`}
                    value={result.unit}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        keyResults: form.keyResults.map((value, position) =>
                          position === index
                            ? { ...value, unit: event.target.value }
                            : value,
                        ),
                      })
                    }
                  />
                  <Button
                    type="button"
                    size="icon"
                    variant="ghost"
                    className="size-11 text-muted-foreground hover:text-destructive"
                    aria-label={`删除关键结果 ${index + 1}`}
                    onClick={() =>
                      setForm({
                        ...form,
                        keyResults: form.keyResults.filter(
                          (_, position) => position !== index,
                        ),
                      })
                    }
                  >
                    <Trash2 className="size-4" />
                  </Button>
                </div>
              ))}
            </div>
            <AttachmentQueue
              id="okr-attachments"
              files={files}
              onChange={setFiles}
              disabled={saving}
            />
            {krError && (
              <p
                id="okr-krs-error"
                className="text-sm text-destructive"
                role="alert"
              >
                {krError}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              className="min-h-11"
              disabled={saving}
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button
              className="min-h-11 gap-2"
              onClick={create}
              disabled={saving || !form.title.trim()}
            >
              {saving && <LoaderCircle className="size-4 animate-spin" />}
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={deleteTarget !== null}
        onOpenChange={(value) => !value && !deleting && setDeleteTarget(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>删除 OKR</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            将永久删除「{deleteTarget?.title}」及其关键结果。
          </p>
          <DialogFooter>
            <Button
              variant="outline"
              className="min-h-11"
              disabled={deleting}
              onClick={() => setDeleteTarget(null)}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              className="min-h-11 gap-2"
              disabled={deleting}
              onClick={() => void remove()}
            >
              {deleting && <LoaderCircle className="size-4 animate-spin" />}
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
