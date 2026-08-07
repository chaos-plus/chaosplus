import { useTranslations } from "use-intl"

import { ChevronLeft, ChevronRight } from "lucide-react"

import { cn } from "@workspace/ui/lib/utils"

import { evaluate } from "../auto-form"
import { Button } from "../button"
import { Checkbox } from "../checkbox"
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
} from "../table-sticky"
import { Textarea } from "../textarea"
import {
  formatCellValue,
  getRowKey,
  type ColumnConfig,
  type TableAction,
  type TableOptions,
} from "./types"
import { useAutoTable } from "./use-auto-table"

function SortIndicator<T>({
  column,
  query,
}: {
  column: ColumnConfig<T>
  query: { sort?: { field: string; direction: "asc" | "desc" } }
}) {
  if (!column.sortable) return null
  const active = query.sort?.field === (column.field as string)
  return (
    <span className="ml-1 inline-flex flex-col text-[10px] leading-none text-muted-foreground">
      <span
        className={
          active && query.sort?.direction === "asc" ? "text-foreground" : ""
        }
      >
        ▲
      </span>
      <span
        className={
          active && query.sort?.direction === "desc" ? "text-foreground" : ""
        }
      >
        ▼
      </span>
    </span>
  )
}

function ActionButton<T>({ action, row }: { action: TableAction<T>; row: T }) {
  const visible = action.show?.(row) !== false
  const disabled = action.disabled?.(row) === true
  const Icon = action.icon
  const label = evaluate(action.name)

  if (!visible) return null

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-xs"
      disabled={disabled}
      title={typeof label === "string" ? label : undefined}
      onClick={(e) => {
        e.stopPropagation()
        action.onclick?.(row)
      }}
      aria-label={typeof label === "string" ? label : undefined}
    >
      {Icon ? <Icon className="size-4" /> : label}
    </Button>
  )
}

