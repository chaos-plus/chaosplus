/* eslint-disable react-hooks/set-state-in-effect */
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router";
import { Button } from "@workspace/ui/components/button";
import { Card, CardContent } from "@workspace/ui/components/card";
import { Badge } from "@workspace/ui/components/badge";
import { deleteWorkflow, loadWorkflows, type SavedWorkflow } from "../../../lib/workflow-store";

export default function WorkflowListPage() {
  const navigate = useNavigate();
  const [workflows, setWorkflows] = useState<SavedWorkflow[]>([]);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    setLoading(true);
    const list = await loadWorkflows();
    setWorkflows(list);
    setLoading(false);
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  const remove = async (id: string) => {
    await deleteWorkflow(id);
    await refresh();
  };

  return (
    <div className="space-y-4 p-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-xl font-semibold">工作流</h1>
          <p className="text-sm text-muted-foreground">服务端持久化 · 多机器协同 · 入参出参校验</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={() => navigate("/workflow/runs")}>JSON 直写</Button>
          <Button onClick={() => navigate("/workflow/editor")}>可视化编辑器</Button>
        </div>
      </div>

      {loading ? (
        <p className="text-sm text-muted-foreground">加载中...</p>
      ) : workflows.length === 0 ? (
        <Card>
          <CardContent className="pt-6 text-center text-sm text-muted-foreground">
            还没有保存的工作流。点击「可视化编辑器」创建第一个，或在「JSON 直写」粘贴 WorkflowDef JSON。
          </CardContent>
        </Card>
      ) : (
        <div className="space-y-2">
          {workflows.map((wf) => (
            <Card key={wf.id} className="p-3 text-sm">
              <div className="flex items-center gap-2">
                <span className="font-medium">{wf.name}</span>
                {wf.version && <Badge variant="outline">v{wf.version}</Badge>}
                <Badge variant="secondary">{((wf.def as { nodes?: unknown[] } | null)?.nodes?.length) ?? 0} 节点</Badge>
                <span className="text-muted-foreground text-xs">{wf.updatedAt?.slice(0, 16)?.replace("T", " ") ?? ""}</span>
                <div className="ml-auto flex gap-1">
                  <Button size="sm" variant="outline" onClick={() => navigate(`/workflow/editor?id=${wf.id}`)}>编辑</Button>
                  <Button size="sm" variant="destructive" onClick={() => remove(wf.id)}>删除</Button>
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}
    </div>
  );
}
