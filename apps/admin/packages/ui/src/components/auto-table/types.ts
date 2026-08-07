import type { ComponentType, ReactNode } from "react"

import type { Evaluable } from "../auto-form/types"
import type { StickyConfig } from "../table-sticky"

export type { Evaluable } from "../auto-form/types"
export type { StickyConfig } from "../table-sticky"

export interface ColumnConfig<T = unknown> {
  field: keyof T | string
  name: Evaluable<ReactNode>
  type?: "text" | "image" | "icon" | "qrcode" | "area" | "actions" | "custom"
  format?: (value: unknown, row: T) => ReactNode
  color?: string | ((value: unknown, row: T) => string | undefined)
  sortable?: boolean
  width?: string
  align?: "left" | "center" | "right"
  onclick?: (row: T, event: React.MouseEvent) => false | void
  render?: (
    value: unknown,
    row: T,
    ctx: { column: ColumnConfig<T> }
  ) => ReactNode
}

export interface TableAction<T = unknown> {
  name: Evaluable<ReactNode>
  icon?: ComponentType<{ className?: string }>
  color?: string
  onclick?: (row: T) => void
  disabled?: (row: T) => boolean
  show?: (row: T) => boolean
}

export interface TableQuery {
  page: number
  pageSize: number
  sort?: { field: string; direction: "asc" | "desc" }
  filter?: Record<string, unknown>
}

export interface TableOptions<T = unknown> {
  data?: T[]
  fetch?: (query: TableQuery) => Promise<{ rows: T[]; total: number }>
  columns: ColumnConfig<T>[]
  actions?: TableAction<T>[]
  rowKey?: keyof T | ((row: T) => string)
  selectable?: boolean
  selection?: "single" | "multiple"
  pageSize?: number
  pageSizeOptions?: number[]
  defaultSort?: { field: string; direction: "asc" | "desc" }
  filter?: Record<string, unknown>
  title?: ReactNode
  onRowClick?: (row: T) => void
  onSelectionChange?: (rows: T[]) => void
  onQueryChange?: (query: TableQuery) => void
  className?: string
  loading?: boolean
  /** Sticky columns/header/pagination config. */
  sticky?: StickyConfig
}

export interface UseAutoTableReturn<T> {
  rows: T[]
  total: number
  loading: boolean
  error: Error | null
  query: TableQuery
  selected: T[]
  setPage: (page: number) => void
  setPageSize: (size: number) => void
  setSort: (field: string) => void
  setFilter: (filter: Record<string, unknown>) => void
  toggleSelect: (row: T) => void
  selectAll: (rows: T[]) => void
  refresh: () => void
}

export function getRowKey<T>(
  row: T,
  rowKey?: keyof T | ((row: T) => string)
): string {
  if (!rowKey) return (row as { id?: string })?.id ?? String(row)
  if (typeof rowKey === "function") return rowKey(row)
  return String(row[rowKey])
}

export function formatCellValue<T>(row: T, column: ColumnConfig<T>): ReactNode {
  const raw = (row as Record<string, unknown>)[column.field as string]
  if (column.format) return column.format(raw, row)
  return raw as ReactNode
}
