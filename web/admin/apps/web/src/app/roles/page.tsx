import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
  type ReactNode,
} from "react"
import {
  BriefcaseBusiness,
  Database,
  KeyRound,
  Pencil,
  Plus,
  SlidersHorizontal,
  Trash2,
  UsersRound,
} from "lucide-react"
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
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Group,
  type DataScope,
  type Department,
  type Member,
  type Permission,
  type Position,
  type Role,
  type RoleDirectoryBinding,
  type RoleDataScope,
  type RolePermissionGrant,
} from "../../lib/iam-api"
import {
  trustedContextCondition,
  trustedContextConditionDraft,
  trustedContextConditionSummary,
  type TrustedContextConditionDraft,
} from "../../lib/policy-condition"

export default function RolesPage() {
  const [roles, setRoles] = useState<Role[]>([])
  const [catalog, setCatalog] = useState<Permission[]>([])
  const [selected, setSelected] = useState<Role | null>(null)
  const [details, setDetails] = useState<{
    roleID: string
    grants: RolePermissionGrant[]
    boundMembers: string[]
    members: Member[]
    bindings: RoleDirectoryBinding[]
    groups: Group[]
    positions: Position[]
    dataScope: RoleDataScope
    departments: Department[]
  }>({
    roleID: "",
    grants: [],
    boundMembers: [],
    members: [],
    bindings: [],
    groups: [],
    positions: [],
    dataScope: { role_id: "", scope: "all", department_ids: [] },
    departments: [],
  })
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Role | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [scopeDraft, setScopeDraft] = useState<DataScope>("all")
  const [scopeDepartmentIDs, setScopeDepartmentIDs] = useState<string[]>([])
  const [conditionPermission, setConditionPermission] =
    useState<Permission | null>(null)
  const [conditionDraft, setConditionDraft] =
    useState<TrustedContextConditionDraft>(() =>
      trustedContextConditionDraft(undefined, localTimezone())
    )

  const refresh = useCallback(
    (preferredID?: string) =>
      Promise.all([iamApi.roles(), iamApi.permissions()]).then(
        ([nextRoles, nextCatalog]) => {
          setRoles(nextRoles)
          setCatalog(nextCatalog)
          setSelected((current) => {
            const id = preferredID ?? current?.id
            return (
              nextRoles.find((role) => role.id === id) ?? nextRoles[0] ?? null
            )
          })
        }
      ),
    []
  )

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  useEffect(() => {
    if (!selected) return
    let current = true
    void Promise.all([
      iamApi.rolePermissionGrants(selected.id),
      iamApi.roleMembers(selected.id),
      iamApi.members(),
      iamApi.roleDirectoryBindings(selected.id),
      iamApi.groups(),
      iamApi.positions(),
      iamApi.roleDataScope(selected.id),
      iamApi.departments(),
    ])
      .then(
        ([
          nextGrants,
          nextBoundMembers,
          nextMembers,
          nextBindings,
          nextGroups,
          nextPositions,
          nextDataScope,
          nextDepartments,
        ]) => {
          if (!current) return
          setDetails({
            roleID: selected.id,
            grants: nextGrants,
            boundMembers: nextBoundMembers,
            members: nextMembers,
            bindings: nextBindings,
            groups: nextGroups,
            positions: nextPositions,
            dataScope: nextDataScope,
            departments: nextDepartments,
          })
          setScopeDraft(nextDataScope.scope)
          setScopeDepartmentIDs(nextDataScope.department_ids)
        }
      )
      .catch((cause: Error) => {
        if (current) setError(cause.message)
      })
    return () => {
      current = false
    }
  }, [selected])

  const loadingDetails = details.roleID !== selected?.id
  const grants = loadingDetails ? [] : details.grants
  const boundMembers = loadingDetails ? [] : details.boundMembers
  const members = loadingDetails ? [] : details.members
  const directoryBindings = loadingDetails ? [] : details.bindings
  const directoryGroups = loadingDetails ? [] : details.groups
  const directoryPositions = loadingDetails ? [] : details.positions
  const departments = loadingDetails ? [] : details.departments
  const conditionGrant = grants.find(
    (grant) => grant.permission_code === conditionPermission?.code
  )
  const condition = trustedContextCondition(
    conditionDraft.minimumAcr,
    conditionDraft.clientID,
    conditionDraft.networkZone,
    conditionDraft.timeStart,
    conditionDraft.timeEnd,
    conditionDraft.timezone
  )

  const permissionGroups = useMemo(
    () =>
      catalog.reduce<Record<string, Permission[]>>((result, permission) => {
        const permissions = result[permission.resource] ?? []
        permissions.push(permission)
        result[permission.resource] = permissions
        return result
      }, {}),
    [catalog]
  )

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const body = {
      name: String(data.get("name")),
      description: String(data.get("description")),
    }
    try {
      const role = editing
        ? await iamApi.updateRole(editing.id, body)
        : await iamApi.createRole(body)
      setOpen(false)
      setEditing(null)
      await refresh(role.id)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const togglePermission = async (code: string, checked: boolean) => {
    if (!selected) return
    setBusy(true)
    setError("")
    try {
      if (checked) await iamApi.grantPermission(selected.id, code)
      else await iamApi.revokePermission(selected.id, code)
      const nextGrants = await iamApi.rolePermissionGrants(selected.id)
      setDetails((current) =>
        current.roleID === selected.id
          ? { ...current, grants: nextGrants }
          : current
      )
      if (!checked && conditionPermission?.code === code)
        setConditionPermission(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openPermissionCondition = (permission: Permission) => {
    const current = grants.find(
      (grant) => grant.permission_code === permission.code
    )
    setConditionDraft(
      trustedContextConditionDraft(current?.condition, localTimezone())
    )
    setConditionPermission(permission)
  }

  const savePermissionCondition = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!selected || !conditionPermission || !condition) return
    setBusy(true)
    setError("")
    try {
      await iamApi.setRolePermissionCondition(
        selected.id,
        conditionPermission.code,
        condition
      )
      const nextGrants = await iamApi.rolePermissionGrants(selected.id)
      setDetails((current) =>
        current.roleID === selected.id
          ? { ...current, grants: nextGrants }
          : current
      )
      setConditionPermission(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const clearPermissionCondition = async () => {
    if (!selected || !conditionPermission) return
    setBusy(true)
    setError("")
    try {
      await iamApi.clearRolePermissionCondition(
        selected.id,
        conditionPermission.code
      )
      const nextGrants = await iamApi.rolePermissionGrants(selected.id)
      setDetails((current) =>
        current.roleID === selected.id
          ? { ...current, grants: nextGrants }
          : current
      )
      setConditionPermission(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const toggleMember = async (subject: string, checked: boolean) => {
    if (!selected) return
    setBusy(true)
    setError("")
    try {
      if (checked) await iamApi.addRoleMember(selected.id, subject)
      else await iamApi.removeRoleMember(selected.id, subject)
      const nextBoundMembers = await iamApi.roleMembers(selected.id)
      setDetails((current) =>
        current.roleID === selected.id
          ? { ...current, boundMembers: nextBoundMembers }
          : current
      )
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const toggleDirectoryBinding = async (
    assigneeType: RoleDirectoryBinding["assignee_type"],
    assigneeID: string,
    checked: boolean
  ) => {
    if (!selected) return
    setBusy(true)
    setError("")
    try {
      if (checked)
        await iamApi.addRoleDirectoryBinding(
          selected.id,
          assigneeType,
          assigneeID
        )
      else
        await iamApi.removeRoleDirectoryBinding(
          selected.id,
          assigneeType,
          assigneeID
        )
      const nextBindings = await iamApi.roleDirectoryBindings(selected.id)
      setDetails((current) =>
        current.roleID === selected.id
          ? { ...current, bindings: nextBindings }
          : current
      )
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!selected) return
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteRole(selected.id)
      setSelected(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const saveDataScope = async () => {
    if (!selected) return
    setBusy(true)
    setError("")
    try {
      const dataScope = await iamApi.setRoleDataScope(selected.id, {
        scope: scopeDraft,
        department_ids:
          scopeDraft === "selected_departments" ? scopeDepartmentIDs : [],
      })
      setDetails((current) =>
        current.roleID === selected.id ? { ...current, dataScope } : current
      )
      setScopeDepartmentIDs(dataScope.department_ids)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="角色权限"
        description="按租户管理角色、成员与权限目录绑定"
        action={
          <Button
            onClick={() => {
              setEditing(null)
              setOpen(true)
            }}
          >
            <Plus />
            创建角色
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="grid min-h-[620px] overflow-hidden rounded-md border bg-card lg:grid-cols-[280px_minmax(0,1fr)]">
        <aside className="border-b lg:border-r lg:border-b-0">
          <header className="flex h-12 items-center justify-between border-b bg-muted/30 px-4 text-sm font-semibold">
            角色列表
            <span className="text-xs font-normal text-muted-foreground">
              {roles.length}
            </span>
          </header>
          {roles.map((role) => (
            <button
              key={role.id}
              className={`grid min-h-16 w-full grid-cols-[34px_minmax(0,1fr)] items-center gap-3 border-b px-3 text-left hover:bg-muted/40 ${selected?.id === role.id ? "bg-primary/5 shadow-[inset_3px_0_var(--primary)]" : ""}`}
              onClick={() => {
                setConditionPermission(null)
                setSelected(role)
              }}
            >
              <span className="grid size-8 place-items-center rounded-md bg-primary/10 text-primary">
                <KeyRound size={16} />
              </span>
              <span className="min-w-0">
                <strong className="block truncate text-sm">{role.name}</strong>
                <small className="mt-1 block truncate text-xs text-muted-foreground">
                  {role.description || role.id}
                </small>
              </span>
            </button>
          ))}
        </aside>
        <section className="min-w-0">
          {selected ? (
            <>
              <header className="flex min-h-20 flex-wrap items-center justify-between gap-4 border-b px-5 py-3">
                <div>
                  <h2 className="font-semibold">{selected.name}</h2>
                  <p className="mt-1 text-xs text-muted-foreground">
                    {selected.description || "未填写描述"}
                  </p>
                </div>
                <div className="flex gap-2">
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={busy}
                    onClick={() => {
                      setEditing(selected)
                      setOpen(true)
                    }}
                  >
                    <Pencil />
                    编辑
                  </Button>
                  <Button
                    variant="destructive"
                    size="sm"
                    disabled={busy}
                    onClick={() => void remove()}
                  >
                    <Trash2 />
                    删除角色
                  </Button>
                </div>
              </header>
              <div className="p-5">
                <section className="mb-7" data-role-data-scope>
                  <h3 className="mb-3 flex items-center gap-2 border-b pb-2 text-xs font-semibold text-muted-foreground uppercase">
                    <Database size={15} />
                    数据范围
                  </h3>
                  {loadingDetails ? (
                    <p className="text-sm text-muted-foreground">
                      加载数据范围中
                    </p>
                  ) : (
                    <div className="grid gap-4">
                      <div className="grid max-w-md gap-2">
                        <Label htmlFor="role-data-scope">范围</Label>
                        <SimpleSelect
                          id="role-data-scope"
                          value={scopeDraft}
                          disabled={busy}
                          onValueChange={(value) =>
                            setScopeDraft(value as DataScope)
                          }
                          options={{
                            all: "全部数据",
                            self: "本人数据",
                            department: "本部门",
                            department_and_descendants: "本部门及下级部门",
                            selected_departments: "指定部门",
                          }}
                        />
                      </div>
                      {scopeDraft === "selected_departments" && (
                        <div
                          className="grid gap-2 md:grid-cols-2"
                          data-scope-departments
                        >
                          {departments.map((department) => {
                            const checked = scopeDepartmentIDs.includes(
                              department.id
                            )
                            return (
                              <label
                                key={department.id}
                                className="flex min-h-12 items-center gap-3 rounded-md border p-3 hover:bg-muted/30"
                              >
                                <Checkbox
                                  data-scope-department-id={department.id}
                                  checked={checked}
                                  disabled={
                                    busy ||
                                    (department.status !== "active" && !checked)
                                  }
                                  onCheckedChange={(value) =>
                                    setScopeDepartmentIDs((current) =>
                                      value === true
                                        ? [
                                            ...new Set([
                                              ...current,
                                              department.id,
                                            ]),
                                          ]
                                        : current.filter(
                                            (id) => id !== department.id
                                          )
                                    )
                                  }
                                />
                                <span className="min-w-0 flex-1 truncate text-sm">
                                  {department.name}
                                </span>
                                {department.status !== "active" && (
                                  <Badge variant="destructive">停用</Badge>
                                )}
                              </label>
                            )
                          })}
                        </div>
                      )}
                      <div>
                        <Button
                          size="sm"
                          disabled={busy}
                          onClick={() => void saveDataScope()}
                          data-save-data-scope
                        >
                          保存数据范围
                        </Button>
                      </div>
                    </div>
                  )}
                </section>
                <section className="mb-7">
                  <h3 className="mb-3 flex items-center gap-2 border-b pb-2 text-xs font-semibold text-muted-foreground uppercase">
                    <UsersRound size={15} />
                    角色成员
                  </h3>
                  {loadingDetails ? (
                    <p className="text-sm text-muted-foreground">加载成员中</p>
                  ) : members.length === 0 ? (
                    <p className="text-sm text-muted-foreground">
                      当前租户暂无可分配成员
                    </p>
                  ) : (
                    <div className="grid gap-2 md:grid-cols-2">
                      {members.map((member) => (
                        <label
                          key={member.subject}
                          className="flex min-h-14 items-center gap-3 rounded-md border p-3 hover:bg-muted/30"
                        >
                          <Checkbox
                            checked={boundMembers.includes(member.subject)}
                            disabled={busy || member.status !== "active"}
                            onCheckedChange={(value) =>
                              void toggleMember(member.subject, value === true)
                            }
                          />
                          <span className="min-w-0 flex-1">
                            <strong className="block truncate text-sm">
                              {member.display_name}
                            </strong>
                            <small className="mt-1 block truncate text-xs text-muted-foreground">
                              {member.email || member.subject}
                            </small>
                          </span>
                          {member.status !== "active" && (
                            <Badge variant="destructive">停用</Badge>
                          )}
                        </label>
                      ))}
                    </div>
                  )}
                </section>
                <DirectoryBindingsSection
                  title="用户组"
                  kind="group"
                  icon={<UsersRound size={15} />}
                  loading={loadingDetails}
                  busy={busy}
                  items={directoryGroups.map((group) => ({
                    id: group.id,
                    name: group.name,
                    detail: group.description || "静态用户组",
                    status: group.status,
                  }))}
                  boundIDs={directoryBindings
                    .filter((binding) => binding.assignee_type === "group")
                    .map((binding) => binding.assignee_id)}
                  onToggle={toggleDirectoryBinding}
                />
                <DirectoryBindingsSection
                  title="岗位"
                  kind="position"
                  icon={<BriefcaseBusiness size={15} />}
                  loading={loadingDetails}
                  busy={busy}
                  items={directoryPositions.map((position) => ({
                    id: position.id,
                    name: position.name,
                    detail: position.code,
                    status: position.status,
                  }))}
                  boundIDs={directoryBindings
                    .filter((binding) => binding.assignee_type === "position")
                    .map((binding) => binding.assignee_id)}
                  onToggle={toggleDirectoryBinding}
                />
                <section>
                  <h3 className="mb-3 border-b pb-2 text-xs font-semibold text-muted-foreground uppercase">
                    权限授权
                  </h3>
                  {Object.entries(permissionGroups).map(
                    ([resource, permissions]) => (
                      <section key={resource} className="mb-6">
                        <h4 className="mb-3 text-xs font-semibold text-muted-foreground uppercase">
                          {resource}
                        </h4>
                        <div className="grid gap-2 md:grid-cols-2">
                          {permissions.map((permission) => {
                            const grant = grants.find(
                              (item) => item.permission_code === permission.code
                            )
                            return (
                              <div
                                key={permission.code}
                                className="grid min-h-14 grid-cols-[auto_minmax(0,1fr)] items-center gap-x-3 gap-y-2 rounded-md border p-3 hover:bg-muted/30 sm:grid-cols-[auto_minmax(0,1fr)_auto]"
                                data-permission-code={permission.code}
                              >
                                <Checkbox
                                  aria-label={`授予 ${permission.summary}`}
                                  checked={Boolean(grant)}
                                  disabled={busy || loadingDetails}
                                  onCheckedChange={(value) =>
                                    void togglePermission(
                                      permission.code,
                                      value === true
                                    )
                                  }
                                />
                                <span className="min-w-0 flex-1">
                                  <strong className="block text-sm">
                                    {permission.summary}
                                  </strong>
                                  <small className="mt-1 block font-mono text-xs text-muted-foreground">
                                    {permission.code}
                                  </small>
                                  {grant?.condition && (
                                    <small
                                      className="mt-1 block text-xs break-words text-muted-foreground"
                                      data-permission-condition-summary
                                    >
                                      {trustedContextConditionSummary(
                                        grant.condition
                                      )}
                                    </small>
                                  )}
                                </span>
                                {grant && (
                                  <Button
                                    type="button"
                                    variant="ghost"
                                    size="sm"
                                    className="col-start-2 justify-self-start sm:col-start-3 sm:row-start-1 sm:justify-self-end"
                                    disabled={busy}
                                    onClick={() =>
                                      openPermissionCondition(permission)
                                    }
                                    data-edit-permission-condition={
                                      permission.code
                                    }
                                  >
                                    <SlidersHorizontal />
                                    条件
                                  </Button>
                                )}
                              </div>
                            )
                          })}
                        </div>
                      </section>
                    )
                  )}
                </section>
              </div>
            </>
          ) : (
            <div className="grid min-h-96 place-items-center text-sm text-muted-foreground">
              选择或创建一个角色
            </div>
          )}
        </section>
      </div>
      <Dialog
        open={Boolean(conditionPermission)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) setConditionPermission(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>设置权限条件</DialogTitle>
            <DialogDescription>
              {conditionPermission
                ? `${conditionPermission.summary} (${conditionPermission.code})`
                : "限制权限只在可信请求上下文满足时生效。"}
              {conditionGrant?.condition && (
                <span className="mt-1 block" data-current-condition-summary>
                  当前条件：
                  {trustedContextConditionSummary(conditionGrant.condition)}
                </span>
              )}
            </DialogDescription>
          </DialogHeader>
          <form
            id="role-permission-condition-form"
            className="grid gap-4 md:grid-cols-2"
            onSubmit={savePermissionCondition}
          >
            <div className="grid gap-2">
              <Label htmlFor="permission-minimum-acr">最低认证级别</Label>
              <Input
                id="permission-minimum-acr"
                type="number"
                min="1"
                max="99"
                value={conditionDraft.minimumAcr}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    minimumAcr: event.target.value,
                  }))
                }
                placeholder="不限"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="permission-client-id">OAuth 客户端</Label>
              <Input
                id="permission-client-id"
                value={conditionDraft.clientID}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    clientID: event.target.value,
                  }))
                }
                placeholder="不限"
                maxLength={255}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="permission-network-zone">网络区域</Label>
              <Input
                id="permission-network-zone"
                value={conditionDraft.networkZone}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    networkZone: event.target.value,
                  }))
                }
                placeholder="不限"
                maxLength={255}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="permission-timezone">IANA 时区</Label>
              <Input
                id="permission-timezone"
                value={conditionDraft.timezone}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    timezone: event.target.value,
                  }))
                }
                required={Boolean(
                  conditionDraft.timeStart || conditionDraft.timeEnd
                )}
                maxLength={64}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="permission-time-start">每日开始时间</Label>
              <Input
                id="permission-time-start"
                type="time"
                value={conditionDraft.timeStart}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    timeStart: event.target.value,
                  }))
                }
                required={Boolean(conditionDraft.timeEnd)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="permission-time-end">每日结束时间</Label>
              <Input
                id="permission-time-end"
                type="time"
                value={conditionDraft.timeEnd}
                onChange={(event) =>
                  setConditionDraft((current) => ({
                    ...current,
                    timeEnd: event.target.value,
                  }))
                }
                required={Boolean(conditionDraft.timeStart)}
              />
            </div>
          </form>
          <DialogFooter>
            {conditionGrant?.condition && (
              <Button
                variant="destructive"
                disabled={busy}
                onClick={() => void clearPermissionCondition()}
                data-clear-permission-condition
              >
                清除条件
              </Button>
            )}
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => setConditionPermission(null)}
            >
              取消
            </Button>
            <Button
              type="submit"
              form="role-permission-condition-form"
              disabled={busy || !condition}
              data-save-permission-condition
            >
              {busy ? "保存中" : "保存条件"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen)
          if (!nextOpen) setEditing(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑角色" : "创建租户角色"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "角色资料更新后立即生效，不改变现有成员和权限。"
                : "角色创建后可绑定成员并从权限目录中按需授权。"}
            </DialogDescription>
          </DialogHeader>
          <form
            key={editing?.id ?? "create"}
            id="role-form"
            className="grid gap-4"
            onSubmit={submit}
          >
            <div className="grid gap-2">
              <Label htmlFor="role-name">角色名称</Label>
              <Input
                id="role-name"
                name="name"
                defaultValue={editing?.name}
                required
                maxLength={128}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="role-description">描述</Label>
              <Input
                id="role-description"
                name="description"
                defaultValue={editing?.description}
                maxLength={4096}
              />
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="role-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

