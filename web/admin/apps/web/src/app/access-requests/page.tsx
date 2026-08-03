import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  Ban,
  Check,
  ClipboardCheck,
  LoaderCircle,
  Plus,
  RefreshCw,
  Undo2,
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
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@workspace/ui/components/tabs"
import { Textarea } from "@workspace/ui/components/textarea"
import { useOutletContext } from "react-router"
import { Alert } from "../../components/alert"
import { useAuth } from "../../components/auth"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type AccessRequest,
  type AccessRequestStatus,
  type EffectiveMenu,
  type RequestableRole,
} from "../../lib/iam-api"
import { effectiveMenuPaths } from "../../lib/navigation"

type DecisionAction = "approve" | "reject" | "revoke" | "withdraw"

interface Decision {
  action: DecisionAction
  request: AccessRequest
}

export default function AccessRequestsPage() {
  const { session } = useAuth()
  const { menus } = useOutletContext<{ menus: EffectiveMenu[] | null }>()
  const canApprove = Boolean(
    menus && effectiveMenuPaths(menus).has("/iam/access-requests")
  )
  const [mine, setMine] = useState<AccessRequest[]>([])
  const [queue, setQueue] = useState<AccessRequest[]>([])
  const [roles, setRoles] = useState<RequestableRole[]>([])
  const [tab, setTab] = useState("mine")
  const [createOpen, setCreateOpen] = useState(false)
  const [roleID, setRoleID] = useState("")
  const [expiresAt, setExpiresAt] = useState("")
  const [decision, setDecision] = useState<Decision | null>(null)
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(
    () =>
      Promise.all([
        iamApi.myAccessRequests(),
        iamApi.requestableRoles(),
        canApprove ? iamApi.accessRequests() : Promise.resolve([]),
      ]).then(([myRequests, availableRoles, approvalQueue]) => {
        setMine(myRequests)
        setRoles(availableRoles)
        setQueue(approvalQueue)
        setLoaded(true)
      }),
    [canApprove]
  )

  useEffect(() => {
    void refresh().catch((cause: Error) => {
      setError(cause.message)
      setLoaded(true)
    })
  }, [refresh])

  const openCreate = () => {
    setRoleID(roles[0]?.id ?? "")
    setExpiresAt(toLocalInputValue(new Date(Date.now() + 8 * 60 * 60 * 1000)))
    setCreateOpen(true)
  }

  const submitRequest = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const data = new FormData(event.currentTarget)
    setBusy(true)
    setError("")
    try {
      await iamApi.createAccessRequest({
        role_id: roleID,
        reason: String(data.get("reason") ?? "").trim(),
        access_expires_at: new Date(expiresAt).toISOString(),
      })
      setCreateOpen(false)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const applyDecision = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!decision) return
    const data = new FormData(event.currentTarget)
    const note = String(data.get("note") ?? "").trim()
    setBusy(true)
    setError("")
    try {
      switch (decision.action) {
        case "approve":
          await iamApi.approveAccessRequest(decision.request.id, note)
          break
        case "reject":
          await iamApi.rejectAccessRequest(decision.request.id, note)
          break
        case "revoke":
          await iamApi.revokeAccessRequest(decision.request.id, note)
          break
        case "withdraw":
          await iamApi.withdrawAccessRequest(decision.request.id, note)
          break
      }
      setDecision(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="访问申请"
        description="申请限时角色权限，并由另一名授权人员审批"
        action={
          <div className="flex gap-2">
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => void refresh().catch(showError(setError))}
            >
              <RefreshCw className={busy ? "animate-spin" : ""} />
              刷新
            </Button>
            <Button disabled={roles.length === 0 || busy} onClick={openCreate}>
              <Plus />
              发起申请
            </Button>
          </div>
        }
      />
      {error && <Alert>{error}</Alert>}
      <Tabs value={canApprove ? tab : "mine"} onValueChange={setTab}>
        <TabsList className="mb-4">
          <TabsTrigger value="mine">我的申请</TabsTrigger>
          {canApprove && <TabsTrigger value="queue">审批队列</TabsTrigger>}
        </TabsList>
        <TabsContent value="mine">
          <AccessRequestTable
            items={mine}
            loaded={loaded}
            mode="mine"
            currentSubject={session?.subject ?? ""}
            busy={busy}
            onDecision={setDecision}
          />
        </TabsContent>
        {canApprove && (
          <TabsContent value="queue">
            <AccessRequestTable
              items={queue}
              loaded={loaded}
              mode="queue"
              currentSubject={session?.subject ?? ""}
              busy={busy}
              onDecision={setDecision}
            />
          </TabsContent>
        )}
      </Tabs>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>发起访问申请</DialogTitle>
            <DialogDescription>
              权限仅在审批后生效，并在设定时间自动失效。
            </DialogDescription>
          </DialogHeader>
          <form
            id="access-request-form"
            className="grid gap-4"
            onSubmit={submitRequest}
          >
            <div className="grid gap-2">
              <Label htmlFor="access-request-role">角色</Label>
              <SimpleSelect
                id="access-request-role"
                value={roleID}
                onValueChange={setRoleID}
                options={roles.map((role) => ({
                  value: role.id,
                  label: role.name,
                }))}
                placeholder="选择角色"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="access-request-expiry">访问截止时间</Label>
              <Input
                id="access-request-expiry"
                type="datetime-local"
                value={expiresAt}
                min={toLocalInputValue(new Date())}
                onChange={(event) => setExpiresAt(event.target.value)}
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="access-request-reason">申请原因</Label>
              <Textarea
                id="access-request-reason"
                name="reason"
                minLength={3}
                maxLength={500}
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
              form="access-request-form"
              disabled={busy || !roleID || !expiresAt}
            >
              {busy && <LoaderCircle className="animate-spin" />}
              提交申请
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(decision)} onOpenChange={() => setDecision(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {decision && decisionTitle[decision.action]}
            </DialogTitle>
            <DialogDescription>
              {decision?.request.requester_id} · {decision?.request.role_name}
            </DialogDescription>
          </DialogHeader>
          <form
            id="access-decision-form"
            className="grid gap-2"
            onSubmit={applyDecision}
          >
            <Label htmlFor="access-decision-note">
              {decision?.action === "approve" ? "审批备注" : "原因"}
            </Label>
            <Textarea
              id="access-decision-note"
              name="note"
              maxLength={500}
              autoFocus
            />
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDecision(null)}>
              取消
            </Button>
            <Button
              type="submit"
              form="access-decision-form"
              variant={
                decision?.action === "approve" ? "default" : "destructive"
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

function AccessRequestTable({
  items,
  loaded,
  mode,
  currentSubject,
  busy,
  onDecision,
}: {
  items: AccessRequest[]
  loaded: boolean
  mode: "mine" | "queue"
  currentSubject: string
  busy: boolean
  onDecision: (decision: Decision) => void
}) {
  if (loaded && items.length === 0)
    return (
      <div className="grid min-h-40 place-items-center rounded-md border bg-card text-sm text-muted-foreground">
        <div className="grid justify-items-center gap-2">
          <ClipboardCheck className="size-7" />
          暂无访问申请
        </div>
      </div>
    )
  return (
    <>
      <div className="space-y-2 md:hidden">
        {items.map((request) => (
          <article key={request.id} className="rounded-md border bg-card p-4">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <strong className="block truncate text-sm">
                  {request.role_name}
                </strong>
                <span className="block truncate font-mono text-xs text-muted-foreground">
                  {request.role_id}
                </span>
              </div>
              <RequestStatus status={request.status} />
            </div>
            <p className="mt-3 text-sm break-words">{request.reason}</p>
            <dl className="mt-3 grid grid-cols-[5rem_1fr] gap-x-3 gap-y-1 text-xs">
              <dt className="text-muted-foreground">申请人</dt>
              <dd className="truncate">{request.requester_id}</dd>
              <dt className="text-muted-foreground">访问截止</dt>
              <dd>{formatTime(request.access_expires_at)}</dd>
              <dt className="text-muted-foreground">审批人</dt>
              <dd className="truncate">{request.decided_by || "-"}</dd>
            </dl>
            <div className="mt-3 flex min-h-9 items-center justify-end border-t pt-2">
              <RequestActions
                request={request}
                mode={mode}
                currentSubject={currentSubject}
                busy={busy}
                onDecision={onDecision}
              />
            </div>
          </article>
        ))}
      </div>
      <div className="hidden overflow-x-auto rounded-md border bg-card md:block">
        <Table className="min-w-[1040px]">
          <TableHeader>
            <TableRow>
              <TableHead>申请人</TableHead>
              <TableHead>角色</TableHead>
              <TableHead>原因</TableHead>
              <TableHead className="w-24">状态</TableHead>
              <TableHead>访问截止</TableHead>
              <TableHead>审批人</TableHead>
              <TableHead className="w-32 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((request) => (
              <TableRow key={request.id}>
                <TableCell>
                  <strong className="block text-sm">
                    {request.requester_id}
                  </strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {request.id}
                  </span>
                </TableCell>
                <TableCell>
                  <strong className="block text-sm">{request.role_name}</strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {request.role_id}
                  </span>
                </TableCell>
                <TableCell className="max-w-72 text-sm whitespace-normal">
                  {request.reason}
                </TableCell>
                <TableCell>
                  <RequestStatus status={request.status} />
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {formatTime(request.access_expires_at)}
                </TableCell>
                <TableCell className="text-sm">
                  {request.decided_by || "-"}
                </TableCell>
                <TableCell className="text-right">
                  <RequestActions
                    request={request}
                    mode={mode}
                    currentSubject={currentSubject}
                    busy={busy}
                    onDecision={onDecision}
                  />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </>
  )
}

function RequestActions({
  request,
  mode,
  currentSubject,
  busy,
  onDecision,
}: {
  request: AccessRequest
  mode: "mine" | "queue"
  currentSubject: string
  busy: boolean
  onDecision: (decision: Decision) => void
}) {
  if (mode === "mine") {
    if (request.status !== "pending" && request.status !== "approved")
      return null
    return (
      <IconAction
        label={request.status === "pending" ? "撤回申请" : "放弃访问权限"}
        disabled={busy}
        onClick={() => onDecision({ action: "withdraw", request })}
      >
        <Undo2 />
      </IconAction>
    )
  }
  if (request.requester_id === currentSubject && request.status === "pending")
    return <span className="text-xs text-muted-foreground">需他人审批</span>
  if (request.status === "pending")
    return (
      <div className="flex justify-end gap-1">
        <IconAction
          label="批准申请"
          disabled={busy}
          onClick={() => onDecision({ action: "approve", request })}
        >
          <Check />
        </IconAction>
        <IconAction
          label="拒绝申请"
          disabled={busy}
          onClick={() => onDecision({ action: "reject", request })}
        >
          <X />
        </IconAction>
      </div>
    )
  if (request.status === "approved")
    return (
      <IconAction
        label="撤销访问权限"
        disabled={busy}
        onClick={() => onDecision({ action: "revoke", request })}
      >
        <Ban />
      </IconAction>
    )
  return null
}

function IconAction({
  label,
  children,
  ...props
}: React.ComponentProps<typeof Button> & { label: string }) {
  return (
    <Button
      type="button"
      size="icon"
      variant="ghost"
      aria-label={label}
      title={label}
      {...props}
    >
      {children}
    </Button>
  )
}

function RequestStatus({ status }: { status: AccessRequestStatus }) {
  const value = statusLabels[status]
  return (
    <Badge className="shrink-0 whitespace-nowrap" variant={value.variant}>
      {value.label}
    </Badge>
  )
}

const statusLabels: Record<
  AccessRequestStatus,
  {
    label: string
    variant: "default" | "secondary" | "destructive" | "outline"
  }
> = {
  pending: { label: "待审批", variant: "outline" },
  approved: { label: "已批准", variant: "secondary" },
  rejected: { label: "已拒绝", variant: "destructive" },
  cancelled: { label: "已撤回", variant: "outline" },
  revoked: { label: "已撤销", variant: "destructive" },
  expired: { label: "已过期", variant: "outline" },
}

const decisionTitle: Record<DecisionAction, string> = {
  approve: "批准访问申请",
  reject: "拒绝访问申请",
  revoke: "撤销访问权限",
  withdraw: "撤回申请或放弃权限",
}

function toLocalInputValue(value: Date): string {
  const offset = value.getTimezoneOffset() * 60_000
  return new Date(value.getTime() - offset).toISOString().slice(0, 16)
}

function formatTime(value: string): string {
  return new Date(value).toLocaleString()
}

function showError(setError: (message: string) => void) {
  return (cause: unknown) => setError((cause as Error).message)
}
