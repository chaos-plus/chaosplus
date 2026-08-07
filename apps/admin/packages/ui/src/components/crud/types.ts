import type { ComponentType, ReactNode } from "react"

import type {
  ApiEnvelope,
  FilterOp,
  FilterSyntax,
  FilterValue,
  ListResult,
  SortDir,
} from "@workspace/ui/lib/api-client"
import type { FormConfig } from "../auto-form/types"
import type { StickyConfig } from "../table-sticky"

export type Row = Record<string, unknown>

/**
 * FormFactory is the per-resource contract consumed by {@link CrudForm} — the React
 * equivalent of the legacy admin's `factory` object passed to `CommonForm.vue`.
 */
export interface FormFactory {
  /** Returns the form field definition (AutoForm config). */
  getFormDef: () => FormConfig
  /** Fetches one entity by id (already unwrapped to the entity object). */
  getInfo: (id: string) => Promise<Row>
  /** Server → form mapping (e.g. parse JSON columns). */
  toLocalData?: (server: Row) => Row | Promise<Row>
  /** Form → server mapping (e.g. stringify JSON columns, scale prices). */
  toServerData?: (local: Row) => Row
  commitAdd: (body: Row) => Promise<unknown>
  commitEdit: (id: string, body: Row) => Promise<unknown>
  commitCallback?: {
    onSuccess?: () => void
    onError?: (error: unknown) => void
  }
  /** When false, hides the submit/edit button (read-only resource). */
  editable?: boolean
}

export type ColumnType = "text" | "image" | "icon" | "qrcode" | "area"

export interface ColumnDef {
  field: string
  name: ReactNode | ((col: ColumnDef) => ReactNode)
  type?: ColumnType
  sortable?: boolean
  width?: string
  format?: (value: unknown, row: Row) => ReactNode
  color?: string | ((value: unknown, row: Row) => string | undefined)
  /** Cell click; return false to stop the row click from also firing. */
  onclick?: (row: Row) => boolean | void
}

export interface ActionDef {
  name?: string
  icon?: ComponentType<{ className?: string }>
  color?: string
  onclick?: (row: Row) => void
  /** Return true to disable the action for this row (matches auto-table semantics). */
  disabled?: (row: Row) => boolean
  /** Return false to hide the action for this row. */
  show?: (row: Row) => boolean
}

export interface TableOptions {
  url: string
  columns: ColumnDef[]
  actions?: ActionDef[]
  onclick?: (row: Row) => void
  /** Adapter override; defaults to the api-client's adaptList (handles both envelope shapes). */
  request?: (env: ApiEnvelope) => ListResult<Row>
  /** Static filter merged into every request. */
  filter?: Record<string, FilterValue | unknown>
  /** Default sort order. */
  order?: Record<string, SortDir>
  limit?: number
  /**
   * Filter dialect sent to the backend. `simple` (default) emits `filter[field]=op:value`;
   * `rsql` serializes the same filter to a single `filter=<expr>` (enables OR/IN/BETWEEN/grouping).
   */
  filterMode?: FilterSyntax
  /** Sticky columns/header/pagination config. */
  sticky?: StickyConfig
}

export interface FilterFieldOption {
  field: string
  name: ReactNode
  type?: "string" | "number" | "date" | "datetime" | "select" | "boolean"
  conditions?: FilterOp[]
  options?: Array<{ label: ReactNode; value: string }> | Record<string, string>
}

/** Current value of a single filter row. */
export type FilterState = Record<string, FilterValue>