interface DirectoryOption {
  id: string
  name: string
  detail: string
  status: "active" | "disabled"
}

function localTimezone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
}

function DirectoryBindingsSection({
  title,
  kind,
  icon,
  items,
  boundIDs,
  loading,
  busy,
  onToggle,
}: {
  title: string
  kind: RoleDirectoryBinding["assignee_type"]
  icon: ReactNode
  items: DirectoryOption[]
  boundIDs: string[]
  loading: boolean
  busy: boolean
  onToggle: (
    kind: RoleDirectoryBinding["assignee_type"],
    id: string,
    checked: boolean
  ) => void
}) {
  return (
    <section className="mb-7" data-directory-section={kind}>
      <h3 className="mb-3 flex items-center gap-2 border-b pb-2 text-xs font-semibold text-muted-foreground uppercase">
        {icon}
        {title}
      </h3>
      {loading ? (
        <p className="text-sm text-muted-foreground">加载中</p>
      ) : items.length === 0 ? (
        <p className="text-sm text-muted-foreground">暂无可分配项</p>
      ) : (
        <div className="grid gap-2 md:grid-cols-2">
          {items.map((item) => {
            const checked = boundIDs.includes(item.id)
            return (
              <label
                key={item.id}
                className="flex min-h-14 items-center gap-3 rounded-md border p-3 hover:bg-muted/30"
              >
                <Checkbox
                  data-assignee-type={kind}
                  data-assignee-id={item.id}
                  aria-label={`${title} ${item.name}`}
                  checked={checked}
                  disabled={busy || (item.status !== "active" && !checked)}
                  onCheckedChange={(value) =>
                    onToggle(kind, item.id, value === true)
                  }
                />
                <span className="min-w-0 flex-1">
                  <strong className="block truncate text-sm">
                    {item.name}
                  </strong>
                  <small className="mt-1 block truncate text-xs text-muted-foreground">
                    {item.detail}
                  </small>
                </span>
                {item.status !== "active" && (
                  <Badge variant="destructive">停用</Badge>
                )}
              </label>
            )
          })}
        </div>
      )}
    </section>
  )
}
