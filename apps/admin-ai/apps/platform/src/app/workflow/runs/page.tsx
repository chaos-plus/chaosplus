/* eslint-disable react-refresh/only-export-components */
import { useCallback, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { LoaderCircle, Play, RefreshCw } from "lucide-react";
import { Badge } from "@workspace/ui/components/badge";
import { Button } from "@workspace/ui/components/button";
import { Card, CardContent } from "@workspace/ui/components/card";
import { Input } from "@workspace/ui/components/input";
import { Textarea } from "@workspace/ui/components/textarea";
import { WorkflowCanvas } from "../../../components/workflow-canvas";
import { controlApi, type Run } from "../../../lib/control-api";
import { toast } from "@workspace/ui/components/sonner";
import { useTranslations } from "use-intl";

export const smokeWorkflow = {
  id: "smoke",
  version: "1",
  nodes: [
    { id: "start", type: "trigger", trigger: { source: "manual" } },
    {
      id: "w",
      type: "agent",
      agent: {
        id: "w",
        role: "pm",
        executor: "claude",
        systemPrompt:
          "你是测试 agent,完成任务后把结论写入 output.json(单个 JSON 对象,含 ok 与 summary)。",
      },
    },
    {
      id: "gate",
      type: "human_approval",
      humanApproval: {
        approvers: "any_human",
        timeoutMs: 600000,
        onTimeout: "pause",
        onReject: "pause",
      },
    },
  ],
  edges: [
    { from: "start", to: "w" },
    { from: "w", to: "gate" },
  ],
};

export default function RunsPage() {
  const t = useTranslations("platform.runs");
  const navigate = useNavigate();
  const [runs, setRuns] = useState<Run[]>([]);
  // 默认不预填示例,避免用户以为是自己的数据;要演示可点「载入示例」。
  const [workflowJSON, setWorkflowJSON] = useState("");
  const [workspace, setWorkspace] = useState("");
  const [busy, setBusy] = useState(false);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState(false);

  // 实时解析工作流 JSON,渲染 React Flow 预览。
  let preview: {
    nodes: { id: string; type?: string }[];
    edges: { from: string; to: string; condition?: string }[];
  } | null = null;
  try {
    const def = JSON.parse(workflowJSON) as {
      nodes?: { id: string; type?: string }[];
      edges?: { from: string; to: string; condition?: string }[];
    };
    if (Array.isArray(def.nodes))
      preview = { nodes: def.nodes, edges: def.edges ?? [] };
  } catch {
    preview = null;
  }

  const load = useCallback(() => {
    void controlApi
      .runs()
      .then((items) => {
        setRuns(items);
        setLoadError(false);
      })
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => {
    load();
    const t = setInterval(load, 3000);
    return () => clearInterval(t);
  }, [load]);

  const launch = async () => {
    setBusy(true);
    try {
      let def: unknown;
      try {
        def = JSON.parse(workflowJSON);
      } catch {
        toast.error(t("invalidJson"));
        return;
      }
      const r = await controlApi.launchRun(def, workspace.trim());
      navigate(`/workflow/runs/${r.runId}`);
    } catch (cause) {
      toast.error(
        t("launchFailed", {
          error: cause instanceof Error ? cause.message : String(cause),
        }),
      );
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="space-y-4">
      <h1 className="text-xl font-semibold">{t("title")}</h1>

      <Card>
        <CardContent className="grid gap-3 pt-4">
          <label htmlFor="workflow-json" className="text-sm font-medium">
            WorkflowDef JSON
          </label>
          <Textarea
            id="workflow-json"
            rows={8}
            className="font-mono text-xs"
            value={workflowJSON}
            onChange={(e) => setWorkflowJSON(e.target.value)}
          />
          <label htmlFor="workflow-workspace" className="text-sm font-medium">
            {t("workspace")}
          </label>
          <Input
            id="workflow-workspace"
            value={workspace}
            onChange={(e) => setWorkspace(e.target.value)}
          />
          <Button
            className="min-h-11 gap-2"
            onClick={launch}
            disabled={busy || !workflowJSON.trim()}
          >
            {busy ? (
              <LoaderCircle className="size-4 animate-spin" />
            ) : (
              <Play className="size-4" />
            )}
            {t("launch")}
          </Button>
        </CardContent>
      </Card>

      {preview && (
        <div>
          <h2 className="mb-2 text-sm font-semibold text-muted-foreground">
            {t("preview")}
          </h2>
          <WorkflowCanvas
            nodes={preview.nodes}
            edges={preview.edges}
            height={420}
          />
        </div>
      )}

      <div className="space-y-2">
        {loading && (
          <div
            className="flex min-h-28 items-center justify-center gap-2 text-sm text-muted-foreground"
            role="status"
          >
            <LoaderCircle className="size-4 animate-spin" />
            {t("loading")}
          </div>
        )}
        {!loading && loadError && (
          <div
            className="flex min-h-28 flex-col items-center justify-center gap-3 rounded-md border border-dashed text-sm text-muted-foreground"
            role="alert"
          >
            <span>{t("loadFailed")}</span>
            <Button variant="outline" className="min-h-11 gap-2" onClick={load}>
              <RefreshCw className="size-4" />
              {t("retry")}
            </Button>
          </div>
        )}
        {!loading &&
          !loadError &&
          runs.map((r) => (
            <Link
              key={r.id}
              to={`/workflow/runs/${r.id}`}
              className="block rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
            >
              <Card className="p-3 text-sm transition-colors hover:bg-accent">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{r.id}</span>
                  <Badge
                    variant={
                      r.status === "completed"
                        ? "default"
                        : r.status === "failed"
                          ? "destructive"
                          : "secondary"
                    }
                  >
                    {r.status}
                  </Badge>
                  <span className="w-full text-muted-foreground sm:ml-auto sm:w-auto">
                    {t("nodeCount", { count: r.nodes })} · {r.createdAt}
                  </span>
                </div>
              </Card>
            </Link>
          ))}
        {!loading && !loadError && runs.length === 0 && (
          <div className="grid min-h-28 place-items-center rounded-md border border-dashed px-4 text-center text-sm text-muted-foreground">
            {t("empty")}
          </div>
        )}
      </div>
    </div>
  );
}