export function AutoTable<T>(options: TableOptions<T>) {
  const t = useTranslations("table")
  const table = useAutoTable(options)
  const {
    rows,
    total,
    loading,
    query,
    selected,
    setPage,
    setPageSize,
    setSort,
    toggleSelect,
    selectAll,
  } = table

  const displayRows = rows
  const totalPages = Math.max(1, Math.ceil(total / query.pageSize))

  // Sticky table geometry: column order is [selection?, ...data, actions?].
  const sticky = options.sticky
  const selOffset = options.selectable ? 1 : 0
  const hasActions = !!options.actions?.length
  const totalCols = selOffset + options.columns.length + (hasActions ? 1 : 0)
  const stickyWidths = [
    ...(options.selectable ? [40] : []),
    ...options.columns.map((c) => stickyColWidth(c.width)),
    ...(hasActions ? [40] : []),
  ]
  const headSticky = (i: number, width?: string) => ({
    width,
    ...stickyCellStyle(i, totalCols, stickyWidths, sticky, true),
  })
  const cellSticky = (i: number) =>
    stickyCellStyle(i, totalCols, stickyWidths, sticky, false)
  const stickyBg = (i: number, header = false) =>
    (header && sticky?.header) || isStickyColumn(i, totalCols, sticky)
      ? "bg-background"
      : ""

  const allPageSelected =
    displayRows.length > 0 &&
    displayRows.every((row) =>
      selected.some(
        (r) => getRowKey(r, options.rowKey) === getRowKey(row, options.rowKey)
      )
    )

  const anyPageSelected = displayRows.some((row) =>
    selected.some(
      (r) => getRowKey(r, options.rowKey) === getRowKey(row, options.rowKey)
    )
  )

  const handleHeaderCheckbox = () => {
    if (!options.selectable) return
    selectAll(displayRows)
  }

  const handleRowClick = (row: T) => {
    options.onRowClick?.(row)
  }

  const handleRowKeyDown = (
    event: React.KeyboardEvent<HTMLTableRowElement>,
    row: T
  ) => {
    if (!options.onRowClick) return
    if (event.key !== "Enter" && event.key !== " ") return
    event.preventDefault()
    handleRowClick(row)
  }

  return (
    <div className={options.className}>
      {options.title ? (
        <div className="mb-3 flex items-center justify-between">
          {options.title}
        </div>
      ) : null}

      <Table
        containerClassName="rounded-md border"
        containerStyle={{ maxHeight: sticky?.maxHeight }}
      >
        <TableHeader>
          <TableRow className="bg-muted/50">
            {options.selectable ? (
              <TableHead
                className={cn("w-10", stickyBg(0, true))}
                style={headSticky(0)}
              >
                <Checkbox
                  checked={allPageSelected}
                  indeterminate={!allPageSelected && anyPageSelected}
                  onCheckedChange={handleHeaderCheckbox}
                  aria-label={t("selectAll")}
                />
              </TableHead>
            ) : null}
            {options.columns.map((column, ci) => {
              const field = column.field as string
              const sorted =
                query.sort?.field === field
                  ? query.sort.direction === "asc"
                    ? "ascending"
                    : "descending"
                  : undefined

              return (
                <TableHead
                  key={String(column.field)}
                  className={cn(
                    column.sortable ? "select-none" : undefined,
                    stickyBg(selOffset + ci, true)
                  )}
                  style={headSticky(selOffset + ci, column.width)}
                  aria-sort={sorted}
                >
                  {column.sortable ? (
                    <button
                      type="button"
                      className="flex items-center text-left whitespace-nowrap hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
                      onClick={() => setSort(field)}
                    >
                      <span>{evaluate(column.name)}</span>
                      <SortIndicator column={column} query={query} />
                    </button>
                  ) : (
                    <span className="whitespace-nowrap">
                      {evaluate(column.name)}
                    </span>
                  )}
                </TableHead>
              )
            })}
            {hasActions ? (
              <TableHead
                className={cn(
                  "w-10 text-right",
                  stickyBg(selOffset + options.columns.length, true)
                )}
                style={headSticky(selOffset + options.columns.length)}
              >
                {t("actions")}
              </TableHead>
            ) : null}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell
                colSpan={
                  options.columns.length +
                  (options.selectable ? 1 : 0) +
                  (options.actions?.length ? 1 : 0)
                }
              >
                <div className="space-y-2 py-4">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <div
                      key={i}
                      className="h-8 w-full animate-pulse rounded bg-muted"
                    />
                  ))}
                </div>
              </TableCell>
            </TableRow>
          ) : displayRows.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={
                  options.columns.length +
                  (options.selectable ? 1 : 0) +
                  (options.actions?.length ? 1 : 0)
                }
                className="h-24 text-center text-muted-foreground"
              >
                {t("empty")}
              </TableCell>
            </TableRow>
          ) : (
            displayRows.map((row) => {
              const rowKey = getRowKey(row, options.rowKey)
              const isSelected = selected.some(
                (r) => getRowKey(r, options.rowKey) === rowKey
              )
              return (
                <TableRow
                  key={rowKey}
                  data-selected={isSelected}
                  onClick={
                    options.onRowClick ? () => handleRowClick(row) : undefined
                  }
                  onKeyDown={(event) => handleRowKeyDown(event, row)}
                  tabIndex={options.onRowClick ? 0 : undefined}
                  role={options.onRowClick ? "button" : undefined}
                  className={
                    options.onRowClick
                      ? "cursor-pointer focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:outline-none"
                      : ""
                  }
                >
                  {options.selectable ? (
                    <TableCell
                      className={cn("w-10", stickyBg(0))}
                      style={cellSticky(0)}
                    >
                      <Checkbox
                        checked={isSelected}
                        onCheckedChange={() => toggleSelect(row)}
                        onClick={(e) => e.stopPropagation()}
                        aria-label={t("selectRow")}
                      />
                    </TableCell>
                  ) : null}
                  {options.columns.map((column, ci) => (
                    <TableCell
                      key={`${rowKey}-${String(column.field)}`}
                      align={column.align}
                      className={stickyBg(selOffset + ci)}
                      style={cellSticky(selOffset + ci)}
                      onClick={(e) => {
                        if (column.onclick?.(row, e) === false) {
                          e.stopPropagation()
                        }
                      }}
                    >
                      <CellContent row={row} column={column} />
                    </TableCell>
                  ))}
                  {hasActions ? (
                    <TableCell
                      className={cn(
                        "text-right",
                        stickyBg(selOffset + options.columns.length)
                      )}
                      style={cellSticky(selOffset + options.columns.length)}
                    >
                      <div className="flex items-center justify-end gap-1">
                        {options.actions!.map((action, idx) => (
                          <ActionButton key={idx} action={action} row={row} />
                        ))}
                      </div>
                    </TableCell>
                  ) : null}
                </TableRow>
              )
            })
          )}
        </TableBody>
      </Table>

      <div
        className={cn(
          "mt-3 flex flex-col items-center justify-between gap-3 sm:flex-row",
          sticky?.pagination &&
            "sticky bottom-0 z-10 border-t bg-background py-2"
        )}
      >
        <div className="text-sm text-muted-foreground">
          {t("total", { total })} /{" "}
          {t("page", { page: query.page, totalPages })}
        </div>
        <div className="flex items-center gap-2">
          <select
            value={query.pageSize}
            onChange={(e) => setPageSize(Number(e.target.value))}
            className="h-8 rounded-md border border-input bg-transparent px-2 text-sm"
            aria-label={t("pageSize")}
          >
            {(options.pageSizeOptions ?? [10, 20, 50, 100]).map((size) => (
              <option key={size} value={size}>
                {size} {t("pageSize")}
              </option>
            ))}
          </select>
          <Button
            type="button"
            variant="outline"
            size="icon-xs"
            onClick={() => setPage(query.page - 1)}
            disabled={query.page <= 1}
            aria-label={t("previousPage")}
          >
            <ChevronLeft className="size-4" />
          </Button>
          <Button
            type="button"
            variant="outline"
            size="icon-xs"
            onClick={() => setPage(query.page + 1)}
            disabled={query.page >= totalPages}
            aria-label={t("nextPage")}
          >
            <ChevronRight className="size-4" />
          </Button>
        </div>
      </div>
    </div>
  )
}

