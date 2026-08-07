import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  Ban,
  Check,
  CircleCheckBig,
  Eye,
  ListChecks,
  LoaderCircle,
  Plus,
  RefreshCw,
  X,
} from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table"
import { Textarea } from "@workspace/ui/components/textarea"
import { Alert } from "../../components/alert"
import { useAuth } from "../../components/auth"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type AccessReview,
  type AccessReviewDecision,
  type AccessReviewItem,
  type AccessReviewStatus,
} from "../../lib/iam-api"

type PendingAction =
  | {
      kind: "decide"
      review: AccessReview
      item: AccessReviewItem
      decision: Exclude<AccessReviewDecision, "pending">
    }
  | { kind: "complete"; review: AccessReview }
  | { kind: "cancel"; review: AccessReview }

export default function AccessReviewsPage() {
  const { session } = useAuth()
  const [reviews, setReviews] = useState<AccessReview[]>([])
  const [selected, setSelected] = useState<AccessReview | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [dueAt, setDueAt] = useState("")
  const [action, setAction] = useState<PendingAction | null>(null)
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(async (reviewID?: string) => {
    const [items, detail] = await Promise.all([
      iamApi.accessReviews(),
      reviewID ? iamApi.accessReview(reviewID) : Promise.resolve(null),
    ])
    setReviews(items)
    if (reviewID) setSelected(detail)
    setLoaded(true)
  }, [])

  useEffect(() => {
    let active = true
    void iamApi
      .accessReviews()
      .then((items) => {
        if (!active) return
        setReviews(items)
        setLoaded(true)
      })
      .catch((cause: Error) => {
        if (!active) return
        setError(cause.message)
        setLoaded(true)
      })
    return () => {
      active = false
    }
  }, [])

  const openCreate = () => {
    setDueAt(toLocalInputValue(new Date(Date.now() + 7 * 24 * 60 * 60 * 1000)))
    setCreateOpen(true)
  }

  const createReview = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setBusy(true)
    setError("")
    try {
      const created = await iamApi.createAccessReview({
        name: String(data.get("name") ?? "").trim(),
        due_at: new Date(dueAt).toISOString(),
      })
      setCreateOpen(false)
      setSelected(created)
      await refresh(created.id)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const applyAction = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!action) return
    setBusy(true)
    setError("")
    try {
      if (action.kind === "decide") {
        const data = new FormData(event.currentTarget)
        await iamApi.decideAccessReviewItem(
          action.review.id,
          action.item.id,
          action.decision,
          String(data.get("note") ?? "").trim()
        )
      } else if (action.kind === "complete") {
        await iamApi.completeAccessReview(action.review.id)
      } else {
        await iamApi.cancelAccessReview(action.review.id)
      }
      const reviewID = action.review.id
      setAction(null)
      await refresh(reviewID)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="访问复核"
        action={
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              disabled={busy}
              onClick={() =>
                void refresh(selected?.id).catch(showError(setError))
              }
            >
              <RefreshCw className={busy ? "animate-spin" : ""} />
              刷新
            </Button>
            <Button disabled={busy} onClick={openCreate}>
              <Plus />
              新建复核
            </Button>
          </div>
        }
      />
      {error && <Alert>{error}</Alert>}

      <section aria-labelledby="review-campaigns-heading">
        <h2
          id="review-campaigns-heading"
          className="mb-3 text-sm font-semibold"
        >
          复核活动
        </h2>
        <CampaignList
          items={reviews}
          loaded={loaded}
          selectedID={selected?.id}
          busy={busy}
          onSelect={(review) => {
            setBusy(true)
            setError("")
            void iamApi
              .accessReview(review.id)
              .then(setSelected)
              .catch(showError(setError))
              .finally(() => setBusy(false))
          }}
        />
      </section>

      {selected && (
        <section
          className="mt-8 border-t pt-6"
          aria-labelledby="review-detail-heading"
        >
          <div className="mb-4 flex flex-wrap items-start justify-between gap-4">
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <h2
                  id="review-detail-heading"
                  className="text-base font-semibold"
                >
                  {selected.name}
                </h2>
                <ReviewStatus status={selected.status} />
              </div>
              <p className="mt-1 font-mono text-xs text-muted-foreground">
                {selected.id}
              </p>
            </div>
            {selected.status === "open" && (
              <div className="flex flex-wrap gap-2">
                <Button
                  variant="outline"
                  disabled={busy}
                  onClick={() =>
                    setAction({ kind: "cancel", review: selected })
                  }
                >
                  <Ban />
                  取消复核
                </Button>
                <Button
                  disabled={busy || selected.pending > 0}
                  onClick={() =>
                    setAction({ kind: "complete", review: selected })
                  }
                >
                  <CircleCheckBig />
                  完成复核
                </Button>
              </div>
            )}
          </div>
          <dl className="mb-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-5">
            <ReviewMetric label="总计" value={selected.total} />
            <ReviewMetric label="待处理" value={selected.pending} />
            <ReviewMetric label="保留" value={selected.kept} />
            <ReviewMetric label="撤销" value={selected.revoked} />
            <div>
              <dt className="text-xs text-muted-foreground">截止时间</dt>
              <dd className="mt-1 text-xs">{formatTime(selected.due_at)}</dd>
            </div>
          </dl>
          <ReviewItems
            review={selected}
            currentSubject={session?.subject ?? ""}
            busy={busy}
            onAction={setAction}
          />
        </section>
      )}

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>新建访问复核</DialogTitle>
            <DialogDescription>
              创建时会固定记录当前有效授权。
            </DialogDescription>
          </DialogHeader>
          <form
            id="access-review-create"
            className="grid gap-4"
            onSubmit={createReview}
          >
            <div className="grid gap-2">
              <Label htmlFor="access-review-name">名称</Label>
              <Input
                id="access-review-name"
                name="name"
                minLength={3}
                maxLength={128}
                autoFocus
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="access-review-due-at">截止时间</Label>
              <Input
                id="access-review-due-at"
                type="datetime-local"
                value={dueAt}
                min={toLocalInputValue(new Date())}
                onChange={(event) => setDueAt(event.target.value)}
                required
              />
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setCreateOpen(false)}>
              取消
            </Button>
            <Button
              type="submit"
              form="access-review-create"
              disabled={busy || !dueAt}
            >
              {busy && <LoaderCircle className="animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(action)} onOpenChange={() => setAction(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{action && actionTitle(action)}</DialogTitle>
            <DialogDescription>
              {action && actionDescription(action)}
            </DialogDescription>
          </DialogHeader>
          <form
            id="access-review-action"
            className="grid gap-2"
            onSubmit={applyAction}
          >
            {action?.kind === "decide" && (
              <>
                <Label htmlFor="access-review-note">复核备注</Label>
                <Textarea
                  id="access-review-note"
                  name="note"
                  maxLength={500}
                  autoFocus
                />
              </>
            )}
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAction(null)}>
              返回
            </Button>
            <Button
              type="submit"
              form="access-review-action"
              variant={
                action?.kind === "cancel" ||
                (action?.kind === "decide" && action.decision === "revoke")
                  ? "destructive"
                  : "default"
              }
              disabled={busy}
            >
              {busy && <LoaderCircle className="animate-spin" />}
              确认
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function CampaignList({
  items,
  loaded,
  selectedID,
  busy,
  onSelect,
}: {
  items: AccessReview[]
  loaded: boolean
  selectedID?: string
  busy: boolean
  onSelect: (review: AccessReview) => void
}) {
  if (!loaded)
    return (
      <div
        className="grid min-h-32 place-items-center"
        aria-label="正在加载访问复核"
      >
        <LoaderCircle className="animate-spin text-muted-foreground" />
      </div>
    )
  if (items.length === 0)
    return (
      <div className="grid min-h-40 place-items-center rounded-md border bg-card text-sm text-muted-foreground">
        <div className="grid justify-items-center gap-2">
          <ListChecks className="size-7" />
          暂无访问复核
        </div>
      </div>
    )
  return (
    <>
      <div className="space-y-2 md:hidden">
        {items.map((review) => (
          <article
            key={review.id}
            className="rounded-md border bg-card p-4"
            data-review-id={review.id}
          >
            <div className="flex items-start justify-between gap-3">
              <strong className="min-w-0 truncate text-sm">
                {review.name}
              </strong>
              <ReviewStatus status={review.status} />
            </div>
            <dl className="mt-3 grid grid-cols-[4rem_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-muted-foreground">进度</dt>
              <dd>
                {review.total - review.pending}/{review.total}
              </dd>
              <dt className="text-muted-foreground">截止</dt>
              <dd>{formatTime(review.due_at)}</dd>
              <dt className="text-muted-foreground">负责人</dt>
              <dd className="truncate">{review.owner_id}</dd>
            </dl>
            <Button
              className="mt-3 w-full"
              variant={selectedID === review.id ? "secondary" : "outline"}
              disabled={busy}
              onClick={() => onSelect(review)}
            >
              <Eye />
              查看
            </Button>
          </article>
        ))}
      </div>
      <div className="hidden overflow-x-auto rounded-md border bg-card md:block">
        <Table className="min-w-[760px]">
          <TableHeader>
            <TableRow>
              <TableHead>名称</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>进度</TableHead>
              <TableHead>截止时间</TableHead>
              <TableHead>负责人</TableHead>
              <TableHead className="w-24 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((review) => (
              <TableRow
                key={review.id}
                data-review-id={review.id}
                data-state={selectedID === review.id ? "selected" : undefined}
              >
                <TableCell>
                  <strong className="block text-sm">{review.name}</strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {review.id}
                  </span>
                </TableCell>
                <TableCell>
                  <ReviewStatus status={review.status} />
                </TableCell>
                <TableCell>
                  {review.total - review.pending}/{review.total}
                </TableCell>
                <TableCell className="text-xs">
                  {formatTime(review.due_at)}
                </TableCell>
                <TableCell className="max-w-48 truncate text-sm">
                  {review.owner_id}
                </TableCell>
                <TableCell className="text-right">
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => onSelect(review)}
                  >
                    <Eye />
                    查看
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

function ReviewItems({
  review,
  currentSubject,
  busy,
  onAction,
}: {
  review: AccessReview
  currentSubject: string
  busy: boolean
  onAction: (action: PendingAction) => void
}) {
  const actions = (item: AccessReviewItem) =>
    item.decision === "pending" && review.status === "open" ? (
      <div className="flex justify-end gap-2">
        <Button
          size="sm"
          variant="outline"
          disabled={busy || item.principal_id === currentSubject}
          onClick={() =>
            onAction({ kind: "decide", review, item, decision: "keep" })
          }
        >
          <Check />
          保留
        </Button>
        <Button
          size="sm"
          variant="destructive"
          disabled={busy || item.principal_id === currentSubject}
          onClick={() =>
            onAction({ kind: "decide", review, item, decision: "revoke" })
          }
        >
          <X />
          撤销
        </Button>
      </div>
    ) : (
      <DecisionBadge decision={item.decision} />
    )

  return (
    <>
      <div className="space-y-2 md:hidden">
        {review.items.map((item) => (
          <article
            key={item.id}
            className="rounded-md border bg-card p-4"
            data-review-item-id={item.id}
          >
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <strong className="block truncate text-sm">
                  {item.principal_name}
                </strong>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {item.principal_id}
                </span>
              </div>
              <Badge variant="outline">
                {item.grant_type === "permanent" ? "长期" : "临时"}
              </Badge>
            </div>
            <dl className="mt-3 grid grid-cols-[4rem_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-muted-foreground">角色</dt>
              <dd className="truncate">{item.role_name}</dd>
              <dt className="text-muted-foreground">授权时间</dt>
              <dd>{formatTime(item.grant_created_at)}</dd>
              <dt className="text-muted-foreground">授权截止</dt>
              <dd>
                {item.grant_expires_at
                  ? formatTime(item.grant_expires_at)
                  : "长期有效"}
              </dd>
              {item.principal_id === currentSubject &&
                item.decision === "pending" && (
                  <>
                    <dt className="text-muted-foreground">限制</dt>
                    <dd>不能复核自己的权限</dd>
                  </>
                )}
            </dl>
            <div className="mt-3 border-t pt-3">{actions(item)}</div>
          </article>
        ))}
      </div>
      <div className="hidden overflow-x-auto rounded-md border bg-card md:block">
        <Table className="min-w-[980px]">
          <TableHeader>
            <TableRow>
              <TableHead>主体</TableHead>
              <TableHead>角色</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>授权时间</TableHead>
              <TableHead>授权截止</TableHead>
              <TableHead>决定人</TableHead>
              <TableHead className="w-48 text-right">复核决定</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {review.items.map((item) => (
              <TableRow key={item.id} data-review-item-id={item.id}>
                <TableCell>
                  <strong className="block text-sm">
                    {item.principal_name}
                  </strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {item.principal_id}
                  </span>
                </TableCell>
                <TableCell>
                  <strong className="block text-sm">{item.role_name}</strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {item.role_id}
                  </span>
                </TableCell>
                <TableCell>
                  <Badge variant="outline">
                    {item.grant_type === "permanent" ? "长期" : "临时"}
                  </Badge>
                </TableCell>
                <TableCell className="text-xs">
                  {formatTime(item.grant_created_at)}
                </TableCell>
                <TableCell className="text-xs">
                  {item.grant_expires_at
                    ? formatTime(item.grant_expires_at)
                    : "长期有效"}
                </TableCell>
                <TableCell className="max-w-40 truncate text-sm">
                  {item.decided_by || "-"}
                </TableCell>
                <TableCell className="text-right">{actions(item)}</TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

function ReviewMetric({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="mt-1 font-semibold tabular-nums">{value}</dd>
    </div>
  )
}

function ReviewStatus({ status }: { status: AccessReviewStatus }) {
  const value = reviewStatusLabels[status]
  return (
    <Badge className="shrink-0 whitespace-nowrap" variant={value.variant}>
      {value.label}
    </Badge>
  )
}

function DecisionBadge({ decision }: { decision: AccessReviewDecision }) {
  const value = decisionLabels[decision]
  return (
    <Badge className="whitespace-nowrap" variant={value.variant}>
      {value.label}
    </Badge>
  )
}

const reviewStatusLabels: Record<
  AccessReviewStatus,
  {
    label: string
    variant: "default" | "secondary" | "destructive" | "outline"
  }
> = {
  open: { label: "进行中", variant: "default" },
  completed: { label: "已完成", variant: "secondary" },
  cancelled: { label: "已取消", variant: "outline" },
  expired: { label: "已过期", variant: "destructive" },
}

const decisionLabels: Record<
  AccessReviewDecision,
  {
    label: string
    variant: "default" | "secondary" | "destructive" | "outline"
  }
> = {
  pending: { label: "待处理", variant: "outline" },
  keep: { label: "保留", variant: "secondary" },
  revoke: { label: "已撤销", variant: "destructive" },
}

function actionTitle(action: PendingAction): string {
  if (action.kind === "complete") return "完成访问复核"
  if (action.kind === "cancel") return "取消访问复核"
  return action.decision === "keep" ? "保留授权" : "撤销授权"
}

function actionDescription(action: PendingAction): string {
  if (action.kind === "complete") return `确认完成“${action.review.name}”。`
  if (action.kind === "cancel")
    return `确认取消“${action.review.name}”，已作出的复核决定不会回滚。`
  return `${action.item.principal_name} · ${action.item.role_name}`
}

function toLocalInputValue(value: Date): string {
  return new Date(value.getTime() - value.getTimezoneOffset() * 60_000)
    .toISOString()
    .slice(0, 16)
}

function formatTime(value: string): string {
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(value))
}

function showError(setError: (message: string) => void) {
  return (cause: unknown) => setError((cause as Error).message)
}
