import { useCallback, useEffect, useMemo, useState } from "react";
import { useParams } from "react-router";
import {
  ReactFlow,
  Background,
  Controls,
  Handle,
  Position,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import {
  Check,
  LoaderCircle,
  Pause,
  Play,
  RefreshCw,
  ShieldQuestion,
  Square,
  X,
} from "lucide-react";
import { Button } from "@workspace/ui/components/button";
import { toast } from "@workspace/ui/components/sonner";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog";
import { Input } from "@workspace/ui/components/input";
import { Textarea } from "@workspace/ui/components/textarea";
import {
  controlApi,
  FEEDBACK_CATEGORIES,
  type FeedbackCategory,
  type RunDetailResponse,
} from "../../../lib/control-api";
import { useTranslations } from "use-intl";

const STATUS_COLOR: Record<string, string> = {
  pending: "#5b6270",
  running: "#d9a13c",
  completed: "#3fb96f",
  failed: "#e0564e",
  skipped: "#5b6270",
  waiting_approval: "#4aa3e8",
  paused_for_human: "#d9a13c",
  rejected: "#e0564e",
};

interface DefNode {
  id: string;
  type?: string;
}
interface DefEdge {
  from: string;
  to: string;
  condition?: string;
}

function FlowNode({ data }: NodeProps) {
  const d = data as { label: string; status: string };
  return (
    <>
      <Handle type="target" position={Position.Top} />
      <div
        className="rounded-md border px-3 py-2 text-xs"
        style={{
          borderColor: STATUS_COLOR[d.status] ?? "#5b6270",
          background: d.status === "waiting_approval" ? "#1c2a3a" : "#1b1e26",
          color: "#e6e8ee",
        }}
      >
        <div className="font-medium">{d.label}</div>
        <div className="text-[10px] opacity-70">{d.status}</div>
      </div>
      <Handle type="source" position={Position.Bottom} />
    </>
  );
}

/** 简单 rank 布局:BFS 按入度分层,同层纵向排开。 */
function layout(
  nodes: DefNode[],
  edges: DefEdge[],
): { nodes: Node[]; edges: Edge[] } {
  const inDegree: Record<string, number> = {};
  for (const n of nodes) inDegree[n.id] = 0;
  for (const e of edges) inDegree[e.to] = (inDegree[e.to] ?? 0) + 1;

  const rank: Record<string, number> = {};
  const byRank: Record<number, string[]> = {};
  const queue = nodes
    .filter((n) => (inDegree[n.id] ?? 0) === 0)
    .map((n) => n.id);
  for (const id of queue) {
    rank[id] = 0;
    byRank[0] = [...(byRank[0] ?? []), id];
  }

  const index = new Set(queue);
  while (queue.length) {
    const id = queue.shift()!;
    for (const e of edges.filter((e) => e.from === id)) {
      inDegree[e.to] -= 1;
      if (inDegree[e.to] === 0 && !index.has(e.to)) {
        index.add(e.to);
        rank[e.to] = (rank[id] ?? 0) + 1;
        byRank[rank[e.to]] = [...(byRank[rank[e.to]] ?? []), e.to];
        queue.push(e.to);
      }
    }
  }

  const rn: Node[] = nodes.map((n) => ({
    id: n.id,
    type: "flow",
    position: {
      x: (rank[n.id] ?? 0) * 240,
      y: (byRank[rank[n.id] ?? 0] ?? []).indexOf(n.id) * 120,
    },
    data: { label: `${n.id} · ${n.type ?? "node"}`, status: "pending" },
  }));
  const re: Edge[] = edges.map((e, i) => ({
    id: `e${i}`,
    source: e.from,
    target: e.to,
    animated: true,
    label: e.condition ?? "success",
  }));
  return { nodes: rn, edges: re };
}

export default function RunDetail() {
  const t = useTranslations("platform.runDetail");
  const { runId } = useParams();
  const [detail, setDetail] = useState<RunDetailResponse | null>(null);
  const [statuses, setStatuses] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [action, setAction] = useState<"pause" | "resume" | "cancel" | null>(
    null,
  );
  const [approvalBusy, setApprovalBusy] = useState<string | null>(null);

  const load = useCallback(async () => {
    if (!runId) return;
    setLoading(true);
    try {
      const next = await controlApi.runDetail(runId);
      setDetail(next);
      setLoadError("");
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : t("loadFailed"));
    } finally {
      setLoading(false);
    }
  }, [runId, t]);

  useEffect(() => {
    const timer = window.setTimeout(() => void load(), 0);
    return () => window.clearTimeout(timer);
  }, [load]);

  useEffect(() => {
    if (!runId) return;
    const proto = location.protocol === "https:" ? "wss:" : "ws:";
    const ws = new WebSocket(
      `${proto}//${location.host}/control/api/runs/${runId}/events`,
    );
    ws.onmessage = (e) => {
      try {
        const ev = JSON.parse(e.data) as {
          nodeId?: string;
          status?: string;
          runStatus?: string;
        };
        if (ev.nodeId && ev.status)
          setStatuses((s) => ({ ...s, [ev.nodeId!]: ev.status! }));
        if (ev.runStatus)
          setDetail((current) =>
            current ? { ...current, status: ev.runStatus! } : current,
          );
      } catch {
        // Ignore malformed realtime messages; the REST retry remains available.
      }
    };
    return () => ws.close();
  }, [runId]);

  const { nodes, edges } = useMemo(() => {
    if (!detail?.def?.nodes) return { nodes: [], edges: [] };
    return layout(detail.def.nodes, detail.def.edges ?? []);
  }, [detail]);

  const flowNodes = useMemo<Node[]>(
    () =>
      nodes.map((n) => ({
        ...n,
        data: { ...(n.data as object), status: statuses[n.id] ?? "pending" },
      })),
    [nodes, statuses],
  );

  const [reject, setReject] = useState<{ nodeId: string } | null>(null);
  const [fb, setFb] = useState<{
    category: FeedbackCategory;
    location: string;
    expected: string;
    detail: string;
  }>({
    category: "功能缺陷",
    location: "",
    expected: "",
    detail: "",
  });
  const [fbError, setFbError] = useState("");

  const waitingNodes = flowNodes.filter(
    (n) => (n.data as { status: string }).status === "waiting_approval",
  );

  const approveNode = async (nodeId: string) => {
    if (!runId || approvalBusy) return;
    setApprovalBusy(nodeId);
    try {
      await controlApi.approve(runId, nodeId, true);
      setStatuses((current) => ({ ...current, [nodeId]: "completed" }));
      toast.success(t("approved"));
      await load();
    } catch (e) {
      toast.error(
        t("approveFailed", {
          error: e instanceof Error ? e.message : String(e),
        }),
      );
    } finally {
      setApprovalBusy(null);
    }
  };

  const submitRejection = async () => {
    if (!runId || !reject || approvalBusy) return;
    if (!fb.detail.trim()) {
      setFbError(t("detailRequired"));
      return;
    }
    setApprovalBusy(reject.nodeId);
    try {
      await controlApi.approve(runId, reject.nodeId, false, fb.detail, {
        category: fb.category,
        location: fb.location || undefined,
        expected: fb.expected || undefined,
        detail: fb.detail.trim(),
      });
      setReject(null);
      setFb({ category: "功能缺陷", location: "", expected: "", detail: "" });
      setFbError("");
      toast.success(t("rejected"));
      await load();
    } catch (e) {
      setFbError(e instanceof Error ? e.message : t("submitFailed"));
    } finally {
      setApprovalBusy(null);
    }
  };

  const changeLifecycle = async (next: "pause" | "resume" | "cancel") => {
    if (!runId || action) return;
    if (next === "cancel" && !window.confirm(t("cancelConfirm"))) return;
    setAction(next);
    try {
      if (next === "pause") await controlApi.pauseRun(runId);
      else if (next === "resume") await controlApi.resumeRun(runId);
      else await controlApi.cancelRun(runId);
      toast.success(
        t(
          next === "pause"
            ? "paused"
            : next === "resume"
              ? "resumed"
              : "cancelled",
        ),
      );
      await load();
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t("actionFailed"));
    } finally {
      setAction(null);
    }
  };

  if (loading && !detail) {
    return (
      <div
        className="flex min-h-40 items-center justify-center gap-2 text-sm text-muted-foreground"
        role="status"
      >
        <LoaderCircle className="size-4 animate-spin" aria-hidden="true" />
        {t("loading")}
      </div>
    );
  }

  if (loadError && !detail) {
    return (
      <div
        className="flex min-h-40 flex-col items-center justify-center gap-3 text-sm text-muted-foreground"
        role="alert"
      >
        <span>{t("loadError", { error: loadError })}</span>
        <Button
          variant="outline"
          className="min-h-11 gap-2"
          onClick={() => void load()}
        >
          <RefreshCw className="size-4" aria-hidden="true" />
          {t("retry")}
        </Button>
      </div>
    );
  }

  if (!detail) return null;

  const canPause =
    detail.status === "running" || detail.status === "waiting_approval";
  const canResume = detail.status === "paused";
  const canCancel = !["completed", "failed", "cancelled"].includes(
    detail.status,
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <h1 className="min-w-0 break-all text-xl font-semibold">
          Run {detail.id}
        </h1>
        <span className="text-sm text-muted-foreground" aria-live="polite">
          {detail.status}
        </span>
        <div className="flex w-full flex-wrap gap-2 sm:ml-auto sm:w-auto">
          {canPause && (
            <Button
              variant="outline"
              className="min-h-11 gap-2"
              disabled={action !== null}
              onClick={() => void changeLifecycle("pause")}
            >
              {action === "pause" ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Pause className="size-4" />
              )}
              {t("pause")}
            </Button>
          )}
          {canResume && (
            <Button
              className="min-h-11 gap-2"
              disabled={action !== null}
              onClick={() => void changeLifecycle("resume")}
            >
              {action === "resume" ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Play className="size-4" />
              )}
              {t("resume")}
            </Button>
          )}
          {canCancel && (
            <Button
              variant="destructive"
              className="min-h-11 gap-2"
              disabled={action !== null}
              onClick={() => void changeLifecycle("cancel")}
            >
              {action === "cancel" ? (
                <LoaderCircle className="size-4 animate-spin" />
              ) : (
                <Square className="size-4" />
              )}
              {t("cancelRun")}
            </Button>
          )}
        </div>
      </div>

      {loadError && (
        <div
          className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/40 p-3 text-sm"
          role="alert"
        >
          <span className="min-w-0 flex-1">
            {t("refreshFailed", { error: loadError })}
          </span>
          <Button
            variant="outline"
            className="min-h-11"
            onClick={() => void load()}
          >
            {t("retry")}
          </Button>
        </div>
      )}

      {waitingNodes.map((n) => (
        <div
          key={n.id}
          className="rounded-lg border border-amber-500/40 bg-amber-500/5 p-4"
        >
          <div className="flex items-start gap-3">
            <ShieldQuestion className="mt-0.5 size-5 shrink-0 text-amber-600 dark:text-amber-400" />
            <div className="min-w-0 flex-1">
              <p className="font-medium">
                {t("approvalWaiting", { id: n.id })}
              </p>
              <p className="mt-0.5 text-sm text-muted-foreground">
                {t("approvalHint")}
              </p>
              <div className="mt-3 flex gap-2">
                <Button
                  className="min-h-11 cursor-pointer gap-1.5"
                  disabled={approvalBusy !== null}
                  onClick={() => void approveNode(n.id)}
                >
                  {approvalBusy === n.id ? (
                    <LoaderCircle className="size-4 animate-spin" />
                  ) : (
                    <Check className="size-4" />
                  )}
                  {approvalBusy === n.id ? t("submitting") : t("approve")}
                </Button>
                <Button
                  variant="outline"
                  className="min-h-11 cursor-pointer gap-1.5"
                  disabled={approvalBusy !== null}
                  onClick={() => {
                    setReject({ nodeId: n.id });
                    setFbError("");
                  }}
                >
                  <X className="size-4" />
                  {t("reject")}
                </Button>
              </div>
            </div>
          </div>
        </div>
      ))}

      <Dialog open={!!reject} onOpenChange={(v) => !v && setReject(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("rejectTitle")}</DialogTitle>
          </DialogHeader>
          <div className="grid gap-3 py-2">
            <div className="grid gap-1.5">
              <label htmlFor="fb-cat" className="text-sm font-medium">
                {t("category")} <span className="text-destructive">*</span>
              </label>
              <select
                id="fb-cat"
                value={fb.category}
                onChange={(e) =>
                  setFb({ ...fb, category: e.target.value as FeedbackCategory })
                }
                className="min-h-11 cursor-pointer rounded-md border border-input bg-background px-3 text-sm"
              >
                {FEEDBACK_CATEGORIES.map((c) => (
                  <option key={c} value={c}>
                    {t(`categories.${c}`)}
                  </option>
                ))}
              </select>
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-loc" className="text-sm font-medium">
                {t("location")}
              </label>
              <Input
                id="fb-loc"
                value={fb.location}
                onChange={(e) => setFb({ ...fb, location: e.target.value })}
                className="min-h-11"
                placeholder={t("locationPlaceholder")}
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-exp" className="text-sm font-medium">
                {t("expected")}
              </label>
              <Input
                id="fb-exp"
                value={fb.expected}
                onChange={(e) => setFb({ ...fb, expected: e.target.value })}
                className="min-h-11"
                placeholder={t("expectedPlaceholder")}
              />
            </div>
            <div className="grid gap-1.5">
              <label htmlFor="fb-detail" className="text-sm font-medium">
                {t("detail")} <span className="text-destructive">*</span>
              </label>
              <Textarea
                id="fb-detail"
                rows={3}
                value={fb.detail}
                onChange={(e) => setFb({ ...fb, detail: e.target.value })}
                placeholder={t("detailPlaceholder")}
              />
            </div>
            {fbError && (
              <p
                className="text-sm text-destructive"
                role="alert"
                aria-live="assertive"
              >
                {fbError}
              </p>
            )}
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              className="min-h-11 cursor-pointer"
              onClick={() => setReject(null)}
            >
              {t("cancel")}
            </Button>
            <Button
              variant="destructive"
              className="min-h-11 cursor-pointer"
              onClick={() => void submitRejection()}
              disabled={!fb.detail.trim() || approvalBusy !== null}
            >
              {approvalBusy ? t("submitting") : t("submitReject")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <div className="h-[min(520px,60dvh)] min-h-80 w-full overflow-hidden rounded-md border">
        <ReactFlow
          nodes={flowNodes}
          edges={edges}
          nodeTypes={{ flow: FlowNode }}
          fitView
        >
          <Background />
          <Controls />
        </ReactFlow>
      </div>
    </div>
  );
}
