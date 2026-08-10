import { useEffect, useState, type FormEvent } from "react"
import {
  CheckCircle2,
  ChevronLeft,
  ChevronRight,
  Download,
  Eye,
  FileClock,
  LoaderCircle,
  RefreshCw,
  Search,
  ShieldAlert,
} from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button, buttonVariants } from "@workspace/ui/components/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { Label } from "@workspace/ui/components/label"
import { SimpleSelect } from "@workspace/ui/components/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@workspace/ui/components/tooltip"
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import { iamApi, type AuditEvent, type AuditIntegrity } from "../../lib/iam-api"

const pageSize = 50

interface AuditFilters {
  eventType: string
  outcome: string
  principalID: string
  targetType: string
  targetID: string
  from: string
  to: string
}

const emptyFilters: AuditFilters = {
  eventType: "",
  outcome: "",
  principalID: "",
  targetType: "",
  targetID: "",
  from: "",
  to: "",
}

const outcomeOptions = [
  { value: "all", label: "全部结果" },
  { value: "success", label: "成功" },
  { value: "denied", label: "拒绝" },
  { value: "failure", label: "失败" },
]

export default function AuditEventsPage() {
  const [events, setEvents] = useState<AuditEvent[]>([])
  const [total, setTotal] = useState(0)
  const [offset, setOffset] = useState(0)
  const [query, setQuery] = useState("")
  const [filters, setFilters] = useState<AuditFilters>(emptyFilters)
  const [integrity, setIntegrity] = useState<AuditIntegrity | null>(null)
  const [selected, setSelected] = useState<AuditEvent | null>(null)
  const [loading, setLoading] = useState(true)
  const [exporting, setExporting] = useState(false)
  const [detailBusy, setDetailBusy] = useState(false)
  const [error, setError] = useState("")
  const [refreshKey, setRefreshKey] = useState(0)

  useEffect(() => {
    let active = true
    const params = new URLSearchParams(query)
    params.set("offset", String(offset))
    params.set("limit", String(pageSize))
    void Promise.all([
      iamApi.auditEvents(params.toString()),
      iamApi.auditIntegrity(),
    ]).then(
      ([page, chain]) => {
        if (!active) return
        setEvents(page.items)
        setTotal(page.total)
        setIntegrity(chain)
        setError("")
        setLoading(false)
      },
      (cause: Error) => {
        if (!active) return
        setError(cause.message)
        setLoading(false)
      }
    )
    return () => {
      active = false
    }
  }, [offset, query, refreshKey])

  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const params = new URLSearchParams()
    append(params, "event_type", filters.eventType)
    append(params, "outcome", filters.outcome === "all" ? "" : filters.outcome)
    append(params, "principal_id", filters.principalID)
    append(params, "target_type", filters.targetType)
    append(params, "target_id", filters.targetID)
    append(params, "from", asRFC3339(filters.from))
    append(params, "to", asRFC3339(filters.to))
    setLoading(true)
    setOffset(0)
    setQuery(params.toString())
  }

  const reset = () => {
    setLoading(true)
    setFilters(emptyFilters)
    setOffset(0)
    setQuery("")
  }

  const openDetail = async (event: AuditEvent) => {
    setDetailBusy(true)
    setError("")
    try {
      setSelected(await iamApi.auditEvent(event.id))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setDetailBusy(false)
    }
  }

  const exportEvents = async () => {
    setExporting(true)
    setError("")
    try {
      const download = await iamApi.auditExport(query)
      const url = URL.createObjectURL(download.blob)
      const anchor = document.createElement("a")
      anchor.href = url
      anchor.download = download.filename
      anchor.hidden = true
      document.body.append(anchor)
      anchor.click()
      anchor.remove()
      window.setTimeout(() => URL.revokeObjectURL(url), 0)
      setLoading(true)
      setRefreshKey((value) => value + 1)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setExporting(false)
    }
  }

  const first = total === 0 ? 0 : offset + 1
  const last = Math.min(offset + events.length, total)

  return (
    <>
      <PageHeader
        title="审计日志"
        description="检索租户安全事件并验证追加式 hash chain"
        action={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={loading || exporting || integrity?.valid !== true}
              onClick={() => void exportEvents()}
            >
              {exporting ? (
                <LoaderCircle className="animate-spin" />
              ) : (
                <Download />
              )}
              {exporting ? "正在导出" : "导出当前结果"}
            </Button>
            <Button
              variant="outline"
              disabled={loading || exporting}
              onClick={() => {
                setLoading(true)
                setRefreshKey((value) => value + 1)
              }}
            >
              <RefreshCw className={loading ? "animate-spin" : ""} />
              刷新
            </Button>
          </div>
        }
      />
      {error && <Alert>{error}</Alert>}
      <IntegrityStatus value={integrity} loading={loading} />

      <form
        className="mb-4 grid gap-3 rounded-md border bg-card p-4 md:grid-cols-2 xl:grid-cols-4"
        onSubmit={submit}
      >
        <FilterInput
          id="audit-event-type"
          label="事件类型"
          value={filters.eventType}
          placeholder="oauth_client_created"
          onChange={(value) => setFilters({ ...filters, eventType: value })}
        />
        <div className="grid gap-2">
          <Label htmlFor="audit-outcome">结果</Label>
          <SimpleSelect
            id="audit-outcome"
            options={outcomeOptions}
            value={filters.outcome || "all"}
            onValueChange={(value) =>
              setFilters({ ...filters, outcome: value })
            }
          />
        </div>
        <FilterInput
          id="audit-principal"
          label="操作主体 ID"
          value={filters.principalID}
          onChange={(value) => setFilters({ ...filters, principalID: value })}
        />
        <FilterInput
          id="audit-target-type"
          label="目标类型"
          value={filters.targetType}
          placeholder="oauth_client"
          onChange={(value) => setFilters({ ...filters, targetType: value })}
        />
        <FilterInput
          id="audit-target-id"
          label="目标 ID"
          value={filters.targetID}
          onChange={(value) => setFilters({ ...filters, targetID: value })}
        />
        <FilterInput
          id="audit-from"
          label="开始时间"
          type="datetime-local"
          value={filters.from}
          onChange={(value) => setFilters({ ...filters, from: value })}
        />
        <FilterInput
          id="audit-to"
          label="结束时间"
          type="datetime-local"
          value={filters.to}
          onChange={(value) => setFilters({ ...filters, to: value })}
        />
        <div className="flex items-end gap-2">
          <Button type="submit" className="flex-1">
            <Search />
            查询
          </Button>
          <Button type="button" variant="outline" onClick={reset}>
            重置
          </Button>
        </div>
      </form>

      <section className="overflow-hidden rounded-md border bg-card">
        <header className="flex min-h-14 items-center justify-between gap-3 border-b bg-muted/20 px-4">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <FileClock className="size-4 text-muted-foreground" />
            安全事件
          </div>
          <span className="text-xs text-muted-foreground">
            {loading ? "正在加载" : `${first}-${last} / ${total}`}
          </span>
        </header>
        <div className="hidden md:block">
          <Table containerClassName="min-h-64">
            <TableHeader>
              <TableRow>
                <TableHead className="min-w-44">发生时间</TableHead>
                <TableHead className="min-w-52">事件</TableHead>
                <TableHead className="min-w-44">主体</TableHead>
                <TableHead className="min-w-52">目标</TableHead>
                <TableHead className="w-24">结果</TableHead>
                <TableHead className="w-24">链序号</TableHead>
                <TableHead className="w-20 text-right">详情</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((event) => (
                <TableRow key={event.id}>
                  <TableCell className="text-xs">
                    {formatTime(event.created_at)}
                  </TableCell>
                  <TableCell>
                    <code className="font-mono text-xs">
                      {event.event_type}
                    </code>
                  </TableCell>
                  <TableCell>
                    <code className="block max-w-48 truncate font-mono text-xs text-muted-foreground">
                      {event.principal_id || "system"}
                    </code>
                  </TableCell>
                  <TableCell>
                    <span className="block text-xs">
                      {event.target_type || "-"}
                    </span>
                    <code className="mt-1 block max-w-56 truncate font-mono text-xs text-muted-foreground">
                      {event.target_id || "-"}
                    </code>
                  </TableCell>
                  <TableCell>
                    <OutcomeBadge outcome={event.outcome} />
                  </TableCell>
                  <TableCell className="text-xs">
                    {event.sequence > 0 ? `#${event.sequence}` : "legacy"}
                  </TableCell>
                  <TableCell className="text-right">
                    <DetailButton
                      event={event}
                      disabled={detailBusy}
                      onClick={() => void openDetail(event)}
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!loading && events.length === 0 && (
                <TableRow>
                  <TableCell colSpan={7} className="h-56 text-center">
                    <EmptyEvents />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
        <div className="md:hidden">
          {events.map((event) => (
            <article
              key={event.id}
              className="space-y-3 border-b p-4 last:border-0"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <code className="block font-mono text-xs font-semibold break-words">
                    {event.event_type}
                  </code>
                  <time className="mt-1 block text-xs text-muted-foreground">
                    {formatTime(event.created_at)}
                  </time>
                </div>
                <OutcomeBadge outcome={event.outcome} />
              </div>
              <div className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-xs">
                <span className="text-muted-foreground">主体</span>
                <code className="truncate font-mono">
                  {event.principal_id || "system"}
                </code>
                <span className="text-muted-foreground">目标</span>
                <code className="truncate font-mono">
                  {event.target_type || "-"}:{event.target_id || "-"}
                </code>
                <span className="text-muted-foreground">链序号</span>
                <span>
                  {event.sequence > 0 ? `#${event.sequence}` : "legacy"}
                </span>
              </div>
              <div className="flex justify-end">
                <DetailButton
                  event={event}
                  disabled={detailBusy}
                  onClick={() => void openDetail(event)}
                />
              </div>
            </article>
          ))}
          {!loading && events.length === 0 && (
            <div className="grid h-56 place-items-center text-center">
              <EmptyEvents />
            </div>
          )}
        </div>
        <footer className="flex min-h-14 items-center justify-between gap-3 border-t px-4">
          <span className="text-xs text-muted-foreground">
            每页 {pageSize} 条
          </span>
          <div className="flex gap-2">
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label="上一页"
              disabled={loading || offset === 0}
              onClick={() => {
                setLoading(true)
                setOffset(Math.max(0, offset - pageSize))
              }}
            >
              <ChevronLeft />
            </Button>
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label="下一页"
              disabled={loading || offset + events.length >= total}
              onClick={() => {
                setLoading(true)
                setOffset(offset + pageSize)
              }}
            >
              <ChevronRight />
            </Button>
          </div>
        </footer>
      </section>
      <EventDetail value={selected} onClose={() => setSelected(null)} />
    </>
  )
}

function IntegrityStatus({
  value,
  loading,
}: {
  value: AuditIntegrity | null
  loading: boolean
}) {
  const valid = value?.valid === true
  return (
    <section
      className={`mb-4 flex min-h-16 items-center gap-3 rounded-md border px-4 ${valid ? "border-success/30 bg-success/5" : value ? "border-destructive/30 bg-destructive/5" : "bg-muted/20"}`}
      aria-live="polite"
    >
      {valid ? (
        <CheckCircle2 className="size-5 shrink-0 text-success" />
      ) : (
        <ShieldAlert className="size-5 shrink-0 text-muted-foreground" />
      )}
      <div className="min-w-0">
        <strong className="block text-sm">
          {loading
            ? "正在验证审计链"
            : valid
              ? "审计链完整"
              : value
                ? "审计链校验失败"
                : "尚未验证"}
        </strong>
        <p className="mt-1 truncate text-xs text-muted-foreground">
          {value
            ? `已验证 ${value.verified_events} 个链式事件，当前 head #${value.head_sequence}`
            : "等待当前租户的完整性结果"}
        </p>
      </div>
    </section>
  )
}

function FilterInput({
  id,
  label,
  value,
  onChange,
  type = "text",
  placeholder,
}: {
  id: string
  label: string
  value: string
  onChange: (value: string) => void
  type?: string
  placeholder?: string
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        type={type}
        value={value}
        placeholder={placeholder}
        maxLength={128}
        onChange={(event) => onChange(event.target.value)}
      />
    </div>
  )
}

function OutcomeBadge({ outcome }: { outcome: AuditEvent["outcome"] }) {
  const label =
    outcome === "success" ? "成功" : outcome === "denied" ? "拒绝" : "失败"
  return (
    <Badge
      variant={
        outcome === "success"
          ? "default"
          : outcome === "denied"
            ? "outline"
            : "destructive"
      }
      className="shrink-0 whitespace-nowrap"
    >
      {label}
    </Badge>
  )
}

function DetailButton({
  event,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & { event: AuditEvent }) {
  const label = `查看 ${event.event_type} 详情`
  return (
    <TooltipProvider>
      <Tooltip>
        <TooltipTrigger
          {...props}
          aria-label={label}
          className={buttonVariants({
            size: "icon",
            variant: "ghost",
            className: "size-10",
          })}
        >
          <Eye />
        </TooltipTrigger>
        <TooltipContent>{label}</TooltipContent>
      </Tooltip>
    </TooltipProvider>
  )
}

function EventDetail({
  value,
  onClose,
}: {
  value: AuditEvent | null
  onClose: () => void
}) {
  return (
    <Dialog open={value !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>审计事件详情</DialogTitle>
          <DialogDescription>
            {value
              ? `${value.event_type} · ${formatTime(value.created_at)}`
              : ""}
          </DialogDescription>
        </DialogHeader>
        {value && (
          <div className="grid gap-4">
            <DetailGrid event={value} />
            <DetailCode
              label="事件数据"
              value={JSON.stringify(value.detail, null, 2)}
            />
            <DetailCode
              label="Previous Hash"
              value={value.previous_hash || "-"}
            />
            <DetailCode
              label="Event Hash"
              value={value.event_hash || "legacy event"}
            />
          </div>
        )}
        <DialogFooter>
          <Button type="button" onClick={onClose}>
            关闭
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DetailGrid({ event }: { event: AuditEvent }) {
  const items = [
    ["Event ID", event.id],
    ["Tenant", event.tenant_id],
    ["Principal", event.principal_id || "system"],
    ["Target", `${event.target_type || "-"}:${event.target_id || "-"}`],
    ["Outcome", event.outcome],
    ["Sequence", event.sequence > 0 ? String(event.sequence) : "legacy"],
    ["IP", event.ip_address || "-"],
    ["User Agent", event.user_agent || "-"],
  ]
  return (
    <dl className="grid gap-x-4 gap-y-3 rounded-md border p-4 sm:grid-cols-2">
      {items.map(([label, value]) => (
        <div key={label} className="min-w-0">
          <dt className="text-xs text-muted-foreground">{label}</dt>
          <dd className="mt-1 font-mono text-xs break-words">{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function DetailCode({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <pre className="max-h-48 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs break-all whitespace-pre-wrap">
        {value}
      </pre>
    </div>
  )
}

function EmptyEvents() {
  return (
    <div>
      <FileClock className="mx-auto mb-2 size-7 text-muted-foreground" />
      <p className="text-sm font-medium">没有匹配的审计事件</p>
    </div>
  )
}

function append(params: URLSearchParams, key: string, value: string) {
  const trimmed = value.trim()
  if (trimmed) params.set(key, trimmed)
}

function asRFC3339(value: string): string {
  return value ? new Date(value).toISOString() : ""
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}
