import { useCallback, useEffect, useState, type FormEvent } from "react"
import { Pencil, Plus, Search, UserRoundCheck } from "lucide-react"
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
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Department,
  type Member,
  type Principal,
} from "../../lib/iam-api"

export default function PrincipalsPage() {
  const [items, setItems] = useState<Principal[]>([])
  const [members, setMembers] = useState<Member[]>([])
  const [departments, setDepartments] = useState<Department[]>([])
  const [search, setSearch] = useState("")
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Principal | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [departmentID, setDepartmentID] = useState("")
  const refresh = useCallback(
    () =>
      Promise.all([
        iamApi.principals(search),
        iamApi.members(),
        iamApi.departments(),
      ]).then(([result, nextMembers, nextDepartments]) => {
        setItems(result.items)
        setMembers(nextMembers)
        setDepartments(nextDepartments)
      }),
    [search]
  )
  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    try {
      if (editing) {
        await iamApi.updatePrincipal(editing.id, {
          display_name: String(data.get("display_name")),
          email: String(data.get("email")),
        })
        const membership = members.find(
          (member) => member.subject === editing.id
        )
        if (membership && (membership.department_id ?? "") !== departmentID)
          await iamApi.updateMember(editing.id, { department_id: departmentID })
      } else {
        await iamApi.createPrincipal({
          login_name: String(data.get("login_name")),
          password: String(data.get("password")),
          display_name: String(data.get("display_name")),
          email: String(data.get("email")),
        })
      }
      setOpen(false)
      setEditing(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }
  const editingMembership = editing
    ? members.find((member) => member.subject === editing.id)
    : undefined
  const toggle = async (principal: Principal) => {
    setBusy(true)
    setError("")
    try {
      if (principal.status === "active")
        await iamApi.disablePrincipal(principal.id)
      else await iamApi.restorePrincipal(principal.id)
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
        title="主体与成员"
        description="管理本地登录账号、全局生命周期及当前租户归属"
        action={
          <Button
            onClick={() => {
              setEditing(null)
              setDepartmentID("")
              setOpen(true)
            }}
          >
            <Plus />
            创建主体
          </Button>
        }
      />
      <div className="mb-4 flex items-center justify-between gap-3">
        <label className="relative w-full max-w-md">
          <Search className="absolute top-2.5 left-3 size-4 text-muted-foreground" />
          <Input
            className="pl-9"
            aria-label="搜索主体"
            placeholder="登录名、显示名或邮箱"
            value={search}
            onChange={(event) => setSearch(event.target.value)}
          />
        </label>
        <span className="text-xs whitespace-nowrap text-muted-foreground">
          {items.length} 条
        </span>
      </div>
      {error && <Alert>{error}</Alert>}
      <div className="overflow-hidden rounded-md border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>主体</TableHead>
              <TableHead>登录名</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>主部门</TableHead>
              <TableHead>更新时间</TableHead>
              <TableHead className="w-44 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {items.map((principal) => (
              <TableRow key={principal.id}>
                <TableCell>
                  <div className="flex items-center gap-3">
                    <span className="grid size-9 place-items-center rounded-full bg-primary/10 font-semibold text-primary">
                      {principal.display_name[0]?.toUpperCase()}
                    </span>
                    <div>
                      <strong className="block text-sm">
                        {principal.display_name}
                      </strong>
                      <span className="flex flex-wrap items-center gap-1.5 text-xs text-muted-foreground">
                        {principal.email || principal.id}
                        {principal.email && (
                          <Badge
                            variant={
                              principal.email_verified ? "secondary" : "outline"
                            }
                            className="px-1.5 py-0 text-[10px]"
                          >
                            {principal.email_verified ? "已验证" : "未验证"}
                          </Badge>
                        )}
                      </span>
                    </div>
                  </div>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {principal.login_name}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      principal.status === "active"
                        ? "secondary"
                        : "destructive"
                    }
                  >
                    {principal.status === "active" ? "启用" : "停用"}
                  </Badge>
                </TableCell>
                <TableCell
                  className="text-sm"
                  data-member-department={principal.id}
                >
                  {departments.find(
                    (department) =>
                      department.id ===
                      members.find((member) => member.subject === principal.id)
                        ?.department_id
                  )?.name ?? "未分配"}
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {new Date(principal.updated_at).toLocaleString()}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => {
                        setEditing(principal)
                        setDepartmentID(
                          members.find(
                            (member) => member.subject === principal.id
                          )?.department_id ?? ""
                        )
                        setOpen(true)
                      }}
                      aria-label={`编辑${principal.display_name}`}
                      title="编辑主体"
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant={
                        principal.status === "active"
                          ? "destructive"
                          : "outline"
                      }
                      size="sm"
                      disabled={busy}
                      onClick={() => void toggle(principal)}
                    >
                      {principal.status === "active" ? "停用" : "恢复"}
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {items.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <UserRoundCheck className="mb-2 size-7" />
            暂无本地主体
          </div>
        )}
      </div>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen)
          if (!nextOpen) {
            setEditing(null)
            setDepartmentID("")
          }
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑主体" : "创建本地主体"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "更新全局主体资料；邮箱变化会重置验证状态并撤销旧凭证。"
                : "同时创建密码凭证和当前租户成员关系。"}
            </DialogDescription>
          </DialogHeader>
          <form
            key={editing?.id ?? "create"}
            id="principal-form"
            className="grid gap-4"
            onSubmit={submit}
          >
            {!editing && <Field label="登录名" name="login_name" required />}
            <Field
              label="显示名称"
              name="display_name"
              defaultValue={editing?.display_name}
              required
            />
            {editing && editingMembership && (
              <div className="grid gap-2">
                <Label htmlFor="principal-department">主部门</Label>
                <SimpleSelect
                  id="principal-department"
                  value={departmentID || "__none__"}
                  onValueChange={(value) =>
                    setDepartmentID(value === "__none__" ? "" : value)
                  }
                  options={[
                    { value: "__none__", label: "未分配" },
                    ...departments
                      .filter(
                        (department) =>
                          department.status === "active" ||
                          department.id === departmentID
                      )
                      .map((department) => ({
                        value: department.id,
                        label:
                          department.status === "active"
                            ? department.name
                            : `${department.name}（停用）`,
                      })),
                  ]}
                />
              </div>
            )}
            <Field
              label="邮箱"
              name="email"
              type="email"
              defaultValue={editing?.email}
            />
            {!editing && (
              <Field
                label="初始密码"
                name="password"
                type="password"
                minLength={12}
                required
              />
            )}
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="principal-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function Field({
  label,
  name,
  type = "text",
  required = false,
  minLength,
  defaultValue,
}: {
  label: string
  name: string
  type?: string
  required?: boolean
  minLength?: number
  defaultValue?: string
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={name}>{label}</Label>
      <Input
        id={name}
        name={name}
        type={type}
        required={required}
        minLength={minLength}
        defaultValue={defaultValue}
        maxLength={type === "password" ? 1024 : 320}
        autoComplete={type === "password" ? "new-password" : "off"}
      />
    </div>
  )
}
