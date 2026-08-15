/* eslint-disable react-hooks/set-state-in-effect */
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams, useSearchParams } from "react-router";
import {
  Bug,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  CirclePlay,
  FileText,
  Hash,
  LoaderCircle,
  Paperclip,
  Pencil,
  Plus,
  RefreshCw,
  TestTube2,
  Trash2,
  XCircle,
} from "lucide-react";
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
import { Progress } from "@workspace/ui/components/progress";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@workspace/ui/components/sheet";
import { Textarea } from "@workspace/ui/components/textarea";
import { toast } from "@workspace/ui/components/sonner";
import {
  appendUploadedImages,
  controlApi,
  type Attachment,
  type Defect,
  type TestCase,
  type TestRun,
  type WorkItem,
} from "../../lib/control-api";
import { iamApi, type Member } from "../../lib/iam-api";
import {
  AttachmentQueue,
  MarkdownView,
  ProtectedAttachmentMedia,
  RichContentEditor,
} from "./rich-content";

/** 统一把失败暴露给用户 —— 静默失败会让人以为操作成功了。 */
function reportError(action: string, e: unknown) {
  toast.error(`${action}失败:${e instanceof Error ? e.message : String(e)}`);
}

const TYPE_LABEL: Record<string, string> = {
  requirement: "需求",
  task: "任务",
  test: "测试",
  bug: "缺陷",
};
const STATUS_LABEL: Record<string, string> = {
  draft: "草稿",
  approved: "已批准",
  rejected: "已拒绝",
  closed: "已关闭",
  open: "待办",
  in_progress: "进行中",
  review: "评审中",
  done: "已完成",
  cancelled: "已取消",
};
const STATUS_TONE: Record<string, string> = {
  open: "bg-muted text-muted-foreground",
  in_progress: "bg-primary/10 text-primary",
  review: "bg-amber-500/15 text-amber-700 dark:text-amber-400",
  done: "bg-emerald-500/15 text-emerald-700 dark:text-emerald-400",
};
const WORK_ITEM_STATUSES: Record<string, string[]> = {
  requirement: ["draft", "approved", "rejected", "closed"],
  task: ["open", "in_progress", "review", "done", "cancelled"],
};
/** 路由段(复数)→ 存储类型(单数)。 */
const ROUTE_TYPE: Record<string, string> = {
  requirements: "requirement",
  tasks: "task",
  tests: "test",
  bugs: "bug",
};

const PRIORITY_LABEL: Record<string, string> = {
  highest: "最高",
  high: "高",
  medium: "中",
  low: "低",
  lowest: "最低",
};

function workItemForm(parent?: WorkItem) {
  return {
    title: "",
    description: "",
    acceptanceCriteria: "",
    estimateHours: "",
    requirementId: parent?.requirementId ?? "",
    keyResultIds: [] as string[],
    priority: parent?.priority || "medium",
    dueDate: "",
    assigneeId: parent?.assigneeId ?? "",
  };
}

function dateInputValue(value: number) {
  if (!value) return "";
  const date = new Date(value);
  return new Date(value - date.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 10);
}

function dueDateValue(value: string) {
  return value ? new Date(`${value}T23:59:59.999`).getTime() : 0;
}

function StatusChip({ status }: { status: string }) {
  return (
    <span
      className={`rounded-full px-2 py-0.5 text-xs font-medium ${STATUS_TONE[status] ?? STATUS_TONE.open}`}
    >
      {STATUS_LABEL[status] ?? status}
    </span>
  );
}

export default function WorkspacePage() {
  const { type: routeType = "tasks" } = useParams();
  if (routeType === "tests") return <TestsPage />;
  if (routeType === "bugs") return <DefectsPage />;
  return <WorkItemsPage routeType={routeType} />;
}

