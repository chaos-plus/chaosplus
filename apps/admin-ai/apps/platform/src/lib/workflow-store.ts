
export interface SavedWorkflow {
  id: string;
  name: string;
  version?: string;
  def: unknown;
  updatedAt: string;
}

/** List workflows from the server. Falls back to empty list on error. */
export async function loadWorkflows(): Promise<SavedWorkflow[]> {
  try {
    const resp = await fetch("/api/workflows", { headers: { Accept: "application/json" } });
    if (!resp.ok) return [];
    const list = (await resp.json()) as Array<{
      id: string; version: string; name: string; def: unknown; updatedAt: string;
    }>;
    return list.map((w) => ({ id: w.id, name: w.name, version: w.version, def: w.def, updatedAt: w.updatedAt }));
  } catch {
    return [];
  }
}

/** Save a workflow to the server. Throws on non-2xx response. */
export async function saveWorkflow(wf: SavedWorkflow): Promise<void> {
  const resp = await fetch("/api/workflows", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ id: wf.id, version: wf.version ?? "1", name: wf.name, def: wf.def }),
  });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(`保存失败 (${resp.status}): ${body}`);
  }
}

/** Delete a workflow from the server. Throws on non-2xx response. */
export async function deleteWorkflow(id: string): Promise<void> {
  const resp = await fetch(`/api/workflows/${encodeURIComponent(id)}`, { method: "DELETE" });
  if (!resp.ok) {
    const body = await resp.text();
    throw new Error(`删除失败 (${resp.status}): ${body}`);
  }
}
