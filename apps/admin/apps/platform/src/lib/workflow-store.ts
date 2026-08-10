const KEY = "chaosplus-workflows";

export interface SavedWorkflow {
  id: string;
  name: string;
  def: unknown; // WorkflowDef JSON
  updatedAt: string;
}

export function loadWorkflows(): SavedWorkflow[] {
  try { return JSON.parse(localStorage.getItem(KEY) ?? "[]"); } catch { return []; }
}

export function saveWorkflow(wf: SavedWorkflow): void {
  const list = loadWorkflows().filter((w) => w.id !== wf.id);
  list.push(wf);
  localStorage.setItem(KEY, JSON.stringify(list));
}

export function deleteWorkflow(id: string): void {
  localStorage.setItem(KEY, JSON.stringify(loadWorkflows().filter((w) => w.id !== id)));
}

export function getWorkflow(id: string): SavedWorkflow | undefined {
  return loadWorkflows().find((w) => w.id === id);
}
