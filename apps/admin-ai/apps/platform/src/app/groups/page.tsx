import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  Eye,
  Pencil,
  Plus,
  Trash2,
  UserRoundCog,
  UsersRound,
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
import { Textarea } from "@workspace/ui/components/textarea"
import { Alert } from "../../components/alert"
import {
  DirectoryMembersDialog,
  type DirectoryMember,
} from "../../components/directory-members-dialog"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Group,
  type GroupInput,
  type GroupRuleCondition,
  type GroupRuleField,
  type GroupMember,
  type GroupMemberInput,
  type Member,
} from "../../lib/iam-api"

interface RuleConditionDraft {
  field: GroupRuleField
  operator: GroupRuleCondition["operator"]
  values: string
}

const newRuleCondition = (): RuleConditionDraft => ({
  field: "member.department_id",
  operator: "in",
  values: "",
})

export default function GroupsPage() {
  const [groups, setGroups] = useState<Group[]>([])
  const [groupOpen, setGroupOpen] = useState(false)
  const [editingGroup, setEditingGroup] = useState<Group | null>(null)
  const [status, setStatus] = useState<GroupInput["status"]>("active")
  const [groupType, setGroupType] = useState<GroupInput["type"]>("static")
  const [ruleMatch, setRuleMatch] = useState<"all" | "any">("all")
  const [ruleConditions, setRuleConditions] = useState<RuleConditionDraft[]>([
    newRuleCondition(),
  ])
  const [memberGroup, setMemberGroup] = useState<Group | null>(null)
  const [groupMembers, setGroupMembers] = useState<GroupMember[]>([])
  const [tenantMembers, setTenantMembers] = useState<Member[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(() => iamApi.groups().then(setGroups), [])

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const openCreate = () => {
    setEditingGroup(null)
    setStatus("active")
    setGroupType("static")
    setRuleMatch("all")
    setRuleConditions([newRuleCondition()])
    setGroupOpen(true)
  }

  const openEdit = (group: Group) => {
    setEditingGroup(group)
    setStatus(group.status)
    setGroupType(group.type)
    setRuleMatch(group.membership_rule?.match ?? "all")
    setRuleConditions(
      group.membership_rule?.conditions.map((condition) => ({
        ...condition,
        values: condition.values.join(", "),
      })) ?? [newRuleCondition()]
    )
    setGroupOpen(true)
  }

  const submitGroup = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const input: GroupInput = {
      name: String(data.get("name")),
      type: groupType,
      description: String(data.get("description")),
      status,
      sort_order: Number(data.get("sort_order") || 0),
      ...(groupType === "dynamic"
        ? {
            membership_rule: {
              version: 1,
              match: ruleMatch,
              conditions: ruleConditions.map((condition) => ({
                field: condition.field,
                operator: condition.operator,
                values: condition.values
                  .split(",")
                  .map((value) => value.trim())
                  .filter(Boolean),
              })),
            },
          }
        : {}),
    }
    try {
      if (editingGroup)
        await iamApi.updateGroup(editingGroup.id, {
          name: input.name,
          description: input.description,
          status: input.status,
          sort_order: input.sort_order,
          ...(input.membership_rule
            ? { membership_rule: input.membership_rule }
            : {}),
          version: editingGroup.version,
        })
      else await iamApi.createGroup(input)
      setGroupOpen(false)
      setEditingGroup(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removeGroup = async (group: Group) => {
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteGroup(group.id, group.version)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openMembers = async (group: Group) => {
    setBusy(true)
    setError("")
    try {
      const [assigned, members] = await Promise.all([
        iamApi.groupMembers(group.id),
        group.type === "static" ? iamApi.members() : Promise.resolve([]),
      ])
      setMemberGroup(group)
      setGroupMembers(assigned)
      setTenantMembers(members)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const putMember = async (
    principalID: string,
    input: GroupMemberInput
  ): Promise<boolean> => {
    if (!memberGroup) return false
    setBusy(true)
    setError("")
    try {
      await iamApi.putGroupMember(memberGroup.id, principalID, input)
      setGroupMembers(await iamApi.groupMembers(memberGroup.id))
      return true
    } catch (cause) {
      setError((cause as Error).message)
      return false
    } finally {
      setBusy(false)
    }
  }

  const removeMember = async (member: DirectoryMember): Promise<boolean> => {
    if (!memberGroup) return false
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteGroupMember(memberGroup.id, member.principal_id)
      setGroupMembers(await iamApi.groupMembers(memberGroup.id))
      return true
    } catch (cause) {
      setError((cause as Error).message)
      return false
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="用户组管理"
        description="维护当前租户的静态安全组和成员有效时间"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建用户组
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[920px]">
          <TableHeader>
            <TableRow>
              <TableHead>用户组</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>说明</TableHead>
              <TableHead>排序</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>版本</TableHead>
              <TableHead className="w-32 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.map((group) => (
              <TableRow key={group.id}>
                <TableCell>
                  <div className="flex min-w-0 items-center gap-2">
                    <UsersRound className="size-4 shrink-0 text-primary" />
                    <strong className="truncate text-sm">{group.name}</strong>
                  </div>
                </TableCell>
                <TableCell>
                  <Badge variant="outline">
                    {group.type === "dynamic" ? "动态" : "静态"}
                  </Badge>
                </TableCell>
                <TableCell className="max-w-72 truncate text-sm text-muted-foreground">
                  {group.description || "-"}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {group.sort_order}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      group.status === "active" ? "secondary" : "destructive"
                    }
                  >
                    {group.status === "active" ? "启用" : "停用"}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  v{group.version}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => void openMembers(group)}
                      aria-label={`${group.type === "dynamic" ? "查看" : "管理"}${group.name}成员`}
                      title={
                        group.type === "dynamic" ? "查看计算成员" : "管理成员"
                      }
                    >
                      {group.type === "dynamic" ? <Eye /> : <UserRoundCog />}
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => openEdit(group)}
                      aria-label={`编辑${group.name}`}
                      title="编辑用户组"
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => void removeGroup(group)}
                      aria-label={`删除${group.name}`}
                      title="删除空用户组"
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {groups.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <span className="text-center">
              <UsersRound className="mx-auto mb-2 size-7" />
              当前租户暂无用户组
            </span>
          </div>
        )}
      </div>

      <Dialog
        open={groupOpen}
        onOpenChange={(open) => {
          setGroupOpen(open)
          if (!open) setEditingGroup(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingGroup ? "编辑用户组" : "创建用户组"}
            </DialogTitle>
            <DialogDescription>
              用户组名称在租户内不区分大小写且必须唯一；动态组按当前成员属性实时计算。
            </DialogDescription>
          </DialogHeader>
          <form
            key={editingGroup?.id ?? "create"}
            id="group-form"
            className="grid gap-4"
            onSubmit={submitGroup}
          >
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="group-name">用户组名称</Label>
                <Input
                  id="group-name"
                  name="name"
                  defaultValue={editingGroup?.name}
                  maxLength={128}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="group-type">类型</Label>
                <SimpleSelect
                  id="group-type"
                  options={{ static: "静态用户组", dynamic: "动态用户组" }}
                  value={groupType}
                  onValueChange={(value) =>
                    setGroupType(value as GroupInput["type"])
                  }
                  disabled={Boolean(editingGroup)}
                />
              </div>
            </div>
            {groupType === "dynamic" && (
              <div className="grid gap-3 border-t pt-4">
                <div className="flex items-end justify-between gap-3">
                  <div className="grid flex-1 gap-2">
                    <Label htmlFor="group-rule-match">匹配方式</Label>
                    <SimpleSelect
                      id="group-rule-match"
                      options={{ all: "满足全部条件", any: "满足任一条件" }}
                      value={ruleMatch}
                      onValueChange={(value) =>
                        setRuleMatch(value as "all" | "any")
                      }
                    />
                  </div>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={() =>
                      setRuleConditions((current) => [
                        ...current,
                        newRuleCondition(),
                      ])
                    }
                    disabled={ruleConditions.length >= 16}
                  >
                    <Plus />
                    添加条件
                  </Button>
                </div>
                {ruleConditions.map((condition, index) => (
                  <div
                    key={index}
                    className="grid grid-cols-1 gap-2 sm:grid-cols-[1fr_8rem_1.2fr_2.5rem]"
                  >
                    <SimpleSelect
                      id={`group-rule-field-${index}`}
                      aria-label={`条件 ${index + 1} 属性`}
                      options={{
                        "member.subject": "主体 ID",
                        "member.email": "邮箱",
                        "member.email_domain": "邮箱域名",
                        "member.department_id": "部门 ID",
                        "member.status": "成员状态",
                      }}
                      value={condition.field}
                      onValueChange={(value) =>
                        setRuleConditions((current) =>
                          current.map((item, itemIndex) =>
                            itemIndex === index
                              ? { ...item, field: value as GroupRuleField }
                              : item
                          )
                        )
                      }
                    />
                    <SimpleSelect
                      id={`group-rule-operator-${index}`}
                      aria-label={`条件 ${index + 1} 操作符`}
                      options={{ in: "属于", not_in: "不属于" }}
                      value={condition.operator}
                      onValueChange={(value) =>
                        setRuleConditions((current) =>
                          current.map((item, itemIndex) =>
                            itemIndex === index
                              ? {
                                  ...item,
                                  operator:
                                    value as GroupRuleCondition["operator"],
                                }
                              : item
                          )
                        )
                      }
                    />
                    <Input
                      aria-label={`条件 ${index + 1} 值`}
                      value={condition.values}
                      onChange={(event) =>
                        setRuleConditions((current) =>
                          current.map((item, itemIndex) =>
                            itemIndex === index
                              ? { ...item, values: event.target.value }
                              : item
                          )
                        )
                      }
                      placeholder="多个值用逗号分隔"
                      required
                    />
                    <Button
                      type="button"
                      variant="ghost"
                      size="icon"
                      aria-label={`删除条件 ${index + 1}`}
                      title="删除条件"
                      disabled={ruleConditions.length === 1}
                      onClick={() =>
                        setRuleConditions((current) =>
                          current.filter((_, itemIndex) => itemIndex !== index)
                        )
                      }
                    >
                      <Trash2 />
                    </Button>
                  </div>
                ))}
              </div>
            )}
            <div className="grid gap-2">
              <Label htmlFor="group-description">说明</Label>
              <Textarea
                id="group-description"
                name="description"
                defaultValue={editingGroup?.description}
                maxLength={1024}
                rows={3}
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="group-sort-order">排序</Label>
                <Input
                  id="group-sort-order"
                  name="sort_order"
                  type="number"
                  min={0}
                  max={1000000}
                  defaultValue={editingGroup?.sort_order ?? 0}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="group-status">状态</Label>
                <SimpleSelect
                  id="group-status"
                  options={{ active: "启用", disabled: "停用" }}
                  value={status}
                  onValueChange={(value) =>
                    setStatus(value as GroupInput["status"])
                  }
                />
              </div>
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setGroupOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="group-form" disabled={busy}>
              {busy ? "保存中" : editingGroup ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {memberGroup && (
        <DirectoryMembersDialog
          key={`${memberGroup.id}:${groupMembers.map((member) => `${member.principal_id}:${member.starts_at}:${member.ends_at}`).join("|")}`}
          open
          itemName={memberGroup.name}
          directoryLabel="用户组"
          scheduleLabel="有效时间"
          formID="group-member-form"
          inputPrefix="group-member"
          members={groupMembers}
          tenantMembers={tenantMembers}
          busy={busy}
          computed={memberGroup.type === "dynamic"}
          onClose={() => setMemberGroup(null)}
          onError={setError}
          onPut={putMember}
          onDelete={removeMember}
        />
      )}
    </>
  )
}
