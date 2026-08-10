/**
 * Props/API description-table data for each component, rendered by the showcase page's "API" tab.
 * `descKey` points to i18n: `showcase.api.<demoId>.<propName>`.
 * Filled in component by component from P2 onward; unregistered components show "none" in the API tab.
 */
export interface PropMeta {
  name: string
  type: string
  default?: string
  descKey: string
}

export const propsMeta: Record<string, PropMeta[]> = {
  // Example (button) — the remaining components are filled in alongside their component from P2
  button: [
    {
      name: "variant",
      type: '"default" | "secondary" | "outline" | "ghost" | "destructive" | "link"',
      default: '"default"',
      descKey: "variant",
    },
    {
      name: "size",
      type: '"xs" | "sm" | "default" | "lg" | "icon"',
      default: '"default"',
      descKey: "size",
    },
    {
      name: "disabled",
      type: "boolean",
      default: "false",
      descKey: "disabled",
    },
  ],
}

export function getPropsMeta(id: string): PropMeta[] {
  return propsMeta[id] ?? []
}
