import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Ban, Copy, MailPlus, RefreshCw } from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Checkbox } from "@workspace/ui/components/checkbox"
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
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Department,
  type Invitation,
  type IssuedInvitation,
  type Role,
} from "../../lib/iam-api"

export default function InvitationsPage() {
  const [items, setItems] = useState<Invitation[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [open, setOpen] = useState(false)
  const [issued, setIssued] = useState<IssuedInvitation | null>(null)
  const [departmentID, setDepartmentID] = useState("")
  const [roleIDs, setRoleIDs] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [loaded, setLoaded] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(
    () =>
      iamApi.invitations().then((invitations) => {
        setItems(invitations)
        setLoaded(true)
      }),
    []
  )

  useEffect(() => {
    void refresh().catch((cause: Error) => {
      setError(cause.message)
      setLoaded(true)
    })
    void Promise.allSettled([iamApi.departments(), iamApi.roles()]).then(
      ([departmentResult, roleResult]) => {
        if (departmentResult.status === "fulfilled")
          setDepartments(departmentResult.value)
        if (roleResult.status === "fulfilled") setRoles(roleResult.value)
      }
    )
  }, [refresh])

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    try {
      const result = await iamApi.createInvitation({
        email: String(data.get("email") ?? "").trim(),
        department_id: departmentID || undefined,
        role_ids: roleIDs,
        expires_in_hours: Number(data.get("expires_in_hours")),
      })
      setOpen(false)
      setIssued(result)
      setDepartmentID("")
      setRoleIDs([])
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const resend = async (invitation: Invitation) => {
    if (!window.confirm("重新发送会立即使旧邀请凭据失效，是否继续？")) return
    setBusy(true)
    setError("")
    try {
      setIssued(await iamApi.resendInvitation(invitation.id))
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const revoke = async (invitation: Invitation) => {
    if (!window.confirm(`确认撤销发往 ${invitation.email} 的邀请？`)) return
    setBusy(true)
    setError("")
    try {
      await iamApi.revokeInvitation(invitation.id)
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
        title="成员邀请"
        description="签发、轮换和撤销当前租户的成员邀请"
        action={
          <Button onClick={() => setOpen(true)}>
            <MailPlus />
            创建邀请
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[860px]">
          <TableHeader>
            <TableRow>
              <TableHead>邮箱</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>默认部门</TableHead>
              <TableHead>默认角色</TableHead>
              <TableHead>到期时间</TableHead>
              <TableHead className="w-28 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((invitation) => (
              <TableRow key={invitation.id}>
                <TableCell>
                  <strong className="block text-sm">{invitation.email}</strong>
                  <span className="font-mono text-xs text-muted-foreground">
                    {invitation.id}
                  </span>
                </TableCell>
                <TableCell>
                  <InvitationStatus status={invitation.status} />
                </TableCell>
                <TableCell className="text-sm">
                  {departments.find(
                    (department) => department.id === invitation.department_id
                  )?.name ??
                    invitation.department_id ??
                    "未分配"}
                </TableCell>
                <TableCell className="text-sm">
                  {invitation.role_ids.length
                    ? invitation.role_ids
                        .map(
                          (id) =>
                            roles.find((role) => role.id === id)?.name ?? id
                        )
                        .join("、")
                    : "未分配"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {new Date(invitation.expires_at).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  {invitation.status === "pending" && (
                    <div className="flex justify-end gap-1">
                      <Button
                        size="icon"
                        variant="ghost"
                        disabled={busy}
                        onClick={() => void resend(invitation)}
                        aria-label={`重新发送 ${invitation.email}`}
                        title="重新发送并轮换凭据"
                      >
                        <RefreshCw />
                      </Button>
                      <Button
                        size="icon"
                        variant="ghost"
                        disabled={busy}
                        onClick={() => void revoke(invitation)}
                        aria-label={`撤销 ${invitation.email}`}
                        title="撤销邀请"
                      >
                        <Ban />
                      </Button>
                    </div>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {loaded && items.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <MailPlus className="mb-2 size-7" />
            暂无成员邀请
          </div>
        )}
      </div>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next)
          if (!next) {
            setDepartmentID("")
            setRoleIDs([])
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>创建成员邀请</DialogTitle>
            <DialogDescription>
              接受邀请时将创建已验证邮箱的本地主体和当前租户成员关系。
            </DialogDescription>
          </DialogHeader>
          <form id="invitation-form" className="grid gap-4" onSubmit={submit}>
            <div className="grid gap-2">
              <Label htmlFor="invitation-email">邮箱</Label>
              <Input
                id="invitation-email"
                name="email"
                type="email"
                autoComplete="email"
                required
                maxLength={320}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="invitation-expiry">有效小时数</Label>
              <Input
                id="invitation-expiry"
                name="expires_in_hours"
                type="number"
                min={1}
                max={720}
                defaultValue={72}
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="invitation-department">默认部门</Label>
              <SimpleSelect
                id="invitation-department"
                value={departmentID || "__none__"}
                onValueChange={(value) =>
                  setDepartmentID(value === "__none__" ? "" : value)
                }
                options={[
                  { value: "__none__", label: "未分配" },
                  ...departments
                    .filter((department) => department.status === "active")
                    .map((department) => ({
                      value: department.id,
                      label: department.name,
                    })),
                ]}
              />
            </div>
            <fieldset className="grid gap-2">
              <legend className="text-sm font-medium">默认角色</legend>
              <div className="max-h-36 space-y-2 overflow-y-auto rounded-md border p-3">
                {roles.length ? (
                  roles.map((role) => (
                    <label
                      key={role.id}
                      className="flex min-h-7 items-center gap-2 text-sm"
                    >
                      <Checkbox
                        checked={roleIDs.includes(role.id)}
                        onCheckedChange={(checked) =>
                          setRoleIDs((current) =>
                            checked
                              ? [...current, role.id]
                              : current.filter((id) => id !== role.id)
                          )
                        }
                      />
                      {role.name}
                    </label>
                  ))
                ) : (
                  <span className="text-sm text-muted-foreground">
                    无可选角色
                  </span>
                )}
              </div>
            </fieldset>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="invitation-form" disabled={busy}>
              {busy ? "签发中" : "签发邀请"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={Boolean(issued)} onOpenChange={() => setIssued(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>一次性邀请凭据</DialogTitle>
            <DialogDescription>
              此凭据仅显示一次。关闭后只能重新发送并轮换凭据。
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="issued-invitation-token">邀请凭据</Label>
            <div className="flex gap-2">
              <Input
                id="issued-invitation-token"
                className="font-mono text-xs"
                value={issued?.token ?? ""}
                readOnly
              />
              <Button
                size="icon"
                variant="outline"
                onClick={() =>
                  void navigator.clipboard
                    .writeText(issued?.token ?? "")
                    .catch(() =>
                      setError("浏览器拒绝写入剪贴板，请手动选择凭据。")
                    )
                }
                aria-label="复制邀请凭据"
                title="复制邀请凭据"
              >
                <Copy />
              </Button>
            </div>
          </div>
          <DialogFooter>
            <Button onClick={() => setIssued(null)}>完成</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function InvitationStatus({ status }: { status: Invitation["status"] }) {
  const label = {
    pending: "待接受",
    accepted: "已接受",
    revoked: "已撤销",
    expired: "已过期",
  }[status]
  return (
    <Badge
      variant={
        status === "pending"
          ? "secondary"
          : status === "accepted"
            ? "outline"
            : "destructive"
      }
    >
      {label}
    </Badge>
  )
}
