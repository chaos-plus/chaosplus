import type { CSSProperties } from "react"

/**
 * Shared sticky-table configuration + cell-style computation, used by both CommonTable and
 * AutoTable. Left/right columns are pinned horizontally, the header row vertically, and the
 * pagination bar to the bottom. Sticky columns need a width (left/right offsets are summed
 * from column widths); a fallback is used when a width is missing.
 */
export interface StickyConfig {
  /** Pin the header row to the top (requires `maxHeight` for a vertical scroll). */
  header?: boolean
  /** Number of leftmost columns to pin. */
  left?: number
  /** Number of rightmost columns to pin (the actions column counts as the last column). */
  right?: number
  /** Pin the pagination bar to the bottom of the table block. */
  pagination?: boolean
  /** Scroll-container max height (e.g. '70vh', 480) — enables vertical scrolling + sticky header. */
  maxHeight?: string | number
}

const FALLBACK_WIDTH = 150

export function stickyColWidth(width?: string | number): number {
  if (typeof width === "number") return width
  if (typeof width === "string") {
    const n = parseFloat(width)
    if (Number.isFinite(n)) return n
  }
  return FALLBACK_WIDTH
}

// z-index layers: body sticky col < header row < header sticky col (corner)
const Z_COL = 10
const Z_HEADER = 20
const Z_CORNER = 30

/**
 * Computes the sticky style for a cell at `index` of `total` columns.
 * `widths` is the resolved px width per column. `isHeader` raises the z-index so the header
 * sits above sticky body columns and corner cells above both.
 */
export function stickyCellStyle(
  index: number,
  total: number,
  widths: number[],
  cfg: StickyConfig | undefined,
  isHeader = false
): CSSProperties | undefined {
  if (!cfg) return undefined
  const left = cfg.left ?? 0
  const right = cfg.right ?? 0
  const headerSticky = isHeader && cfg.header

  let colStyle: CSSProperties | undefined
  if (index < left) {
    let offset = 0
    for (let i = 0; i < index; i++) offset += widths[i] ?? FALLBACK_WIDTH
    colStyle = {
      position: "sticky",
      left: offset,
      zIndex: isHeader ? Z_CORNER : Z_COL,
    }
  } else if (right > 0 && index >= total - right) {
    let offset = 0
    for (let i = index + 1; i < total; i++)
      offset += widths[i] ?? FALLBACK_WIDTH
    colStyle = {
      position: "sticky",
      right: offset,
      zIndex: isHeader ? Z_CORNER : Z_COL,
    }
  }

  if (headerSticky) {
    return {
      position: "sticky",
      top: 0,
      ...colStyle,
      zIndex: colStyle ? Z_CORNER : Z_HEADER,
    }
  }
  return colStyle
}

/** True when the cell at `index` is pinned (left or right) and therefore needs an opaque bg. */
export function isStickyColumn(
  index: number,
  total: number,
  cfg: StickyConfig | undefined
): boolean {
  if (!cfg) return false
  const left = cfg.left ?? 0
  const right = cfg.right ?? 0
  return index < left || (right > 0 && index >= total - right)
}