function WorkItemsPage({ routeType }: { routeType: string }) {
  const type = ROUTE_TYPE[routeType] ?? routeType;
  const navigate = useNavigate();

  const [items, setItems] = useState<WorkItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [filterStatus, setFilterStatus] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [createFor, setCreateFor] = useState<{ open: boolean; parent: string }>(
    { open: false, parent: "" },
  );
  const [form, setForm] = useState(workItemForm());
  const [files, setFiles] = useState<File[]>([]);
  const [creating, setCreating] = useState(false);
  const [detail, setDetail] = useState<WorkItem | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<WorkItem | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [requirements, setRequirements] = useState<WorkItem[]>([]);
  const [keyResults, setKeyResults] = useState<
    Array<{ id: string; title: string }>
  >([]);
  const [members, setMembers] = useState<Member[]>([]);

  const load = useCallback(async () => {
    try {
      const list = await controlApi.workItems(type, filterStatus || undefined);
      setItems(list ?? []);
      setLoadError(false);
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, [type, filterStatus]);

  useEffect(() => {
    setLoading(true);
    void load();
    const t = setInterval(() => void load(), 5000);
    return () => clearInterval(t);
  }, [load]);
  useEffect(() => {
    if (type === "task")
      void Promise.all([controlApi.workItems("requirement"), iamApi.members()])
        .then(([nextRequirements, nextMembers]) => {
          setRequirements(nextRequirements);
          setMembers(nextMembers.filter((value) => value.status === "active"));
        })
        .catch((error) => reportError("加载任务选项", error));
    if (type === "requirement")
      void controlApi
        .okrs()
        .then((values) =>
          setKeyResults(values.flatMap((value) => value.keyResultOptions)),
        );
  }, [type]);

  const create = async () => {
    if (!form.title.trim()) return;
    setCreating(true);
    try {
      let created = await controlApi.createWorkItem({
        type,
        title: form.title.trim(),
        description: form.description,
        status: "open",
        parentId: createFor.parent,
        estimateHours: Number(form.estimateHours) || 0,
        acceptanceCriteria: form.acceptanceCriteria,
        requirementId: form.requirementId,
        keyResultIds: form.keyResultIds,
        priority: form.priority,
        dueAt: dueDateValue(form.dueDate),
        assigneeId: form.assigneeId,
      });
      const uploaded: Attachment[] = [];
      let uploadError: unknown;
      for (const file of files) {
        try {
          uploaded.push(
            await controlApi.uploadAttachment(type, created.id, file),
          );
        } catch (error) {
          uploadError ??= error;
        }
      }
      const description = appendUploadedImages(
        form.description,
        uploaded,
        controlApi.attachmentContentUrl,
      );
      if (description !== form.description)
        created = await controlApi.updateWorkItem(created.id, {
          type,
          description,
          version: created.version,
        });
      setCreateFor({ open: false, parent: "" });
      setForm(workItemForm());
      setFiles([]);
      await load();
      if (uploadError) reportError("部分附件上传", uploadError);
      else toast.success(`已创建${TYPE_LABEL[type] ?? type}`);
    } catch (e) {
      reportError("创建", e);
    } finally {
      setCreating(false);
    }
  };

  const setStatus = async (id: string, status: string) => {
    try {
      const current = items.find((item) => item.id === id);
      if (!current) return;
      await controlApi.updateWorkItem(id, {
        type: current.type,
        status,
        version: current.version,
      });
      void load();
    } catch (e) {
      reportError("更新状态", e);
      void load(); // 回滚到服务端真实状态
    }
  };

  const remove = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await controlApi.deleteWorkItem(
        deleteTarget.id,
        deleteTarget.type,
        deleteTarget.version,
      );
      if (detail?.id === deleteTarget.id) setDetail(null);
      setDeleteTarget(null);
      await load();
    } catch (e) {
      reportError("删除", e);
    } finally {
      setDeleting(false);
    }
  };

  const execute = async (id: string) => {
    try {
      const current = items.find((item) => item.id === id);
      if (!current) return;
      const r = await controlApi.executeWorkItem(id, current.version);
      void load();
      navigate(`/workflow/runs/${r.runId}`);
    } catch (e) {
      reportError("执行", e);
    }
  };

  const childrenOf = (id: string) => items.filter((it) => it.parentId === id);
  const roots = items.filter(
    (it) => !it.parentId || !items.some((p) => p.id === it.parentId),
  );

  // seen 防环:parentId 由 API 可写,自引用或 A→B→A 会让递归爆栈。
  const renderRow = (
    it: WorkItem,
    depth: number,
    seen: ReadonlySet<string> = new Set(),
  ): React.ReactNode => {
    if (seen.has(it.id)) return null;
    const nextSeen = new Set(seen).add(it.id);
    const kids = childrenOf(it.id).filter((c) => !nextSeen.has(c.id));
    const isCollapsed = collapsed[it.id];
    return (
      <div key={it.id}>
        <Card
          className="group p-3 transition-colors duration-200 hover:border-primary/40"
          style={{ marginLeft: depth * 20 }}
        >
          <div className="flex flex-wrap items-center gap-2">
            {kids.length > 0 ? (
              <button
                aria-label={isCollapsed ? "展开子任务" : "收起子任务"}
                className="grid size-11 shrink-0 cursor-pointer place-items-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-accent-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                onClick={(e) => {
                  e.stopPropagation();
                  setCollapsed((c) => ({ ...c, [it.id]: !c[it.id] }));
                }}
              >
                {isCollapsed ? (
                  <ChevronRight className="size-4" />
                ) : (
                  <ChevronDown className="size-4" />
                )}
              </button>
            ) : (
              <span className="inline-block size-11 shrink-0" />
            )}

            <Badge variant={it.type === "bug" ? "destructive" : "secondary"}>
              {TYPE_LABEL[it.type] ?? it.type}
            </Badge>
            <button
              type="button"
              className="min-h-11 min-w-0 cursor-pointer rounded px-1 text-left font-medium hover:underline focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
              onClick={() => setDetail(it)}
            >
              {it.title}
            </button>
            <StatusChip status={it.status} />
            {it.type === "task" && (
              <Badge variant="outline">
                {PRIORITY_LABEL[it.priority] ?? it.priority}
              </Badge>
            )}
            {it.type === "task" && it.dueAt > 0 && (
              <span className="text-xs text-muted-foreground">
                截止 {new Date(it.dueAt).toLocaleDateString()}
              </span>
            )}
            {it.type === "task" && it.assigneeId && (
              <span className="text-xs text-muted-foreground">
                {members.find((value) => value.subject === it.assigneeId)
                  ?.display_name ?? it.assigneeId}
              </span>
            )}

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

            <div
              className="ml-auto flex items-center gap-1.5"
              onClick={(e) => e.stopPropagation()}
            >
              <select
                value={it.status}
                onChange={(e) => setStatus(it.id, e.target.value)}
                className="min-h-11 cursor-pointer rounded-md border border-input bg-transparent px-2 text-xs transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                aria-label={`${it.title} 状态`}
              >
                {WORK_ITEM_STATUSES[type].map((s) => (
                  <option key={s} value={s}>
                    {STATUS_LABEL[s]}
                  </option>
                ))}
              </select>
              {type === "task" && (
                <>
                  <Button
                    size="sm"
                    variant="outline"
                    className="min-h-11 cursor-pointer gap-1"
                    disabled={
                      it.status === "in_progress" || it.status === "done"
                    }
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
                      setCreateFor({ open: true, parent: it.id });
                      setForm(workItemForm(it));
                      setFiles([]);
                    }}
                    aria-label={`为 ${it.title} 新建子任务`}
                  >
                    <Plus className="size-3.5" />
                    子任务
                  </Button>
                </>
              )}
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
          {it.description && (
            <p className="mt-1.5 line-clamp-2 pl-7 text-sm text-muted-foreground">
              {it.description}
            </p>
          )}
        </Card>
        {!isCollapsed && kids.map((c) => renderRow(c, depth + 1, nextSeen))}
      </div>
    );
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">
            {TYPE_LABEL[type] ?? type}管理
          </h1>
          <p className="text-sm text-muted-foreground">
            {type === "requirement"
              ? "集中收集、评审和关联目标，不在需求池填写工时。"
              : "拆分子任务并按工作流执行，父任务进度与工时自动汇总。"}
          </p>
        </div>
        <Button
          className="cursor-pointer gap-1.5"
          onClick={() => {
            setCreateFor({ open: true, parent: "" });
            setForm(workItemForm());
            setFiles([]);
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
          {WORK_ITEM_STATUSES[type].map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>
        <span className="text-muted-foreground">{items.length} 项</span>
      </div>

      <div className="space-y-2">
        {loading && (
          <div
            className="flex min-h-24 items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground"
            role="status"
          >
            <LoaderCircle className="size-4 animate-spin" />
            加载工作项…
          </div>
        )}
        {!loading && loadError && (
          <div
            className="flex min-h-24 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground"
            role="alert"
          >
            <span>工作项加载失败,请检查控制面连接。</span>
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
        {!loading && !loadError && roots.map((it) => renderRow(it, 0))}
        {!loading && !loadError && roots.length === 0 && (
          <div className="rounded-xl border border-dashed p-8 text-center">
            <FileText className="mx-auto size-6 text-muted-foreground" />
            <p className="mt-2 text-sm text-muted-foreground">
              还没有{TYPE_LABEL[type] ?? type}
              ,点「新建」创建,或到会话区把消息转为工作项。
            </p>
          </div>
        )}
      </div>

      <DetailSheet
        item={detail}
        keyResults={keyResults}
        members={members}
        subtasks={detail ? childrenOf(detail.id) : []}
        onClose={() => setDetail(null)}
        onChanged={load}
        onExecute={execute}
        onAddSubtask={(parentId) => {
          setDetail(null);
          setCreateFor({ open: true, parent: parentId });
          setForm(workItemForm(items.find((value) => value.id === parentId)));
          setFiles([]);
        }}
      />

      <Dialog
        open={createFor.open}
        onOpenChange={(v) => {
          if (!v) setCreateFor({ open: false, parent: "" });
        }}
      >
        <DialogContent className="max-h-[92vh] overflow-y-auto sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>
              {createFor.parent
                ? "新建子任务"
                : `新建${TYPE_LABEL[type] ?? type}`}
            </DialogTitle>
          </DialogHeader>
          <div className="grid gap-4 py-2">
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
            <RichContentEditor
              id="wi-description"
              label="描述"
              value={form.description}
              onChange={(description) => setForm({ ...form, description })}
            />
            {type === "requirement" && (
              <>
                <RichContentEditor
                  id="wi-acceptance"
                  label="验收标准"
                  rows={5}
                  value={form.acceptanceCriteria}
                  onChange={(acceptanceCriteria) =>
                    setForm({ ...form, acceptanceCriteria })
                  }
                />
                <div className="grid gap-1.5">
                  <label
                    htmlFor="wi-key-results"
                    className="text-sm font-medium"
                  >
                    关联关键结果
                  </label>
                  <select
                    id="wi-key-results"
                    multiple
                    className="min-h-24 rounded-md border bg-background px-3 py-2"
                    value={form.keyResultIds}
                    onChange={(event) =>
                      setForm({
                        ...form,
                        keyResultIds: Array.from(
                          event.target.selectedOptions,
                          (option) => option.value,
                        ),
                      })
                    }
                  >
                    {keyResults.map((value) => (
                      <option key={value.id} value={value.id}>
                        {value.title}
                      </option>
                    ))}
                  </select>
                </div>
              </>
            )}
            {type === "task" && (
              <>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="grid gap-1.5">
                    <label
                      htmlFor="wi-requirement"
                      className="text-sm font-medium"
                    >
                      关联需求
                    </label>
                    <select
                      id="wi-requirement"
                      className="min-h-11 rounded-md border bg-background px-3"
                      value={form.requirementId}
                      onChange={(event) =>
                        setForm({ ...form, requirementId: event.target.value })
                      }
                    >
                      <option value="">无</option>
                      {requirements.map((value) => (
                        <option key={value.id} value={value.id}>
                          {value.title}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="grid gap-1.5">
                    <label
                      htmlFor="wi-assignee"
                      className="text-sm font-medium"
                    >
                      负责人
                    </label>
                    <select
                      id="wi-assignee"
                      className="min-h-11 rounded-md border bg-background px-3"
                      value={form.assigneeId}
                      onChange={(event) =>
                        setForm({ ...form, assigneeId: event.target.value })
                      }
                    >
                      <option value="">未分配</option>
                      {members.map((value) => (
                        <option key={value.subject} value={value.subject}>
                          {value.display_name || value.email || value.subject}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="grid gap-1.5">
                    <label
                      htmlFor="wi-priority"
                      className="text-sm font-medium"
                    >
                      优先级
                    </label>
                    <select
                      id="wi-priority"
                      className="min-h-11 rounded-md border bg-background px-3"
                      value={form.priority}
                      onChange={(event) =>
                        setForm({ ...form, priority: event.target.value })
                      }
                    >
                      {Object.entries(PRIORITY_LABEL).map(([value, label]) => (
                        <option key={value} value={value}>
                          {label}
                        </option>
                      ))}
                    </select>
                  </div>
                  <div className="grid gap-1.5">
                    <label htmlFor="wi-due" className="text-sm font-medium">
                      截止日期
                    </label>
                    <Input
                      id="wi-due"
                      type="date"
                      value={form.dueDate}
                      onChange={(event) =>
                        setForm({ ...form, dueDate: event.target.value })
                      }
                    />
                  </div>
                </div>
                <div className="grid gap-1.5 sm:max-w-xs">
                  <label htmlFor="wi-est" className="text-sm font-medium">
                    估时(小时)
                  </label>
                  <Input
                    id="wi-est"
                    type="number"
                    min="0"
                    step="0.5"
                    value={form.estimateHours}
                    onChange={(e) =>
                      setForm({ ...form, estimateHours: e.target.value })
                    }
                  />
                </div>
              </>
            )}
            <AttachmentQueue
              id="wi-attachments"
              files={files}
              onChange={setFiles}
              disabled={creating}
            />
            {createFor.parent && (
              <p className="text-xs text-muted-foreground">
                父任务:
                {items.find((value) => value.id === createFor.parent)?.title}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              className="cursor-pointer"
              onClick={() => setCreateFor({ open: false, parent: "" })}
            >
              取消
            </Button>
            <Button
              className="cursor-pointer"
              onClick={create}
              disabled={creating || !form.title.trim()}
            >
              {creating && <LoaderCircle className="size-4 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={deleteTarget !== null}
        onOpenChange={(open) => !open && !deleting && setDeleteTarget(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>删除工作项</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            将删除「{deleteTarget?.title}
            」。已有子任务不会自动删除,但会变为顶层工作项。
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

function DetailSheet({
  item,
  keyResults,
  members,
  subtasks,
  onClose,
  onChanged,
  onExecute,
  onAddSubtask,
}: {
  item: WorkItem | null;
  keyResults: Array<{ id: string; title: string }>;
  members: Member[];
  subtasks: WorkItem[];
  onClose: () => void;
  onChanged: () => void | Promise<void>;
  onExecute: (id: string) => void;
  onAddSubtask: (parentId: string) => void;
}) {
  const [atts, setAtts] = useState<Attachment[]>([]);
  const [desc, setDesc] = useState("");
  const [estimate, setEstimate] = useState("");
  const [acceptanceCriteria, setAcceptanceCriteria] = useState("");
  const [keyResultIds, setKeyResultIds] = useState<string[]>([]);
  const [priority, setPriority] = useState("medium");
  const [dueDate, setDueDate] = useState("");
  const [assigneeId, setAssigneeId] = useState("");
  const [uploading, setUploading] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (!item) return;
    setDesc(item.description);
    setEstimate(item.estimateHours ? String(item.estimateHours) : "");
    setAcceptanceCriteria(item.acceptanceCriteria);
    setKeyResultIds(item.keyResultIds);
    setPriority(item.priority || "medium");
    setDueDate(dateInputValue(item.dueAt));
    setAssigneeId(item.assigneeId);
    void controlApi
      .attachments(item.type, item.id)
      .then((x) => setAtts(x ?? []))
      .catch(() => setAtts([]));
  }, [item]);

  if (!item) return null;

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
        acceptanceCriteria,
        keyResultIds,
        priority,
        dueAt: dueDateValue(dueDate),
        assigneeId,
        version: item.version,
      });
      await onChanged();
      onClose();
    } catch (e) {
      reportError("保存", e);
    }
  };

  const upload = async (files: FileList | null) => {
    if (!files?.length) return;
    setUploading(true);
    try {
      for (const f of Array.from(files))
        await controlApi.uploadAttachment(item.type, item.id, f);
    } catch (e) {
      reportError("上传附件", e);
    } finally {
      // 无论成功与否都刷新:部分成功的文件也要显示出来。
      setAtts(
        (await controlApi.attachments(item.type, item.id).catch(() => [])) ??
          [],
      );
      setUploading(false);
      if (fileRef.current) fileRef.current.value = "";
    }
  };

  const removeAttachment = async (attachment: Attachment) => {
    try {
      await controlApi.deleteAttachment(attachment.id, attachment.version);
      setAtts((current) =>
        current.filter((value) => value.id !== attachment.id),
      );
    } catch (e) {
      reportError("删除附件", e);
    }
  };

  const downloadAttachment = async (attachment: Attachment) => {
    try {
      const blob = await controlApi.attachmentContent(attachment.id);
      const url = URL.createObjectURL(blob);
      const link = document.createElement("a");
      link.href = url;
      link.download = attachment.filename;
      link.click();
      window.setTimeout(() => URL.revokeObjectURL(url), 0);
    } catch (error) {
      reportError("下载附件", error);
    }
  };

  return (
    <Sheet open onOpenChange={(v) => !v && onClose()}>
      <SheetContent className="w-full overflow-y-auto sm:max-w-lg">
        <SheetHeader>
          <SheetTitle className="flex items-center gap-2 pr-6">
            <Badge variant={item.type === "bug" ? "destructive" : "secondary"}>
              {TYPE_LABEL[item.type] ?? item.type}
            </Badge>
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

          {item.type === "task" && (
            <div className="grid gap-1.5">
              <div className="flex items-center justify-between text-sm">
                <span className="font-medium">进度</span>
                <span className="tabular-nums text-muted-foreground">
                  {item.progress}%
                </span>
              </div>
              <Progress value={item.progress} className="h-2" />
              <p className="text-xs text-muted-foreground">
                估 {item.estimateHours.toFixed(1)}h · 实{" "}
                {item.spentHours.toFixed(2)}h
                {item.estimateHours > 0 && item.spentHours > 0 && (
                  <>
                    {" "}
                    · 偏差{" "}
                    {(
                      ((item.spentHours - item.estimateHours) /
                        item.estimateHours) *
                      100
                    ).toFixed(0)}
                    %
                  </>
                )}
              </p>
            </div>
          )}

          {item.type === "requirement" && (
            <>
              <RichContentEditor
                id="d-acceptance"
                label="验收标准"
                rows={5}
                value={acceptanceCriteria}
                onChange={setAcceptanceCriteria}
              />
              <div className="grid gap-1.5">
                <label htmlFor="d-key-results" className="text-sm font-medium">
                  关联关键结果
                </label>
                <select
                  id="d-key-results"
                  multiple
                  className="min-h-24 rounded-md border bg-background px-3 py-2"
                  value={keyResultIds}
                  onChange={(event) =>
                    setKeyResultIds(
                      Array.from(
                        event.target.selectedOptions,
                        (option) => option.value,
                      ),
                    )
                  }
                >
                  {keyResults.map((value) => (
                    <option key={value.id} value={value.id}>
                      {value.title}
                    </option>
                  ))}
                </select>
              </div>
            </>
          )}

          <RichContentEditor
            id="d-desc"
            label="描述"
            value={desc}
            onChange={setDesc}
          />

          {item.type === "task" && (
            <>
              <div className="grid gap-3 sm:grid-cols-2">
                <div className="grid gap-1.5">
                  <label htmlFor="d-assignee" className="text-sm font-medium">
                    负责人
                  </label>
                  <select
                    id="d-assignee"
                    className="min-h-11 rounded-md border bg-background px-3"
                    value={assigneeId}
                    onChange={(event) => setAssigneeId(event.target.value)}
                  >
                    <option value="">未分配</option>
                    {members.map((value) => (
                      <option key={value.subject} value={value.subject}>
                        {value.display_name || value.email || value.subject}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="grid gap-1.5">
                  <label htmlFor="d-priority" className="text-sm font-medium">
                    优先级
                  </label>
                  <select
                    id="d-priority"
                    className="min-h-11 rounded-md border bg-background px-3"
                    value={priority}
                    onChange={(event) => setPriority(event.target.value)}
                  >
                    {Object.entries(PRIORITY_LABEL).map(([value, label]) => (
                      <option key={value} value={value}>
                        {label}
                      </option>
                    ))}
                  </select>
                </div>
                <div className="grid gap-1.5">
                  <label htmlFor="d-due" className="text-sm font-medium">
                    截止日期
                  </label>
                  <Input
                    id="d-due"
                    type="date"
                    value={dueDate}
                    onChange={(event) => setDueDate(event.target.value)}
                  />
                </div>
                <div className="grid gap-1.5">
                  <label htmlFor="d-est" className="text-sm font-medium">
                    估时(小时)
                  </label>
                  <Input
                    id="d-est"
                    type="number"
                    min="0"
                    step="0.5"
                    value={estimate}
                    onChange={(e) => setEstimate(e.target.value)}
                  />
                </div>
              </div>
              <div className="grid gap-2">
                <span className="text-sm font-medium">子任务</span>
                {subtasks.length === 0 ? (
                  <p className="text-xs text-muted-foreground">暂无子任务</p>
                ) : (
                  <div className="divide-y rounded-md border">
                    {subtasks.map((value) => (
                      <div
                        key={value.id}
                        className="flex min-h-11 items-center gap-2 px-3 py-2 text-sm"
                      >
                        <StatusChip status={value.status} />
                        <span className="min-w-0 flex-1 truncate">
                          {value.title}
                        </span>
                        <span className="shrink-0 tabular-nums text-xs text-muted-foreground">
                          {value.spentHours.toFixed(1)}h /{" "}
                          {value.estimateHours.toFixed(1)}h
                        </span>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </>
          )}

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
              <p className="text-xs text-muted-foreground">
                还没有附件。图片、视频、文档都可上传,聊天中也能引用。
              </p>
            )}
            <div className="grid grid-cols-2 gap-2">
              {atts.map((a) => {
                const isImage = a.mime.startsWith("image/");
                const isVideo = a.mime.startsWith("video/");
                return (
                  <div
                    key={a.id}
                    className="relative overflow-hidden rounded-lg border transition-colors hover:border-primary/40 hover:bg-accent/40"
                  >
                    <button
                      type="button"
                      className="block w-full cursor-pointer text-left focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                      aria-label={`下载附件 ${a.filename}`}
                      onClick={() => void downloadAttachment(a)}
                    >
                      {isImage || isVideo ? (
                        <ProtectedAttachmentMedia
                          attachment={a}
                          className="h-24 w-full object-cover"
                        />
                      ) : (
                        <div className="flex h-24 items-center justify-center bg-muted">
                          <FileText className="size-6 text-muted-foreground" />
                        </div>
                      )}
                      <div className="px-2 py-1.5 pr-10">
                        <p className="truncate text-xs font-medium">
                          {a.filename}
                        </p>
                        <p className="text-[11px] text-muted-foreground">
                          {(a.sizeBytes / 1024).toFixed(1)} KB
                        </p>
                      </div>
                    </button>
                    <Button
                      size="icon"
                      variant="ghost"
                      className="absolute right-0.5 bottom-0.5 size-9 text-muted-foreground hover:text-destructive"
                      aria-label={`删除附件 ${a.filename}`}
                      onClick={() => void removeAttachment(a)}
                    >
                      <Trash2 className="size-4" />
                    </Button>
                  </div>
                );
              })}
            </div>
          </div>

          <div className="flex flex-wrap gap-2 border-t pt-4">
            <Button className="cursor-pointer" onClick={save}>
              保存
            </Button>
            {item.type === "task" && (
              <>
                <Button
                  variant="outline"
                  className="cursor-pointer gap-1.5"
                  disabled={
                    item.status === "in_progress" || item.status === "done"
                  }
                  onClick={() => onExecute(item.id)}
                >
                  <CirclePlay className="size-4" />
                  执行工作流
                </Button>
                <Button
                  variant="outline"
                  className="cursor-pointer gap-1.5"
                  onClick={() => onAddSubtask(item.id)}
                >
                  <Plus className="size-4" />
                  新建子任务
                </Button>
              </>
            )}
            <Button
              variant="ghost"
              className="cursor-pointer"
              onClick={onClose}
            >
              关闭
            </Button>
          </div>
        </div>
      </SheetContent>
    </Sheet>
  );
}

const TEST_STATUS_LABEL: Record<string, string> = {
  draft: "草稿",
  active: "生效",
  retired: "已退役",
  queued: "排队中",
  running: "执行中",
  passed: "通过",
  failed: "失败",
  blocked: "阻塞",
  cancelled: "已取消",
};

function TestsPage() {
  const navigate = useNavigate();
  const [cases, setCases] = useState<TestCase[]>([]);
  const [requirements, setRequirements] = useState<WorkItem[]>([]);
  const [runs, setRuns] = useState<TestRun[]>([]);
  const [selected, setSelected] = useState<TestCase | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<TestCase | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<TestCase | null>(null);
  const [pending, setPending] = useState(false);
  const [caseFiles, setCaseFiles] = useState<File[]>([]);
  const [runEnvironment, setRunEnvironment] = useState("local");
  const [runResult, setRunResult] = useState("");
  const [runFailure, setRunFailure] = useState("");
  const [form, setForm] = useState({
    requirementId: "",
    title: "",
    description: "",
    preconditions: "",
    priority: "medium",
    steps: [{ action: "", expectedResult: "" }],
  });

  const load = useCallback(async () => {
    try {
      const [nextCases, nextRequirements] = await Promise.all([
        controlApi.testCases(),
        controlApi.workItems("requirement"),
      ]);
      setCases(nextCases);
      setRequirements(nextRequirements);
      setSelected((current) =>
        current
          ? (nextCases.find((item) => item.id === current.id) ?? null)
          : null,
      );
      setLoadError(false);
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);
  useEffect(() => {
    if (!selected) {
      setRuns([]);
      return;
    }
    void controlApi
      .testRuns(selected.id)
      .then(setRuns)
      .catch(() => setRuns([]));
  }, [selected]);

  const saveCase = async () => {
    if (
      !form.title.trim() ||
      form.steps.some(
        (step) => !step.action.trim() || !step.expectedResult.trim(),
      )
    )
      return;
    setPending(true);
    try {
      const value = {
        requirementId: form.requirementId || undefined,
        title: form.title.trim(),
        description: form.description,
        preconditions: form.preconditions,
        priority: form.priority,
        steps: form.steps,
      };
      let saved: TestCase;
      if (editing) {
        saved = await controlApi.updateTestCase(editing.id, {
          title: value.title,
          description: value.description,
          preconditions: value.preconditions,
          priority: value.priority,
          steps: value.steps,
          version: editing.version,
        });
      } else saved = await controlApi.createTestCase(value);
      const uploaded: Attachment[] = [];
      let uploadError: unknown;
      for (const file of caseFiles) {
        try {
          uploaded.push(
            await controlApi.uploadAttachment("testcase", saved.id, file),
          );
        } catch (error) {
          uploadError ??= error;
        }
      }
      const description = appendUploadedImages(
        value.description,
        uploaded,
        controlApi.attachmentContentUrl,
      );
      if (description !== value.description)
        saved = await controlApi.updateTestCase(saved.id, {
          description,
          version: saved.version,
        });
      setOpen(false);
      setEditing(null);
      setCaseFiles([]);
      setForm({
        requirementId: "",
        title: "",
        description: "",
        preconditions: "",
        priority: "medium",
        steps: [{ action: "", expectedResult: "" }],
      });
      await load();
      if (uploadError) reportError("部分测试附件上传", uploadError);
    } catch (error) {
      reportError("创建测试用例", error);
    } finally {
      setPending(false);
    }
  };

  const beginCreate = () => {
    setEditing(null);
    setCaseFiles([]);
    setForm({
      requirementId: "",
      title: "",
      description: "",
      preconditions: "",
      priority: "medium",
      steps: [{ action: "", expectedResult: "" }],
    });
    setOpen(true);
  };

  const beginEdit = (value: TestCase) => {
    setEditing(value);
    setCaseFiles([]);
    setForm({
      requirementId: value.requirementId ?? "",
      title: value.title,
      description: value.description,
      preconditions: value.preconditions,
      priority: value.priority,
      steps: value.steps.map((step) => ({
        action: step.action,
        expectedResult: step.expectedResult,
      })),
    });
    setOpen(true);
  };

  const removeCase = async () => {
    if (!deleteTarget) return;
    setPending(true);
    try {
      await controlApi.deleteTestCase(deleteTarget.id, deleteTarget.version);
      setDeleteTarget(null);
      if (selected?.id === deleteTarget.id) setSelected(null);
      await load();
    } catch (error) {
      reportError("删除测试用例", error);
    } finally {
      setPending(false);
    }
  };

  const setCaseStatus = async (value: TestCase, status: string) => {
    setPending(true);
    try {
      await controlApi.updateTestCase(value.id, {
        status,
        version: value.version,
      });
      await load();
    } catch (error) {
      reportError("更新测试用例", error);
    } finally {
      setPending(false);
    }
  };

  const createRun = async () => {
    if (!selected || !runEnvironment.trim()) return;
    setPending(true);
    try {
      await controlApi.createTestRun(selected.id, runEnvironment.trim());
      setRuns(await controlApi.testRuns(selected.id));
    } catch (error) {
      reportError("创建测试执行", error);
    } finally {
      setPending(false);
    }
  };

  const updateRun = async (run: TestRun, status: string) => {
    setPending(true);
    try {
      await controlApi.updateTestRun(run.id, {
        status,
        observedResult: runResult || undefined,
        failureSummary: status === "failed" ? runFailure : undefined,
        version: run.version,
      });
      setRuns(await controlApi.testRuns(run.testCaseId));
      setRunResult("");
      setRunFailure("");
    } catch (error) {
      reportError("更新测试执行", error);
    } finally {
      setPending(false);
    }
  };

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">测试管理</h1>
          <p className="text-sm text-muted-foreground">
            维护可复用测试步骤并记录每次真实执行结果。
          </p>
        </div>
        <Button className="min-h-11 gap-2" onClick={beginCreate}>
          <Plus className="size-4" />
          新建测试用例
        </Button>
      </div>
      {loading && (
        <StateBox
          icon={<LoaderCircle className="size-4 animate-spin" />}
          text="加载测试用例…"
        />
      )}
      {!loading && loadError && (
        <StateBox
          icon={<RefreshCw className="size-4" />}
          text="测试用例加载失败"
          action={() => void load()}
        />
      )}
      {!loading && !loadError && cases.length === 0 && (
        <StateBox
          icon={<TestTube2 className="size-5" />}
          text="还没有测试用例。"
        />
      )}
      {!loading && !loadError && cases.length > 0 && (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[700px] text-sm">
            <thead className="bg-muted/50 text-left">
              <tr>
                <th className="p-3">用例</th>
                <th className="p-3">优先级</th>
                <th className="p-3">步骤</th>
                <th className="p-3">状态</th>
                <th className="p-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {cases.map((item) => (
                <tr key={item.id} className="border-t">
                  <td className="p-3">
                    <button
                      className="min-h-11 text-left font-medium hover:underline"
                      onClick={() => setSelected(item)}
                    >
                      {item.title}
                    </button>
                    <p className="max-w-xl truncate text-xs text-muted-foreground">
                      {item.description || "无描述"}
                    </p>
                  </td>
                  <td className="p-3">{item.priority}</td>
                  <td className="p-3 tabular-nums">{item.steps.length}</td>
                  <td className="p-3">
                    <Badge variant="secondary">
                      {TEST_STATUS_LABEL[item.status]}
                    </Badge>
                  </td>
                  <td className="p-3 text-right">
                    <div className="flex justify-end gap-2">
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`编辑 ${item.title}`}
                        disabled={pending || item.status !== "draft"}
                        onClick={() => beginEdit(item)}
                      >
                        <Pencil className="size-4" />
                      </Button>
                      {item.status === "draft" && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={pending || item.steps.length === 0}
                          onClick={() => void setCaseStatus(item, "active")}
                        >
                          生效
                        </Button>
                      )}
                      {item.status !== "retired" && (
                        <Button
                          size="sm"
                          variant="ghost"
                          disabled={pending}
                          onClick={() => void setCaseStatus(item, "retired")}
                        >
                          退役
                        </Button>
                      )}
                      <Button
                        size="icon"
                        variant="ghost"
                        aria-label={`删除 ${item.title}`}
                        disabled={pending}
                        onClick={() => setDeleteTarget(item)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <Sheet
        open={selected !== null}
        onOpenChange={(value) => !value && setSelected(null)}
      >
        <SheetContent className="w-full overflow-y-auto sm:max-w-xl">
          <SheetHeader>
            <SheetTitle>{selected?.title}</SheetTitle>
          </SheetHeader>
          {selected && (
            <div className="space-y-5 px-4 pb-6">
              <div>
                <p className="mb-1 text-sm font-medium">描述</p>
                <MarkdownView value={selected.description} />
              </div>
              <div>
                <p className="text-sm font-medium">前置条件</p>
                <p className="mt-1 whitespace-pre-wrap text-sm text-muted-foreground">
                  {selected.preconditions || "无"}
                </p>
              </div>
              <ol className="space-y-2">
                {selected.steps.map((step) => (
                  <li
                    key={step.id ?? step.position}
                    className="grid grid-cols-[2rem_1fr] gap-2 border-b pb-2 text-sm"
                  >
                    <span className="tabular-nums text-muted-foreground">
                      {step.position}.
                    </span>
                    <div>
                      <p>{step.action}</p>
                      <p className="text-muted-foreground">
                        预期：{step.expectedResult}
                      </p>
                    </div>
                  </li>
                ))}
              </ol>
              <div className="grid gap-2 border-t pt-4">
                <label
                  htmlFor="run-environment"
                  className="text-sm font-medium"
                >
                  执行环境
                </label>
                <div className="flex gap-2">
                  <Input
                    id="run-environment"
                    value={runEnvironment}
                    onChange={(event) => setRunEnvironment(event.target.value)}
                  />
                  <Button
                    disabled={pending || selected.status !== "active"}
                    onClick={() => void createRun()}
                  >
                    <CirclePlay className="size-4" />
                    创建执行
                  </Button>
                </div>
              </div>
              <div className="grid gap-2">
                <label htmlFor="run-result" className="text-sm font-medium">
                  观察结果
                </label>
                <Textarea
                  id="run-result"
                  value={runResult}
                  onChange={(event) => setRunResult(event.target.value)}
                />
                <label htmlFor="run-failure" className="text-sm font-medium">
                  失败摘要
                </label>
                <Input
                  id="run-failure"
                  value={runFailure}
                  onChange={(event) => setRunFailure(event.target.value)}
                />
              </div>
              <div className="space-y-2">
                {runs.length === 0 && (
                  <p className="text-sm text-muted-foreground">
                    暂无执行记录。
                  </p>
                )}
                {runs.map((run) => (
                  <div
                    key={run.id}
                    className="flex flex-wrap items-center gap-2 rounded-md border p-2 text-sm"
                  >
                    <Badge variant="outline">
                      {TEST_STATUS_LABEL[run.status]}
                    </Badge>
                    <span className="text-muted-foreground">
                      {run.environment}
                    </span>
                    <div className="ml-auto flex gap-1">
                      {run.status === "queued" && (
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={pending}
                          onClick={() => void updateRun(run, "running")}
                        >
                          开始
                        </Button>
                      )}
                      {run.status === "running" && (
                        <>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label="标记通过"
                            disabled={pending}
                            onClick={() => void updateRun(run, "passed")}
                          >
                            <CheckCircle2 className="size-4 text-emerald-600" />
                          </Button>
                          <Button
                            size="icon"
                            variant="ghost"
                            aria-label="标记失败"
                            disabled={pending || !runFailure.trim()}
                            onClick={() => void updateRun(run, "failed")}
                          >
                            <XCircle className="size-4 text-destructive" />
                          </Button>
                          <Button
                            size="sm"
                            variant="ghost"
                            disabled={pending}
                            onClick={() => void updateRun(run, "blocked")}
                          >
                            阻塞
                          </Button>
                        </>
                      )}
                      {run.status === "failed" && (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() =>
                            navigate(
                              `/workspace/bugs?sourceType=testRun&sourceId=${run.id}`,
                            )
                          }
                        >
                          新建缺陷
                        </Button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </SheetContent>
      </Sheet>

      <Dialog open={open} onOpenChange={(value) => !pending && setOpen(value)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>
              {editing ? "编辑测试用例" : "新建测试用例"}
            </DialogTitle>
          </DialogHeader>
          <div className="grid gap-3">
            <Field label="关联需求" id="test-requirement">
              <select
                id="test-requirement"
                disabled={editing !== null}
                className="min-h-11 rounded-md border bg-background px-3"
                value={form.requirementId}
                onChange={(event) =>
                  setForm({ ...form, requirementId: event.target.value })
                }
              >
                <option value="">无</option>
                {requirements.map((value) => (
                  <option key={value.id} value={value.id}>
                    {value.title}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="标题" id="test-title">
              <Input
                id="test-title"
                value={form.title}
                onChange={(event) =>
                  setForm({ ...form, title: event.target.value })
                }
              />
            </Field>
            <RichContentEditor
              id="test-description"
              label="描述"
              value={form.description}
              onChange={(description) => setForm({ ...form, description })}
            />
            <Field label="前置条件" id="test-preconditions">
              <Textarea
                id="test-preconditions"
                value={form.preconditions}
                onChange={(event) =>
                  setForm({ ...form, preconditions: event.target.value })
                }
              />
            </Field>
            <Field label="优先级" id="test-priority">
              <select
                id="test-priority"
                className="min-h-11 rounded-md border bg-background px-3"
                value={form.priority}
                onChange={(event) =>
                  setForm({ ...form, priority: event.target.value })
                }
              >
                {["highest", "high", "medium", "low", "lowest"].map((value) => (
                  <option key={value}>{value}</option>
                ))}
              </select>
            </Field>
            {form.steps.map((step, index) => (
              <div
                key={index}
                className="grid gap-2 border-t pt-3 sm:grid-cols-2"
              >
                <Input
                  aria-label={`步骤 ${index + 1} 操作`}
                  placeholder={`步骤 ${index + 1} 操作`}
                  value={step.action}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      steps: form.steps.map((item, position) =>
                        position === index
                          ? { ...item, action: event.target.value }
                          : item,
                      ),
                    })
                  }
                />
                <Input
                  aria-label={`步骤 ${index + 1} 预期结果`}
                  placeholder="预期结果"
                  value={step.expectedResult}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      steps: form.steps.map((item, position) =>
                        position === index
                          ? { ...item, expectedResult: event.target.value }
                          : item,
                      ),
                    })
                  }
                />
              </div>
            ))}
            <AttachmentQueue
              id="test-attachments"
              files={caseFiles}
              onChange={setCaseFiles}
              disabled={pending}
            />
            <Button
              variant="outline"
              className="justify-self-start"
              onClick={() =>
                setForm({
                  ...form,
                  steps: [...form.steps, { action: "", expectedResult: "" }],
                })
              }
            >
              <Plus className="size-4" />
              添加步骤
            </Button>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button
              disabled={pending || !form.title.trim()}
              onClick={() => void saveCase()}
            >
              {pending && <LoaderCircle className="size-4 animate-spin" />}
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={deleteTarget !== null}
        onOpenChange={(value) => !value && !pending && setDeleteTarget(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>删除测试用例</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            将删除「{deleteTarget?.title}」及其步骤。
          </p>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => setDeleteTarget(null)}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={pending}
              onClick={() => void removeCase()}
            >
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

const DEFECT_STATUS_LABEL: Record<string, string> = {
  open: "待分诊",
  triaged: "已分诊",
  in_progress: "处理中",
  resolved: "已解决",
  verified: "已验证",
  closed: "已关闭",
  reopened: "已重开",
  rejected: "已拒绝",
};

function DefectsPage() {
  const [searchParams] = useSearchParams();
  const [defects, setDefects] = useState<Defect[]>([]);
  const [tasks, setTasks] = useState<WorkItem[]>([]);
  const [requirements, setRequirements] = useState<WorkItem[]>([]);
  const [testCases, setTestCases] = useState<TestCase[]>([]);
  const [testRuns, setTestRuns] = useState<TestRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);
  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Defect | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<Defect | null>(null);
  const [pending, setPending] = useState(false);
  const [defectFiles, setDefectFiles] = useState<File[]>([]);
  const [resolutionNotes, setResolutionNotes] = useState<
    Record<string, string>
  >({});
  const [form, setForm] = useState({
    sourceType: searchParams.get("sourceType") ?? "task",
    sourceId: searchParams.get("sourceId") ?? "",
    title: "",
    description: "",
    reproductionSteps: "",
    expectedResult: "",
    actualResult: "",
    severity: "major",
    priority: "medium",
  });

  const load = useCallback(async () => {
    try {
      const [
        nextDefects,
        nextTasks,
        nextRequirements,
        nextTestCases,
        nextTestRuns,
      ] = await Promise.all([
        controlApi.defects(),
        controlApi.workItems("task"),
        controlApi.workItems("requirement"),
        controlApi.testCases(),
        controlApi.testRuns(),
      ]);
      setDefects(nextDefects);
      setTasks(nextTasks);
      setRequirements(nextRequirements);
      setTestCases(nextTestCases);
      setTestRuns(nextTestRuns);
      setLoadError(false);
    } catch {
      setLoadError(true);
    } finally {
      setLoading(false);
    }
  }, []);
  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (searchParams.get("sourceId")) setOpen(true);
  }, [searchParams]);

  const saveDefect = async () => {
    if (
      !form.sourceId ||
      !form.title.trim() ||
      !form.reproductionSteps.trim() ||
      !form.expectedResult.trim() ||
      !form.actualResult.trim()
    )
      return;
    setPending(true);
    try {
      const sourceField = (
        {
          task: "taskId",
          requirement: "requirementId",
          testcase: "testCaseId",
          testRun: "testRunId",
        } as const
      )[form.sourceType as "task" | "requirement" | "testcase" | "testRun"];
      const value = {
        [sourceField]: form.sourceId,
        title: form.title.trim(),
        description: form.description,
        reproductionSteps: form.reproductionSteps,
        expectedResult: form.expectedResult,
        actualResult: form.actualResult,
        severity: form.severity,
        priority: form.priority,
      };
      let saved: Defect;
      if (editing) {
        saved = await controlApi.updateDefect(editing.id, {
          title: value.title,
          description: value.description,
          reproductionSteps: value.reproductionSteps,
          expectedResult: value.expectedResult,
          actualResult: value.actualResult,
          severity: value.severity,
          priority: value.priority,
          version: editing.version,
        });
      } else saved = await controlApi.createDefect(value);
      const uploaded: Attachment[] = [];
      let uploadError: unknown;
      for (const file of defectFiles) {
        try {
          uploaded.push(
            await controlApi.uploadAttachment("defect", saved.id, file),
          );
        } catch (error) {
          uploadError ??= error;
        }
      }
      const description = appendUploadedImages(
        value.description,
        uploaded,
        controlApi.attachmentContentUrl,
      );
      if (description !== value.description)
        saved = await controlApi.updateDefect(saved.id, {
          description,
          version: saved.version,
        });
      setOpen(false);
      setEditing(null);
      setDefectFiles([]);
      setForm({
        sourceType: "task",
        sourceId: "",
        title: "",
        description: "",
        reproductionSteps: "",
        expectedResult: "",
        actualResult: "",
        severity: "major",
        priority: "medium",
      });
      await load();
      if (uploadError) reportError("部分缺陷附件上传", uploadError);
    } catch (error) {
      reportError("创建缺陷", error);
    } finally {
      setPending(false);
    }
  };

  const beginCreate = () => {
    setEditing(null);
    setDefectFiles([]);
    setForm({
      sourceType: "task",
      sourceId: "",
      title: "",
      description: "",
      reproductionSteps: "",
      expectedResult: "",
      actualResult: "",
      severity: "major",
      priority: "medium",
    });
    setOpen(true);
  };

  const beginEdit = (value: Defect) => {
    const source = value.testRunId
      ? ["testRun", value.testRunId]
      : value.testCaseId
        ? ["testcase", value.testCaseId]
        : value.taskId
          ? ["task", value.taskId]
          : ["requirement", value.requirementId ?? ""];
    setEditing(value);
    setDefectFiles([]);
    setForm({
      sourceType: source[0],
      sourceId: source[1],
      title: value.title,
      description: value.description,
      reproductionSteps: value.reproductionSteps,
      expectedResult: value.expectedResult,
      actualResult: value.actualResult,
      severity: value.severity,
      priority: value.priority,
    });
    setOpen(true);
  };

  const removeDefect = async () => {
    if (!deleteTarget) return;
    setPending(true);
    try {
      await controlApi.deleteDefect(deleteTarget.id, deleteTarget.version);
      setDeleteTarget(null);
      await load();
    } catch (error) {
      reportError("删除缺陷", error);
    } finally {
      setPending(false);
    }
  };

  const transition = async (item: Defect, status: string) => {
    setPending(true);
    try {
      await controlApi.updateDefect(item.id, {
        status,
        resolution: status === "resolved" ? "fixed" : undefined,
        resolutionNote:
          status === "resolved" ? resolutionNotes[item.id] : undefined,
        version: item.version,
      });
      setResolutionNotes((values) => ({ ...values, [item.id]: "" }));
      await load();
    } catch (error) {
      reportError("更新缺陷", error);
    } finally {
      setPending(false);
    }
  };

  const sourceOptions =
    form.sourceType === "task"
      ? tasks
      : form.sourceType === "requirement"
        ? requirements
        : form.sourceType === "testcase"
          ? testCases
          : testRuns.map((run) => ({
              id: run.id,
              title: `${TEST_STATUS_LABEL[run.status]} · ${run.environment}`,
            }));
  const sourceOf = (item: Defect) => {
    if (item.testRunId)
      return {
        type: "测试执行",
        title:
          testRuns.find((value) => value.id === item.testRunId)?.environment ??
          item.testRunId,
      };
    if (item.testCaseId)
      return {
        type: "测试用例",
        title:
          testCases.find((value) => value.id === item.testCaseId)?.title ??
          item.testCaseId,
      };
    if (item.taskId)
      return {
        type: "任务",
        title:
          tasks.find((value) => value.id === item.taskId)?.title ?? item.taskId,
      };
    return {
      type: "需求",
      title:
        requirements.find((value) => value.id === item.requirementId)?.title ??
        item.requirementId,
    };
  };
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold">缺陷管理</h1>
          <p className="text-sm text-muted-foreground">
            从需求、任务或测试结果追踪问题直至验证关闭。
          </p>
        </div>
        <Button className="min-h-11 gap-2" onClick={beginCreate}>
          <Plus className="size-4" />
          新建缺陷
        </Button>
      </div>
      {loading && (
        <StateBox
          icon={<LoaderCircle className="size-4 animate-spin" />}
          text="加载缺陷…"
        />
      )}
      {!loading && loadError && (
        <StateBox
          icon={<RefreshCw className="size-4" />}
          text="缺陷加载失败"
          action={() => void load()}
        />
      )}
      {!loading && !loadError && defects.length === 0 && (
        <StateBox icon={<Bug className="size-5" />} text="还没有缺陷。" />
      )}
      {!loading && !loadError && defects.length > 0 && (
        <div className="overflow-x-auto rounded-md border">
          <table className="w-full min-w-[980px] text-sm">
            <thead className="bg-muted/50 text-left">
              <tr>
                <th className="p-3">缺陷</th>
                <th className="p-3">严重度</th>
                <th className="p-3">优先级</th>
                <th className="p-3">状态</th>
                <th className="p-3">来源</th>
                <th className="p-3">解决说明</th>
                <th className="p-3 text-right">操作</th>
              </tr>
            </thead>
            <tbody>
              {defects.map((item) => {
                const source = sourceOf(item);
                return (
                  <tr key={item.id} className="border-t align-top">
                    <td className="p-3">
                      <p className="font-medium">{item.title}</p>
                      <p className="max-w-sm truncate text-xs text-muted-foreground">
                        {item.actualResult}
                      </p>
                    </td>
                    <td className="p-3">
                      <Badge
                        variant={
                          item.severity === "critical" ||
                          item.severity === "blocker"
                            ? "destructive"
                            : "outline"
                        }
                      >
                        {item.severity}
                      </Badge>
                    </td>
                    <td className="p-3">{item.priority}</td>
                    <td className="p-3">{DEFECT_STATUS_LABEL[item.status]}</td>
                    <td className="p-3">
                      <p>{source.type}</p>
                      <p className="max-w-40 truncate text-xs text-muted-foreground">
                        {source.title}
                      </p>
                    </td>
                    <td className="p-3">
                      <Input
                        className="min-w-48"
                        aria-label={`${item.title} 解决说明`}
                        disabled={
                          item.status !== "in_progress" &&
                          item.status !== "reopened"
                        }
                        value={
                          item.status === "in_progress" ||
                          item.status === "reopened"
                            ? (resolutionNotes[item.id] ?? "")
                            : item.resolutionNote
                        }
                        onChange={(event) =>
                          setResolutionNotes((values) => ({
                            ...values,
                            [item.id]: event.target.value,
                          }))
                        }
                      />
                    </td>
                    <td className="p-3">
                      <DefectActions
                        item={item}
                        pending={pending}
                        resolutionNote={resolutionNotes[item.id] ?? ""}
                        onTransition={transition}
                      />
                      <div className="mt-1 flex justify-end gap-1">
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`编辑 ${item.title}`}
                          disabled={
                            pending ||
                            !["open", "triaged"].includes(item.status)
                          }
                          onClick={() => beginEdit(item)}
                        >
                          <Pencil className="size-4" />
                        </Button>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={`删除 ${item.title}`}
                          disabled={pending}
                          onClick={() => setDeleteTarget(item)}
                        >
                          <Trash2 className="size-4" />
                        </Button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}
      <Dialog open={open} onOpenChange={(value) => !pending && setOpen(value)}>
        <DialogContent className="max-h-[90vh] overflow-y-auto sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>{editing ? "编辑缺陷" : "新建缺陷"}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3">
            <div className="grid grid-cols-2 gap-3">
              <Field label="来源类型" id="defect-source-type">
                <select
                  id="defect-source-type"
                  disabled={editing !== null}
                  className="min-h-11 rounded-md border bg-background px-3"
                  value={form.sourceType}
                  onChange={(event) =>
                    setForm({
                      ...form,
                      sourceType: event.target.value,
                      sourceId: "",
                    })
                  }
                >
                  <option value="task">任务</option>
                  <option value="requirement">需求</option>
                  <option value="testcase">测试用例</option>
                  <option value="testRun">测试执行</option>
                </select>
              </Field>
              <Field label="来源" id="defect-source">
                <select
                  id="defect-source"
                  disabled={editing !== null}
                  className="min-h-11 min-w-0 rounded-md border bg-background px-3"
                  value={form.sourceId}
                  onChange={(event) =>
                    setForm({ ...form, sourceId: event.target.value })
                  }
                >
                  <option value="">请选择</option>
                  {sourceOptions.map((item) => (
                    <option key={item.id} value={item.id}>
                      {item.title}
                    </option>
                  ))}
                </select>
              </Field>
            </div>
            <Field label="标题" id="defect-title">
              <Input
                id="defect-title"
                value={form.title}
                onChange={(event) =>
                  setForm({ ...form, title: event.target.value })
                }
              />
            </Field>
            <RichContentEditor
              id="defect-description"
              label="描述"
              value={form.description}
              onChange={(description) => setForm({ ...form, description })}
            />
            <Field label="复现步骤" id="defect-reproduction">
              <Textarea
                id="defect-reproduction"
                value={form.reproductionSteps}
                onChange={(event) =>
                  setForm({ ...form, reproductionSteps: event.target.value })
                }
              />
            </Field>
            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="预期结果" id="defect-expected">
                <Textarea
                  id="defect-expected"
                  value={form.expectedResult}
                  onChange={(event) =>
                    setForm({ ...form, expectedResult: event.target.value })
                  }
                />
              </Field>
              <Field label="实际结果" id="defect-actual">
                <Textarea
                  id="defect-actual"
                  value={form.actualResult}
                  onChange={(event) =>
                    setForm({ ...form, actualResult: event.target.value })
                  }
                />
              </Field>
            </div>
            <div className="grid grid-cols-2 gap-3">
              <Field label="严重度" id="defect-severity">
                <select
                  id="defect-severity"
                  className="min-h-11 rounded-md border bg-background px-3"
                  value={form.severity}
                  onChange={(event) =>
                    setForm({ ...form, severity: event.target.value })
                  }
                >
                  {["blocker", "critical", "major", "minor", "trivial"].map(
                    (value) => (
                      <option key={value}>{value}</option>
                    ),
                  )}
                </select>
              </Field>
              <Field label="优先级" id="defect-priority">
                <select
                  id="defect-priority"
                  className="min-h-11 rounded-md border bg-background px-3"
                  value={form.priority}
                  onChange={(event) =>
                    setForm({ ...form, priority: event.target.value })
                  }
                >
                  {["highest", "high", "medium", "low", "lowest"].map(
                    (value) => (
                      <option key={value}>{value}</option>
                    ),
                  )}
                </select>
              </Field>
            </div>
            <AttachmentQueue
              id="defect-attachments"
              files={defectFiles}
              onChange={setDefectFiles}
              disabled={pending}
            />
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => setOpen(false)}
            >
              取消
            </Button>
            <Button
              disabled={pending || !form.sourceId || !form.title.trim()}
              onClick={() => void saveDefect()}
            >
              {pending && <LoaderCircle className="size-4 animate-spin" />}
              {editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={deleteTarget !== null}
        onOpenChange={(value) => !value && !pending && setDeleteTarget(null)}
      >
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>删除缺陷</DialogTitle>
          </DialogHeader>
          <p className="text-sm text-muted-foreground">
            将删除「{deleteTarget?.title}」。
          </p>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={pending}
              onClick={() => setDeleteTarget(null)}
            >
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={pending}
              onClick={() => void removeDefect()}
            >
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function DefectActions({
  item,
  pending,
  resolutionNote,
  onTransition,
}: {
  item: Defect;
  pending: boolean;
  resolutionNote: string;
  onTransition: (item: Defect, status: string) => Promise<void>;
}) {
  const actions: Record<string, Array<[string, string]>> = {
    open: [
      ["分诊", "triaged"],
      ["处理", "in_progress"],
    ],
    triaged: [["处理", "in_progress"]],
    in_progress: [
      ["解决", "resolved"],
      ["拒绝", "rejected"],
    ],
    reopened: [["解决", "resolved"]],
    resolved: [
      ["验证", "verified"],
      ["重开", "reopened"],
    ],
    verified: [
      ["关闭", "closed"],
      ["重开", "reopened"],
    ],
  };
  return (
    <div className="flex justify-end gap-1">
      {(actions[item.status] ?? []).map(([label, status]) => (
        <Button
          key={status}
          size="sm"
          variant="outline"
          disabled={
            pending || (status === "resolved" && !resolutionNote.trim())
          }
          onClick={() => void onTransition(item, status)}
        >
          {label}
        </Button>
      ))}
    </div>
  );
}

function Field({
  label,
  id,
  children,
}: {
  label: string;
  id: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid gap-1.5">
      <label htmlFor={id} className="text-sm font-medium">
        {label}
      </label>
      {children}
    </div>
  );
}

function StateBox({
  icon,
  text,
  action,
}: {
  icon: React.ReactNode;
  text: string;
  action?: () => void;
}) {
  return (
    <div
      className="flex min-h-24 flex-col items-center justify-center gap-2 rounded-md border border-dashed text-sm text-muted-foreground"
      role={action ? "alert" : "status"}
    >
      {icon}
      <span>{text}</span>
      {action && (
        <Button variant="outline" className="min-h-11" onClick={action}>
          重试
        </Button>
      )}
    </div>
  );
}
