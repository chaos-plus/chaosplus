import { useCallback, useEffect, useRef, useState } from "react"

import {
  getRowKey,
  type TableOptions,
  type TableQuery,
  type UseAutoTableReturn,
} from "./types"

const DEFAULT_PAGE_SIZE = 20
const DEFAULT_PAGE_SIZE_OPTIONS = [10, 20, 50, 100]

function sortLocalData<T>(rows: T[], sort: TableQuery["sort"]): T[] {
  if (!sort) return rows
  const { field, direction } = sort
  const multiplier = direction === "asc" ? 1 : -1
  return [...rows].sort((a, b) => {
    const av = (a as Record<string, unknown>)[field]
    const bv = (b as Record<string, unknown>)[field]
    if (av == null && bv == null) return 0
    if (av == null) return 1 * multiplier
    if (bv == null) return -1 * multiplier
    if (typeof av === "number" && typeof bv === "number") {
      return (av - bv) * multiplier
    }
    return String(av).localeCompare(String(bv)) * multiplier
  })
}

export function useAutoTable<T>(
  options: TableOptions<T>
): UseAutoTableReturn<T> {
  const {
    data,
    fetch,
    selection = "multiple",
    pageSize = DEFAULT_PAGE_SIZE,
    pageSizeOptions = DEFAULT_PAGE_SIZE_OPTIONS,
    defaultSort,
    filter,
    onSelectionChange,
    onQueryChange,
  } = options

  const [query, setQuery] = useState<TableQuery>({
    page: 1,
    pageSize: pageSizeOptions.includes(pageSize)
      ? pageSize
      : (pageSizeOptions[0] ?? DEFAULT_PAGE_SIZE),
    sort: defaultSort
      ? { field: defaultSort.field, direction: defaultSort.direction }
      : undefined,
    filter: filter ?? {},
  })

  const [rows, setRows] = useState<T[]>(data ?? [])
  const [total, setTotal] = useState(data?.length ?? 0)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<Error | null>(null)
  const [selected, setSelected] = useState<T[]>([])
  const fetchSeq = useRef(0)

  const isLocal = data !== undefined

  const fetchData = useCallback(async () => {
    if (isLocal) {
      // Local mode: sort then slice to the current page; total is the full length (only pagination is real)
      const sorted = sortLocalData(data, query.sort)
      const start = (query.page - 1) * query.pageSize
      setRows(sorted.slice(start, start + query.pageSize))
      setTotal(sorted.length)
      return
    }
    if (!fetch) return
    const seq = fetchSeq.current + 1
    fetchSeq.current = seq
    setLoading(true)
    setError(null)
    try {
      const res = await fetch(query)
      if (fetchSeq.current !== seq) return
      setRows(res.rows)
      setTotal(res.total)
    } catch (err) {
      if (fetchSeq.current !== seq) return
      setError(err instanceof Error ? err : new Error(String(err)))
    } finally {
      if (fetchSeq.current === seq) {
        setLoading(false)
      }
    }
  }, [data, fetch, isLocal, query])

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void fetchData()
  }, [fetchData])

  useEffect(() => {
    onQueryChange?.(query)
  }, [query, onQueryChange])

  const setPage = useCallback((page: number) => {
    setQuery((prev) => ({ ...prev, page: Math.max(1, page) }))
  }, [])

  const setPageSize = useCallback((size: number) => {
    setQuery((prev) => ({ ...prev, pageSize: size, page: 1 }))
  }, [])

  const setSort = useCallback((field: string) => {
    setQuery((prev) => {
      const current = prev.sort
      let nextSort: TableQuery["sort"]
      if (current?.field === field) {
        if (current.direction === "asc") {
          nextSort = { field, direction: "desc" }
        } else {
          nextSort = undefined
        }
      } else {
        nextSort = { field, direction: "asc" }
      }
      return { ...prev, sort: nextSort, page: 1 }
    })
  }, [])

  const setFilter = useCallback((nextFilter: Record<string, unknown>) => {
    setQuery((prev) => ({ ...prev, filter: nextFilter, page: 1 }))
  }, [])

  const refresh = useCallback(() => {
    void fetchData()
  }, [fetchData])

  const toggleSelect = useCallback(
    (row: T) => {
      setSelected((prev) => {
        const key = getRowKey(row, options.rowKey)
        const exists = prev.some((r) => getRowKey(r, options.rowKey) === key)
        const next =
          selection === "single"
            ? exists
              ? []
              : [row]
            : exists
              ? prev.filter((r) => getRowKey(r, options.rowKey) !== key)
              : [...prev, row]
        onSelectionChange?.(next)
        return next
      })
    },
    [onSelectionChange, options.rowKey, selection]
  )

  const selectAll = useCallback(
    (pageRows: T[]) => {
      setSelected((prev) => {
        const keys = new Set(prev.map((r) => getRowKey(r, options.rowKey)))
        const allSelected = pageRows.every((r) =>
          keys.has(getRowKey(r, options.rowKey))
        )
        const next = allSelected
          ? prev.filter(
              (r) =>
                !pageRows.some(
                  (pr) =>
                    getRowKey(pr, options.rowKey) ===
                    getRowKey(r, options.rowKey)
                )
            )
          : [
              ...prev,
              ...pageRows.filter(
                (r) => !keys.has(getRowKey(r, options.rowKey))
              ),
            ]
        onSelectionChange?.(next)
        return next
      })
    },
    [onSelectionChange, options.rowKey]
  )

  return {
    rows,
    total,
    loading,
    error,
    query,
    selected,
    setPage,
    setPageSize,
    setSort,
    setFilter,
    toggleSelect,
    selectAll,
    refresh,
  }
}