function CellContent<T>({ row, column }: { row: T; column: ColumnConfig<T> }) {
  if (column.type === "custom" && column.render) {
    return (
      <>
        {column.render(
          (row as Record<string, unknown>)[column.field as string],
          row,
          { column }
        )}
      </>
    )
  }

  if (column.type === "image") {
    const src = (row as Record<string, unknown>)[
      column.field as string
    ] as string
    if (!src) return null
    return <img src={src} alt="" className="h-11 w-11 rounded object-cover" />
  }

  if (column.type === "icon") {
    const value = (row as Record<string, unknown>)[column.field as string]
    const color =
      typeof column.color === "function"
        ? column.color(value, row)
        : column.color
    const Icon = column.format?.(value, row) as unknown as React.ComponentType<{
      className?: string
      style?: React.CSSProperties
    }>
    if (!Icon) return null
    return <Icon className="size-5" style={{ color }} />
  }

  if (column.type === "area") {
    const value = String(
      (row as Record<string, unknown>)[column.field as string] ?? ""
    )
    return (
      <Textarea
        readOnly
        value={value}
        rows={2}
        className="min-h-[60px] resize-none"
      />
    )
  }

  const color =
    typeof column.color === "function"
      ? column.color(
          (row as Record<string, unknown>)[column.field as string],
          row
        )
      : column.color

  return <span style={{ color }}>{formatCellValue(row, column)}</span>
}

export { useAutoTable }
