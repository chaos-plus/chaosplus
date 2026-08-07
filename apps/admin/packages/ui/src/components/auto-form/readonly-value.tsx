import { ChevronDown } from "lucide-react"

import type { FieldComponentConfig, FieldType } from "./types"

// Select-style types keep a (decorative) chevron in read-only mode so the box still reads
// as "this was a dropdown"; plain inputs (text/number/date…) render without one.
const CHEVRON_TYPES = new Set<FieldType>([
  "select",
  "multi-select",
  "bool-select",
])

// Field types rendered as a read-only value block in detail mode (per 3.DESIGN.md
// "detail = 只读块、非输入框"). Complex types (remote-select / image / rich-text / code /
// custom / relation-list / location / tabs) keep their own renderer instead.
const BLOCK_TYPES = new Set<FieldType>([
  "text",
  "textarea",
  "number",
  "password",
  "email",
  "select",
  "multi-select",
  "checkbox",
  "checkbox-group",
  "switch",
  "radio",
  "date",
  "dateRange",
  "datetime",
  "datetimeRange",
  "time",
  "timeRange",
  "price",
  "bool-select",
])

export function isReadonlyBlockType(type: FieldType): boolean {
  return BLOCK_TYPES.has(type)
}

function optionLabel(config: FieldComponentConfig, v: unknown): string {
  const opts = config.options as Record<string, string> | undefined
  const label = opts?.[String(v)]
  return label != null ? label : String(v)
}

/** Renders a field's value as a plain read-only block (normal contrast, selectable). */
export function ReadonlyValue({
  type,
  config,
  value,
}: {
  type: FieldType
  config: FieldComponentConfig
  value: unknown
}) {
  let text: string
  switch (type) {
    case "price": {
      const n = Number(value)
      text =
        Number.isFinite(n) && value != null && value !== ""
          ? (n / 100).toFixed(config.decimals ?? 2)
          : ""
      break
    }
    case "switch":
    case "checkbox":
      text = value ? "✓" : "—"
      break
    case "bool-select":
      text =
        value === true
          ? String(config.trueLabel ?? "✓")
          : value === false
            ? String(config.falseLabel ?? "—")
            : ""
      break
    case "select":
    case "radio":
      text = value == null || value === "" ? "" : optionLabel(config, value)
      break
    case "multi-select":
    case "checkbox-group":
      text = (Array.isArray(value) ? value : [])
        .map((v) => optionLabel(config, v))
        .join("、")
      break
    default:
      text = value == null ? "" : String(value)
  }
  return <ReadonlyBox text={text} chevron={CHEVRON_TYPES.has(type)} />
}

/** Boxed read-only display: keeps the input's box shape (so detail rows align with edit
 * rows) but uses a dashed border + muted fill to signal read-only, text at normal contrast.
 * `chevron` adds a decorative ▼ for select-style fields. */
export function ReadonlyBox({
  text,
  chevron,
}: {
  text: string
  chevron?: boolean
}) {
  return (
    <div className="flex min-h-9 w-full items-center rounded-md border border-dashed bg-muted/30 px-3 py-2 text-sm break-words whitespace-pre-wrap">
      <span className="min-w-0 flex-1">
        {text !== "" ? text : <span className="text-muted-foreground">—</span>}
      </span>
      {chevron ? (
        <ChevronDown className="ms-2 size-4 shrink-0 text-muted-foreground" />
      ) : null}
    </div>
  )
}
