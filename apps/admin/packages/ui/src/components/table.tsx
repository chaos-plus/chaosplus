import * as React from "react"

import { cn } from "@workspace/ui/lib/utils"

interface TableProps extends React.HTMLAttributes<HTMLTableElement> {
  /** Class for the scroll container (the element sticky cells position against). */
  containerClassName?: string
  /** Style for the scroll container — set `maxHeight` here to enable a sticky header. */
  containerStyle?: React.CSSProperties
}

const Table = React.forwardRef<HTMLTableElement, TableProps>(
  ({ className, containerClassName, containerStyle, ...props }, ref) => {
    const scrollRef = React.useRef<HTMLDivElement>(null)
    const [thumb, setThumb] = React.useState<{
      left: number
      width: number
    } | null>(null)

    // Safari's overlay scrollbar auto-hides regardless of CSS (::-webkit-scrollbar styling
    // doesn't override it), so a horizontally-scrollable table can look non-scrollable at
    // rest. Draw a always-visible thumb ourselves, synced to scroll position.
    const updateThumb = React.useCallback(() => {
      const el = scrollRef.current
      if (!el) return
      const { scrollWidth, clientWidth, scrollLeft } = el
      if (scrollWidth <= clientWidth + 1) {
        setThumb(null)
        return
      }
      const width = Math.max(24, (clientWidth / scrollWidth) * clientWidth)
      const left =
        (scrollLeft / (scrollWidth - clientWidth)) * (clientWidth - width)
      setThumb({ left, width })
    }, [])

    React.useEffect(() => {
      const el = scrollRef.current
      if (!el) return
      updateThumb()
      el.addEventListener("scroll", updateThumb, { passive: true })
      const ro = new ResizeObserver(updateThumb)
      ro.observe(el)
      return () => {
        el.removeEventListener("scroll", updateThumb)
        ro.disconnect()
      }
    }, [updateThumb])

    return (
      <div className="relative w-full">
        <div
          ref={scrollRef}
          className={cn("w-full overflow-auto", containerClassName)}
          style={containerStyle}
        >
          <table
            ref={ref}
            className={cn("w-full caption-bottom text-sm", className)}
            {...props}
          />
        </div>
        {thumb ? (
          <div className="pointer-events-none absolute inset-x-0 bottom-0.5 h-1.5 px-0.5">
            <div
              className="h-full rounded-full bg-border"
              style={{ marginLeft: thumb.left, width: thumb.width }}
            />
          </div>
        ) : null}
      </div>
    )
  }
)
Table.displayName = "Table"

const TableHeader = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <thead ref={ref} className={cn("[&_tr]:border-b", className)} {...props} />
))
TableHeader.displayName = "TableHeader"

const TableBody = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tbody
    ref={ref}
    className={cn("[&_tr:last-child]:border-0", className)}
    {...props}
  />
))
TableBody.displayName = "TableBody"

const TableFooter = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tfoot
    ref={ref}
    className={cn(
      "border-t bg-muted/50 font-medium [&_tr]:last:border-b-0",
      className
    )}
    {...props}
  />
))
TableFooter.displayName = "TableFooter"

const TableRow = React.forwardRef<
  HTMLTableRowElement,
  React.HTMLAttributes<HTMLTableRowElement>
>(({ className, ...props }, ref) => (
  <tr
    ref={ref}
    className={cn(
      "border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted",
      className
    )}
    {...props}
  />
))
TableRow.displayName = "TableRow"

const TableHead = React.forwardRef<
  HTMLTableCellElement,
  React.ThHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <th
    ref={ref}
    className={cn(
      "h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
      className
    )}
    {...props}
  />
))
TableHead.displayName = "TableHead"

const TableCell = React.forwardRef<
  HTMLTableCellElement,
  React.TdHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <td
    ref={ref}
    className={cn("p-4 align-middle [&:has([role=checkbox])]:pr-0", className)}
    {...props}
  />
))
TableCell.displayName = "TableCell"

const TableCaption = React.forwardRef<
  HTMLTableCaptionElement,
  React.HTMLAttributes<HTMLTableCaptionElement>
>(({ className, ...props }, ref) => (
  <caption
    ref={ref}
    className={cn("mt-4 text-sm text-muted-foreground", className)}
    {...props}
  />
))
TableCaption.displayName = "TableCaption"

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
