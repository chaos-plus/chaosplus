import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useState,
  type ReactNode,
} from "react"
import { ArrowDown, ArrowUpDown, ArrowUp } from "lucide-react"
import { useTranslations } from "use-intl"

import { cn } from "@workspace/ui/lib/utils"
import {
  adaptList,
  filterStateToRsql,
  type ListQuery,
  type SortDir,
} from "@workspace/ui/lib/api-client"

import { useApi } from "../api-provider"
import { Button } from "../button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "../table"
import {
  isStickyColumn,
  stickyCellStyle,
  stickyColWidth,
  type StickyConfig,
} from "../table-sticky"
import type { ActionDef, ColumnDef, Row, TableOptions } from "./types"

const ACTIONS_COL_WIDTH = 120
const SEQ_COL_WIDTH = 48 // px, used for sticky left-offset calculation

export interface CommonTableHandle {
  refresh: () => void
  exportCsv: (filename?: string) => void
}

interface SortState {
  field: string
  dir: SortDir
}

function colName(col: ColumnDef): ReactNode {
  return typeof col.name === "function" ? col.name(col) : col.name
}

function colColor(
  col: ColumnDef,
  value: unknown,
  row: Row
): string | undefined {
  return typeof col.color === "function" ? col.color(value, row) : col.color
}

function renderCell(
  col: ColumnDef,
  row: Row,
  resolveAsset: (p: string) => string
): ReactNode {
  const value = row[col.field]
  const formatted = col.format ? col.format(value, row) : (value as ReactNode)
  switch (col.type) {
    case "image": {
      const src = typeof formatted === "string" ? formatted : ""
      return src ? (
        <img
          src={resolveAsset(src)}
          alt=""
          className="size-11 rounded object-cover"
        />
      ) : null
    }
    case "area":
      return (
        <span className="line-clamp-2 max-w-xs text-xs whitespace-pre-wrap">
          {formatted}
        </span>
      )
    case "icon":
    case "qrcode":
    case "text":
    default:
      return (
        <span style={{ color: colColor(col, value, row) }}>
          {formatted as ReactNode}
        </span>
      )
  }
}

function csvCell(col: ColumnDef, row: Row): string {
  const value = row[col.field]
  const formatted = col.format ? col.format(value, row) : value
  const out =
    typeof formatted === "string" || typeof formatted === "number"
      ? formatted
      : value
  const s = out == null ? "" : String(out)
  return `"${s.split('"').join('""')}"`
}

/**
 * CommonTable renders a server-driven list from `tableOptions` (= CommonTable.vue):
 * url + RSQL simple query, sortable columns, row/cell click, an actions column, and CSV
 * export. The request() adapter normalizes the envelope, keeping the legacy
 * data.list/total/offset page contract while the backend stays native.
 */
export const CommonTable = forwardRef<
  CommonTableHandle,
  {
    tableOptions: TableOptions
    hiddenFields?: ReadonlySet<string>
    fields?: string[]
  }
>(function CommonTable({ tableOptions, hiddenFields, fields }, ref) {
  const api = useApi()
  const t = useTranslations("table")
  const limit = tableOptions.limit ?? 20
  const [rows, setRows] = useState<Row[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(0) // zero-based
  const [sort, setSort] = useState<SortState | null>(null)
  const [loading, setLoading] = useState(false)

  const filterKey = JSON.stringify(tableOptions.filter ?? {})
  const orderKey = JSON.stringify(tableOptions.order ?? {})
  const fieldsKey = fields ? fields.join(",") : ""

  const load = useCallback(async () => {
    setLoading(true)
    const order: Record<string, SortDir> = sort
      ? { [sort.field]: sort.dir }
      : (tableOptions.order ?? {})
    const query: ListQuery =
      tableOptions.filterMode === "rsql"
        ? {
            offset: page * limit,
            limit,
            order,
            syntax: "rsql",
            rsql: filterStateToRsql(tableOptions.filter),
            fields,
          }
        : {
            offset: page * limit,
            limit,
            filter: tableOptions.filter,
            order,
            fields,
          }
    try {
      const env = await api.get<unknown>(tableOptions.url, query)
      const result = tableOptions.request
        ? tableOptions.request(env)
        : adaptList<Row>(env)
      setRows(result.list)
      setTotal(result.total)
    } catch {
      setRows([])
      setTotal(0)
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    api,
    tableOptions.url,
    tableOptions.filterMode,
    page,
    limit,
    sort,
    filterKey,
    orderKey,
    fieldsKey,
  ])

  useEffect(() => {
    void load()
  }, [load])

  // reset to first page whenever the static filter changes
  useEffect(() => {
    setPage(0)
  }, [filterKey])

  const exportCsv = useCallback(
    (filename = "export.csv") => {
      const visibleCols = tableOptions.columns.filter(
        (c) => !hiddenFields?.has(c.field)
      )
      const header = visibleCols.map(
        (c) => `"${String(colName(c)).split('"').join('""')}"`
      )
      const lines = rows.map((row) =>
        visibleCols.map((c) => csvCell(c, row)).join(",")
      )
      const content = [header.join(","), ...lines].join("\r\n")
      const blob = new Blob([content], { type: "text/csv;charset=utf-8" })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = filename
      a.click()
      URL.revokeObjectURL(url)
    },
    [rows, tableOptions.columns, hiddenFields]
  )

  useImperativeHandle(ref, () => ({ refresh: () => void load(), exportCsv }), [
    load,
    exportCsv,
  ])

  const toggleSort = (field: string) => {
    setSort((prev) => {
      if (!prev || prev.field !== field) return { field, dir: "ASC" }
      if (prev.dir === "ASC") return { field, dir: "DESC" }
      return null
    })
  }

  const actions = tableOptions.actions ?? []
  const totalPages = Math.max(1, Math.ceil(total / limit))

  const cols = tableOptions.columns.filter((c) => !hiddenFields?.has(c.field))
  const hasActions = actions.length > 0
  // +1 for the leading local sequence number column
  const totalCols = 1 + cols.length + (hasActions ? 1 : 0)
  // Seq + first business column are sticky by default; callers can override via tableOptions.sticky.
  const sticky: StickyConfig = { left: 2, ...tableOptions.sticky }
  const widths = [
    SEQ_COL_WIDTH,
    ...cols.map((c) => stickyColWidth(c.width)),
    ...(hasActions ? [ACTIONS_COL_WIDTH] : []),
  ]
  // Sticky columns need a width the browser can't shrink, or their rendered size drifts from
  // the JS-computed offset and a gap opens letting scrolled content show through. A plain
  // `width` is only a hint under table-layout:auto; `min-width` + `max-width` at the same value
  // is a hard pin. Non-sticky columns stay content-sized (auto), just capped by `max-width`.
  const colStyle = (i: number, isHeader: boolean) => {
    const w = `${widths[i]}px`
    const pinned = isStickyColumn(i, totalCols, sticky)
    return {
      maxWidth: w,
      ...(pinned ? { width: w, minWidth: w } : {}),
      ...stickyCellStyle(i, totalCols, widths, sticky, isHeader),
    }
  }
  const headStyle = (i: number) => colStyle(i, true)
  const cellStyle = (i: number) => colStyle(i, false)
  // Sticky cells need an opaque, visually distinct background so scrolled content doesn't
  // bleed through and the pinned region reads as pinned.
  const stickyBg = (i: number, header = false) =>
    (header && sticky?.header) || isStickyColumn(i, totalCols, sticky)
      ? "bg-muted"
      : ""

  return (
    <div className="w-full">
      {/* border-separate + per-cell borders: collapsed borders don't follow position:sticky
            cells when scrolled, which leaves a visible seam. Columns stay table-layout:auto
            (content-sized) — only sticky columns are hard-pinned, see colStyle above. */}
      <Table
        className="border-separate border-spacing-0"
        containerClassName="rounded-md border"
        containerStyle={{ maxHeight: sticky?.maxHeight }}
      >
        <TableHeader>
          <TableRow>
            <TableHead
              className={cn(
                "truncate border-b text-center text-xs text-muted-foreground",
                stickyBg(0, true)
              )}
              style={headStyle(0)}
            >
              #
            </TableHead>
            {cols.map((col, idx) => (
              <TableHead
                key={col.field}
                className={cn("truncate border-b", stickyBg(idx + 1, true))}
                style={headStyle(idx + 1)}
              >
                {col.sortable ? (
                  <button
                    type="button"
                    onClick={() => toggleSort(col.field)}
                    className="inline-flex items-center gap-1 font-medium"
                  >
                    {colName(col)}
                    {sort?.field === col.field ? (
                      sort.dir === "ASC" ? (
                        <ArrowUp className="size-3" />
                      ) : (
                        <ArrowDown className="size-3" />
                      )
                    ) : (
                      <ArrowUpDown className="size-3 opacity-40" />
                    )}
                  </button>
                ) : (
                  colName(col)
                )}
              </TableHead>
            ))}
            {hasActions ? (
              <TableHead
                className={cn(
                  "truncate border-b text-right",
                  stickyBg(cols.length + 1, true)
                )}
                style={headStyle(cols.length + 1)}
              >
                {t("actions")}
              </TableHead>
            ) : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={totalCols}
                className="h-24 text-center text-sm text-muted-foreground"
              >
                {loading ? t("loading") : t("empty")}
              </TableCell>
            </TableRow>
          ) : (
            rows.map((row, i) => (
              <TableRow
                key={(row.id as string | number | undefined) ?? i}
                className={tableOptions.onclick ? "cursor-pointer" : undefined}
                onClick={() => tableOptions.onclick?.(row)}
              >
                <TableCell
                  className={cn(
                    "border-b text-center text-xs text-muted-foreground tabular-nums",
                    stickyBg(0)
                  )}
                  style={cellStyle(0)}
                  onClick={(e) => e.stopPropagation()}
                >
                  {page * limit + i + 1}
                </TableCell>
                {cols.map((col, idx) => (
                  <TableCell
                    key={col.field}
                    className={cn("truncate border-b", stickyBg(idx + 1))}
                    style={cellStyle(idx + 1)}
                    onClick={(e) => {
                      if (col.onclick && col.onclick(row) === false)
                        e.stopPropagation()
                    }}
                  >
                    {renderCell(col, row, api.resolveAsset)}
                  </TableCell>
                ))}
                {hasActions ? (
                  <TableCell
                    className={cn(
                      "border-b text-right",
                      stickyBg(cols.length + 1)
                    )}
                    style={cellStyle(cols.length + 1)}
                    onClick={(e) => e.stopPropagation()}
                  >
                    <div className="flex justify-end gap-1">
                      {actions.map((action: ActionDef, ai) => {
                        if (action.show && action.show(row) === false)
                          return null
                        const Icon = action.icon
                        const disabled = action.disabled
                          ? action.disabled(row)
                          : false
                        return (
                          <Button
                            key={ai}
                            type="button"
                            variant="ghost"
                            size="icon"
                            disabled={disabled}
                            title={action.name}
                            onClick={() => action.onclick?.(row)}
                          >
                            {Icon ? (
                              <Icon className="size-4" />
                            ) : (
                              (action.name ?? "·")
                            )}
                          </Button>
                        )
                      })}
                    </div>
                  </TableCell>
                ) : null}
              </TableRow>
            ))
          )}
        </TableBody>
      </Table>

      <div
        className={cn(
          "mt-3 flex items-center justify-between text-sm text-muted-foreground",
          sticky?.pagination && "sticky bottom-0 z-10 border-t bg-muted py-2"
        )}
      >
        <span>{t("total", { total })}</span>
        <div className="flex items-center gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page <= 0}
            onClick={() => setPage((p) => Math.max(0, p - 1))}
          >
            {t("prevPage")}
          </Button>
          <span>
            {page + 1} / {totalPages}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            disabled={page + 1 >= totalPages}
            onClick={() => setPage((p) => (p + 1 < totalPages ? p + 1 : p))}
          >
            {t("nextPage")}
          </Button>
        </div>
      </div>
    </div>
  )
})
