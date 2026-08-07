import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react"
import {
  Building2,
  ChevronDown,
  ChevronRight,
  KeyRound,
  Pencil,
  Plus,
  ScanSearch,
  Trash2,
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
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type AuthorizationExplanation,
  type DataConstraint,
  type Entity,
  type EntityInput,
  type EntityRoleBinding,
  type Group,
  type Member,
  type Permission,
  type Position,
  type Relationship,
  type RelationshipRelation,
  type RelationshipSubjectType,
  type Role,
} from "../../lib/iam-api"
import {
  trustedContextCondition,
  trustedContextConditionSummary,
} from "../../lib/policy-condition"

const rootEntity = "__root__"

async function loadRelationships(entityID: string) {
  return (
    await Promise.all([
      iamApi.relationships(entityID),
      iamApi.resourceRelationships(entityID),
    ])
  ).flat()
}

export default function EntitiesPage() {
  const [entities, setEntities] = useState<Entity[]>([])
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Entity | null>(null)
  const [parentID, setParentID] = useState(rootEntity)
  const [status, setStatus] = useState<EntityInput["status"]>("active")
  const [bindingEntity, setBindingEntity] = useState<Entity | null>(null)
  const [bindings, setBindings] = useState<EntityRoleBinding[]>([])
  const [relationships, setRelationships] = useState<Relationship[]>([])
  const [roles, setRoles] = useState<Role[]>([])
  const [members, setMembers] = useState<Member[]>([])
  const [groups, setGroups] = useState<Group[]>([])
  const [positions, setPositions] = useState<Position[]>([])
  const [permissions, setPermissions] = useState<Permission[]>([])
  const [roleID, setRoleID] = useState("")
  const [principalID, setPrincipalID] = useState("")
  const [effect, setEffect] = useState<EntityRoleBinding["effect"]>("allow")
  const [expiresAt, setExpiresAt] = useState("")
  const [relationshipSubjectType, setRelationshipSubjectType] =
    useState<RelationshipSubjectType>("principal")
  const [relationshipSubjectID, setRelationshipSubjectID] = useState("")
  const [relationshipSubjectRelation, setRelationshipSubjectRelation] =
    useState<RelationshipRelation>("owner")
  const [relationshipRelation, setRelationshipRelation] =
    useState<RelationshipRelation>("viewer")
  const [relationshipResourceType, setRelationshipResourceType] = useState("")
  const [relationshipResourceID, setRelationshipResourceID] = useState("")
  const [relationshipStartsAt, setRelationshipStartsAt] = useState("")
  const [relationshipEndsAt, setRelationshipEndsAt] = useState("")
  const [relationshipMinimumAcr, setRelationshipMinimumAcr] = useState("")
  const [relationshipClientID, setRelationshipClientID] = useState("")
  const [relationshipNetworkZone, setRelationshipNetworkZone] = useState("")
  const [relationshipTimeStart, setRelationshipTimeStart] = useState("")
  const [relationshipTimeEnd, setRelationshipTimeEnd] = useState("")
  const [relationshipTimezone, setRelationshipTimezone] = useState(
    () => Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC"
  )
  const [inspectionPermission, setInspectionPermission] = useState("")
  const [inspectionResourceID, setInspectionResourceID] = useState("")
  const [inspection, setInspection] = useState<{
    constraint: DataConstraint
    explanation: AuthorizationExplanation
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(() => iamApi.entities().then(setEntities), [])

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const entityByID = useMemo(
    () => new Map(entities.map((entity) => [entity.id, entity])),
    [entities]
  )
  const tree = useMemo(
    () => flattenEntities(entities, collapsed),
    [collapsed, entities]
  )
  const childCounts = useMemo(() => {
    const counts = new Map<string, number>()
    for (const entity of entities) {
      if (entity.parent_id)
        counts.set(entity.parent_id, (counts.get(entity.parent_id) ?? 0) + 1)
    }
    return counts
  }, [entities])
  const parentOptions = useMemo(
    () => [
      { value: rootEntity, label: "顶级实体" },
      ...flattenEntities(entities, new Set())
        .filter(
          ({ entity }) =>
            entity.id !== editing?.id &&
            !isDescendantOf(entity, editing?.id, entityByID)
        )
        .map(({ entity, depth }) => ({
          value: entity.id,
          label: `${"　".repeat(depth)}${entity.name}`,
        })),
    ],
    [editing?.id, entities, entityByID]
  )
  const roleByID = useMemo(
    () => new Map(roles.map((role) => [role.id, role])),
    [roles]
  )
  const memberByID = useMemo(
    () => new Map(members.map((member) => [member.subject, member])),
    [members]
  )
  const groupByID = useMemo(
    () => new Map(groups.map((group) => [group.id, group])),
    [groups]
  )
  const positionByID = useMemo(
    () => new Map(positions.map((position) => [position.id, position])),
    [positions]
  )
  const resourceTypeOptions = useMemo(
    () =>
      [...new Set(permissions.map((permission) => permission.resource))].map(
        (resource) => ({ value: resource, label: resource })
      ),
    [permissions]
  )
  const relationshipSubjectOptions = useMemo(() => {
    switch (relationshipSubjectType) {
      case "group":
        return groups.map((group) => ({ value: group.id, label: group.name }))
      case "position":
        return positions.map((position) => ({
          value: position.id,
          label: position.name,
        }))
      case "entity":
        return entities
          .filter(
            (entity) =>
              entity.status === "active" && entity.id !== bindingEntity?.id
          )
          .map((entity) => ({ value: entity.id, label: entity.name }))
      default:
        return members.map((member) => ({
          value: member.subject,
          label: member.display_name || member.subject,
        }))
    }
  }, [
    bindingEntity?.id,
    entities,
    groups,
    members,
    positions,
    relationshipSubjectType,
  ])

  const openCreate = () => {
    setError("")
    setEditing(null)
    setParentID(rootEntity)
    setStatus("active")
    setOpen(true)
  }

  const openEdit = (entity: Entity) => {
    setError("")
    setEditing(entity)
    setParentID(entity.parent_id || rootEntity)
    setStatus(entity.status)
    setOpen(true)
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    try {
      const input: EntityInput = {
        parent_id: parentID === rootEntity ? "" : parentID,
        type: String(data.get("type")),
        name: String(data.get("name")),
        status,
        metadata: parseMetadata(String(data.get("metadata") || "{}")),
      }
      if (editing) await iamApi.updateEntity(editing.id, input)
      else await iamApi.createEntity(input)
      setOpen(false)
      setEditing(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (entity: Entity) => {
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteEntity(entity.id)
      setCollapsed((current) => {
        const next = new Set(current)
        next.delete(entity.id)
        return next
      })
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openBindings = async (entity: Entity) => {
    setBusy(true)
    setError("")
    try {
      const [
        nextBindings,
        nextRelationships,
        nextRoles,
        nextMembers,
        nextGroups,
        nextPositions,
        nextPermissions,
      ] = await Promise.all([
        iamApi.entityRoleBindings(entity.id),
        loadRelationships(entity.id),
        iamApi.roles(),
        iamApi.members(),
        iamApi.groups(),
        iamApi.positions(),
        iamApi.permissions(),
      ])
      const activeMembers = nextMembers.filter(
        (member) => member.status === "active"
      )
      const dataPermissions = nextPermissions.filter(
        (permission) => permission.data_scoped
      )
      setBindingEntity(entity)
      setBindings(nextBindings)
      setRelationships(nextRelationships)
      setRoles(nextRoles)
      setMembers(activeMembers)
      setGroups(nextGroups.filter((group) => group.status === "active"))
      setPositions(
        nextPositions.filter((position) => position.status === "active")
      )
      setPermissions(dataPermissions)
      setRoleID(nextRoles[0]?.id ?? "")
      setPrincipalID(activeMembers[0]?.subject ?? "")
      setEffect("allow")
      setExpiresAt("")
      setRelationshipSubjectType("principal")
      setRelationshipSubjectID(activeMembers[0]?.subject ?? "")
      setRelationshipSubjectRelation("owner")
      setRelationshipRelation("viewer")
      setRelationshipResourceType(dataPermissions[0]?.resource ?? "")
      setRelationshipResourceID("")
      setRelationshipStartsAt("")
      setRelationshipEndsAt("")
      setRelationshipMinimumAcr("")
      setRelationshipClientID("")
      setRelationshipNetworkZone("")
      setRelationshipTimeStart("")
      setRelationshipTimeEnd("")
      setInspectionPermission(dataPermissions[0]?.code ?? "")
      setInspectionResourceID("")
      setInspection(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const putRelationship = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!bindingEntity || !relationshipSubjectID) return
    setBusy(true)
    setError("")
    try {
      const resourceID = relationshipResourceID.trim()
      await iamApi.putRelationship({
        entity_id: resourceID ? bindingEntity.id : undefined,
        subject_type: relationshipSubjectType,
        subject_id: relationshipSubjectID,
        subject_relation:
          relationshipSubjectType === "principal"
            ? undefined
            : relationshipSubjectType === "entity"
              ? relationshipSubjectRelation
              : "member",
        relation: relationshipRelation,
        resource_type: resourceID
          ? relationshipResourceType
          : bindingEntity.type,
        resource_id: resourceID || bindingEntity.id,
        starts_at: relationshipStartsAt
          ? new Date(relationshipStartsAt).toISOString()
          : undefined,
        ends_at: relationshipEndsAt
          ? new Date(relationshipEndsAt).toISOString()
          : undefined,
        condition: trustedContextCondition(
          relationshipMinimumAcr,
          relationshipClientID,
          relationshipNetworkZone,
          relationshipTimeStart,
          relationshipTimeEnd,
          relationshipTimezone
        ),
      })
      setRelationships(await loadRelationships(bindingEntity.id))
      setRelationshipStartsAt("")
      setRelationshipEndsAt("")
      setRelationshipMinimumAcr("")
      setRelationshipClientID("")
      setRelationshipNetworkZone("")
      setRelationshipTimeStart("")
      setRelationshipTimeEnd("")
      setInspection(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removeRelationship = async (relationship: Relationship) => {
    if (!bindingEntity) return
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteRelationship(relationship)
      setRelationships(await loadRelationships(bindingEntity.id))
      setInspection(null)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const relationshipSubjectLabel = (relationship: Relationship) => {
    switch (relationship.subject_type) {
      case "group":
        return (
          groupByID.get(relationship.subject_id)?.name ??
          relationship.subject_id
        )
      case "position":
        return (
          positionByID.get(relationship.subject_id)?.name ??
          relationship.subject_id
        )
      case "entity":
        return (
          entityByID.get(relationship.subject_id)?.name ??
          relationship.subject_id
        )
      default:
        return (
          memberByID.get(relationship.subject_id)?.display_name ??
          relationship.subject_id
        )
    }
  }

  const putBinding = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!bindingEntity || !roleID || !principalID) return
    setBusy(true)
    setError("")
    try {
      await iamApi.putEntityRoleBinding(bindingEntity.id, roleID, principalID, {
        effect,
        expires_at: expiresAt ? new Date(expiresAt).toISOString() : undefined,
      })
      setBindings(await iamApi.entityRoleBindings(bindingEntity.id))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removeBinding = async (binding: EntityRoleBinding) => {
    if (!bindingEntity) return
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteEntityRoleBinding(
        bindingEntity.id,
        binding.role_id,
        binding.principal_id
      )
      setBindings(await iamApi.entityRoleBindings(bindingEntity.id))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const inspectAuthorization = async () => {
    if (!bindingEntity || !principalID || !inspectionPermission) return
    setBusy(true)
    setError("")
    try {
      const resourceID = inspectionResourceID.trim()
      const resourceType = permissions.find(
        (permission) => permission.code === inspectionPermission
      )?.resource
      const [constraint, explanation] = await Promise.all([
        iamApi.authorizationConstraint(inspectionPermission, principalID),
        resourceID && resourceType
          ? iamApi.explainResourceAuthorization(
              bindingEntity.id,
              resourceType,
              resourceID,
              inspectionPermission,
              principalID
            )
          : iamApi.explainEntityAuthorization(
              bindingEntity.id,
              inspectionPermission,
              principalID
            ),
      ])
      setInspection({ constraint, explanation })
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const toggleCollapsed = (id: string) => {
    setCollapsed((current) => {
      const next = new Set(current)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  return (
    <>
      <PageHeader
        title="实体管理"
        description="维护租户下公司、企业、商户、门店等业务承载实体及其直接角色授权"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建实体
          </Button>
        }
      />
      {error && !open && !bindingEntity && <Alert>{error}</Alert>}
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[900px]">
          <TableHeader>
            <TableRow>
              <TableHead>实体</TableHead>
              <TableHead>类型</TableHead>
              <TableHead>上级实体</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>元数据</TableHead>
              <TableHead className="w-32 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tree.map(({ entity, depth }) => {
              const hasChildren = (childCounts.get(entity.id) ?? 0) > 0
              return (
                <TableRow key={entity.id}>
                  <TableCell>
                    <div
                      className="flex min-w-0 items-center gap-1"
                      style={{ paddingInlineStart: `${depth * 24}px` }}
                    >
                      {hasChildren ? (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 shrink-0"
                          onClick={() => toggleCollapsed(entity.id)}
                          aria-label={`${collapsed.has(entity.id) ? "展开" : "收起"}${entity.name}`}
                          title={
                            collapsed.has(entity.id)
                              ? "展开子实体"
                              : "收起子实体"
                          }
                        >
                          {collapsed.has(entity.id) ? (
                            <ChevronRight />
                          ) : (
                            <ChevronDown />
                          )}
                        </Button>
                      ) : (
                        <span className="block size-8 shrink-0" />
                      )}
                      <Building2 className="size-4 shrink-0 text-primary" />
                      <strong className="truncate text-sm">
                        {entity.name}
                      </strong>
                    </div>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline">{entity.type}</Badge>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {entity.parent_id
                      ? (entityByID.get(entity.parent_id)?.name ??
                        entity.parent_id)
                      : "-"}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        entity.status === "active" ? "secondary" : "destructive"
                      }
                    >
                      {entity.status === "active" ? "启用" : "停用"}
                    </Badge>
                  </TableCell>
                  <TableCell className="max-w-72 truncate font-mono text-xs text-muted-foreground">
                    {metadataSummary(entity.metadata)}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => void openBindings(entity)}
                        aria-label={`管理${entity.name}的角色绑定`}
                        title="角色绑定"
                      >
                        <KeyRound />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => openEdit(entity)}
                        aria-label={`编辑${entity.name}`}
                        title="编辑实体"
                      >
                        <Pencil />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => void remove(entity)}
                        aria-label={`删除${entity.name}`}
                        title="删除空实体"
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              )
            })}
          </TableBody>
        </Table>
        {entities.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <span className="text-center">
              <Building2 className="mx-auto mb-2 size-7" />
              当前租户暂无实体
            </span>
          </div>
        )}
      </div>

      <Dialog
        open={open}
        onOpenChange={(nextOpen) => {
          setOpen(nextOpen)
          if (!nextOpen) setEditing(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑实体" : "创建实体"}</DialogTitle>
            <DialogDescription>
              同级实体的类型和名称必须唯一；类型使用小写字母、数字、下划线或连字符。
            </DialogDescription>
          </DialogHeader>
          {error && <Alert>{error}</Alert>}
          <form
            key={editing?.id ?? "create"}
            id="entity-form"
            className="grid gap-4"
            onSubmit={submit}
          >
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="entity-name">名称</Label>
                <Input
                  id="entity-name"
                  name="name"
                  defaultValue={editing?.name}
                  required
                  maxLength={200}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="entity-type">类型</Label>
                <Input
                  id="entity-type"
                  name="type"
                  defaultValue={editing?.type}
                  pattern="[a-z][a-z0-9_-]{0,63}"
                  placeholder="company"
                  required
                  maxLength={64}
                />
              </div>
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="entity-parent">上级实体</Label>
                <SimpleSelect
                  id="entity-parent"
                  options={parentOptions}
                  value={parentID}
                  onValueChange={setParentID}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="entity-status">状态</Label>
                <SimpleSelect
                  id="entity-status"
                  options={{ active: "启用", disabled: "停用" }}
                  value={status}
                  onValueChange={(value) =>
                    setStatus(value as EntityInput["status"])
                  }
                />
              </div>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="entity-metadata">元数据 JSON</Label>
              <Textarea
                id="entity-metadata"
                name="metadata"
                className="font-mono text-xs"
                defaultValue={JSON.stringify(editing?.metadata ?? {}, null, 2)}
                rows={5}
                spellCheck={false}
              />
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="entity-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(bindingEntity)}
        onOpenChange={(nextOpen) => {
          if (!nextOpen) {
            setBindingEntity(null)
            setInspection(null)
          }
        }}
      >
        <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-4xl">
          <DialogHeader>
            <DialogTitle className="pr-8 break-all">
              {bindingEntity?.name} · 授权与角色绑定
            </DialogTitle>
            <DialogDescription>
              管理实体作用域角色与关系授权；显式拒绝始终优先于关系允许。
            </DialogDescription>
          </DialogHeader>
          {error && <Alert>{error}</Alert>}
          <form
            id="entity-binding-form"
            className="grid grid-cols-1 gap-3 md:grid-cols-4"
            onSubmit={putBinding}
          >
            <div className="grid gap-2">
              <Label htmlFor="entity-binding-role">角色</Label>
              <SimpleSelect
                id="entity-binding-role"
                options={roles.map((role) => ({
                  value: role.id,
                  label: role.name,
                }))}
                value={roleID}
                onValueChange={setRoleID}
                placeholder="无可用角色"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="entity-binding-principal">成员</Label>
              <SimpleSelect
                id="entity-binding-principal"
                options={members.map((member) => ({
                  value: member.subject,
                  label: member.display_name || member.subject,
                }))}
                value={principalID}
                onValueChange={setPrincipalID}
                placeholder="无有效成员"
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="entity-binding-effect">效果</Label>
              <SimpleSelect
                id="entity-binding-effect"
                options={{ allow: "允许", deny: "拒绝" }}
                value={effect}
                onValueChange={(value) =>
                  setEffect(value as EntityRoleBinding["effect"])
                }
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="entity-binding-expiry">到期时间</Label>
              <Input
                id="entity-binding-expiry"
                type="datetime-local"
                value={expiresAt}
                onChange={(event) => setExpiresAt(event.target.value)}
              />
            </div>
          </form>
          <div className="overflow-x-auto rounded-md border">
            <Table className="min-w-[720px]">
              <TableHeader>
                <TableRow>
                  <TableHead>角色</TableHead>
                  <TableHead>成员</TableHead>
                  <TableHead>效果</TableHead>
                  <TableHead>到期时间</TableHead>
                  <TableHead className="w-16 text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {bindings.map((binding) => (
                  <TableRow key={`${binding.role_id}:${binding.principal_id}`}>
                    <TableCell>
                      {roleByID.get(binding.role_id)?.name ?? binding.role_id}
                    </TableCell>
                    <TableCell>
                      {memberByID.get(binding.principal_id)?.display_name ??
                        binding.principal_id}
                    </TableCell>
                    <TableCell>
                      <Badge
                        variant={
                          binding.effect === "allow"
                            ? "secondary"
                            : "destructive"
                        }
                      >
                        {binding.effect === "allow" ? "允许" : "拒绝"}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground">
                      {binding.expires_at
                        ? new Date(binding.expires_at).toLocaleString()
                        : "长期有效"}
                    </TableCell>
                    <TableCell className="text-right">
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => void removeBinding(binding)}
                        aria-label="删除角色绑定"
                        title="删除角色绑定"
                      >
                        <Trash2 />
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {bindings.length === 0 && (
              <div className="grid min-h-24 place-items-center text-sm text-muted-foreground">
                暂无直接角色绑定
              </div>
            )}
          </div>
          <section
            className="border-t pt-5"
            aria-labelledby="relationship-title"
          >
            <h3 id="relationship-title" className="mb-3 text-sm font-semibold">
              关系授权
            </h3>
            <form
              id="entity-relationship-form"
              className="grid grid-cols-1 items-end gap-3 md:grid-cols-2 xl:grid-cols-4"
              onSubmit={putRelationship}
            >
              <div className="grid gap-2">
                <Label htmlFor="relationship-subject-type">主体类型</Label>
                <SimpleSelect
                  id="relationship-subject-type"
                  options={{
                    principal: "成员",
                    group: "用户组",
                    position: "岗位",
                    entity: "实体",
                  }}
                  value={relationshipSubjectType}
                  onValueChange={(value) => {
                    const type = value as RelationshipSubjectType
                    setRelationshipSubjectType(type)
                    setRelationshipSubjectID("")
                  }}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-subject">主体</Label>
                <SimpleSelect
                  id="relationship-subject"
                  options={relationshipSubjectOptions}
                  value={relationshipSubjectID}
                  onValueChange={setRelationshipSubjectID}
                  placeholder="无可用主体"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-subject-relation">主体关系</Label>
                {relationshipSubjectType === "entity" ? (
                  <SimpleSelect
                    id="relationship-subject-relation"
                    options={{
                      owner: "所有者",
                      editor: "编辑者",
                      viewer: "查看者",
                    }}
                    value={relationshipSubjectRelation}
                    onValueChange={(value) =>
                      setRelationshipSubjectRelation(
                        value as RelationshipRelation
                      )
                    }
                  />
                ) : (
                  <Input
                    id="relationship-subject-relation"
                    value={
                      relationshipSubjectType === "principal"
                        ? "直接主体"
                        : "member"
                    }
                    disabled
                  />
                )}
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-relation">授予关系</Label>
                <SimpleSelect
                  id="relationship-relation"
                  options={{
                    owner: "所有者",
                    editor: "编辑者",
                    viewer: "查看者",
                  }}
                  value={relationshipRelation}
                  onValueChange={(value) =>
                    setRelationshipRelation(value as RelationshipRelation)
                  }
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-resource-type">业务资源类型</Label>
                <SimpleSelect
                  id="relationship-resource-type"
                  options={resourceTypeOptions}
                  value={relationshipResourceType}
                  onValueChange={setRelationshipResourceType}
                  placeholder="选择类型"
                  disabled={!relationshipResourceID}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-resource-id">业务资源 ID</Label>
                <Input
                  id="relationship-resource-id"
                  value={relationshipResourceID}
                  onChange={(event) =>
                    setRelationshipResourceID(event.target.value)
                  }
                  placeholder="留空表示当前实体"
                  maxLength={255}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-starts-at">生效时间</Label>
                <Input
                  id="relationship-starts-at"
                  type="datetime-local"
                  value={relationshipStartsAt}
                  onChange={(event) =>
                    setRelationshipStartsAt(event.target.value)
                  }
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-ends-at">到期时间</Label>
                <Input
                  id="relationship-ends-at"
                  type="datetime-local"
                  value={relationshipEndsAt}
                  onChange={(event) =>
                    setRelationshipEndsAt(event.target.value)
                  }
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-minimum-acr">最低认证级别</Label>
                <Input
                  id="relationship-minimum-acr"
                  type="number"
                  min="1"
                  max="99"
                  value={relationshipMinimumAcr}
                  onChange={(event) =>
                    setRelationshipMinimumAcr(event.target.value)
                  }
                  placeholder="不限"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-client-id">OAuth 客户端</Label>
                <Input
                  id="relationship-client-id"
                  value={relationshipClientID}
                  onChange={(event) =>
                    setRelationshipClientID(event.target.value)
                  }
                  placeholder="不限"
                  maxLength={255}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-network-zone">网络区域</Label>
                <Input
                  id="relationship-network-zone"
                  value={relationshipNetworkZone}
                  onChange={(event) =>
                    setRelationshipNetworkZone(event.target.value)
                  }
                  placeholder="不限"
                  maxLength={255}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-time-start">每日开始时间</Label>
                <Input
                  id="relationship-time-start"
                  type="time"
                  value={relationshipTimeStart}
                  onChange={(event) =>
                    setRelationshipTimeStart(event.target.value)
                  }
                  required={Boolean(relationshipTimeEnd)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-time-end">每日结束时间</Label>
                <Input
                  id="relationship-time-end"
                  type="time"
                  value={relationshipTimeEnd}
                  onChange={(event) =>
                    setRelationshipTimeEnd(event.target.value)
                  }
                  required={Boolean(relationshipTimeStart)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="relationship-timezone">时区</Label>
                <Input
                  id="relationship-timezone"
                  value={relationshipTimezone}
                  onChange={(event) =>
                    setRelationshipTimezone(event.target.value)
                  }
                  required={Boolean(
                    relationshipTimeStart || relationshipTimeEnd
                  )}
                  maxLength={64}
                />
              </div>
              <Button
                type="submit"
                className="md:col-span-2 xl:col-span-4 xl:justify-self-end"
                disabled={
                  busy ||
                  !relationshipSubjectID ||
                  (Boolean(relationshipResourceID.trim()) &&
                    !relationshipResourceType)
                }
              >
                <Plus />
                授予关系
              </Button>
            </form>
            <div className="mt-3 overflow-x-auto rounded-md border">
              <Table className="min-w-[1160px]">
                <TableHeader>
                  <TableRow>
                    <TableHead>主体类型</TableHead>
                    <TableHead>主体</TableHead>
                    <TableHead>主体关系</TableHead>
                    <TableHead>授予关系</TableHead>
                    <TableHead>目标</TableHead>
                    <TableHead>有效期</TableHead>
                    <TableHead>条件</TableHead>
                    <TableHead className="w-16 text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {relationships.map((relationship) => (
                    <TableRow
                      key={`${relationship.entity_id}:${relationship.subject_type}:${relationship.subject_id}:${relationship.subject_relation}:${relationship.relation}:${relationship.resource_type}:${relationship.resource_id}`}
                    >
                      <TableCell>{relationship.subject_type}</TableCell>
                      <TableCell>
                        {relationshipSubjectLabel(relationship)}
                      </TableCell>
                      <TableCell>
                        {relationship.subject_relation || "直接"}
                      </TableCell>
                      <TableCell>
                        <Badge variant="secondary">
                          {relationship.relation}
                        </Badge>
                      </TableCell>
                      <TableCell className="max-w-64 font-mono text-xs break-all text-muted-foreground">
                        {relationship.entity_id
                          ? `${relationship.resource_type}:${relationship.resource_id}`
                          : (entityByID.get(relationship.resource_id)?.name ??
                            `${relationship.resource_type}:${relationship.resource_id}`)}
                      </TableCell>
                      <TableCell
                        className="text-xs whitespace-nowrap text-muted-foreground"
                        data-relationship-window
                      >
                        {relationship.starts_at
                          ? new Date(relationship.starts_at).toLocaleString()
                          : "立即生效"}
                        {" 至 "}
                        {relationship.ends_at
                          ? new Date(relationship.ends_at).toLocaleString()
                          : "长期有效"}
                      </TableCell>
                      <TableCell
                        className="max-w-72 text-xs text-muted-foreground"
                        data-relationship-condition
                      >
                        {trustedContextConditionSummary(relationship.condition)}
                      </TableCell>
                      <TableCell className="text-right">
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled={busy}
                          onClick={() => void removeRelationship(relationship)}
                          aria-label="撤销关系"
                          title="撤销关系"
                        >
                          <Trash2 />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
              {relationships.length === 0 && (
                <div className="grid min-h-20 place-items-center text-sm text-muted-foreground">
                  暂无关系授权
                </div>
              )}
            </div>
          </section>
          <section
            className="border-t pt-5"
            aria-labelledby="authorization-inspection-title"
          >
            <h3
              id="authorization-inspection-title"
              className="mb-3 flex items-center gap-2 text-sm font-semibold"
            >
              <ScanSearch className="size-4 text-primary" />
              授权解释
            </h3>
            <div className="grid grid-cols-1 items-end gap-3 md:grid-cols-2 xl:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_minmax(0,1fr)_auto]">
              <div className="grid gap-2">
                <Label htmlFor="authorization-inspection-principal">成员</Label>
                <SimpleSelect
                  id="authorization-inspection-principal"
                  options={members.map((member) => ({
                    value: member.subject,
                    label: member.display_name || member.subject,
                  }))}
                  value={principalID}
                  onValueChange={(value) => {
                    setPrincipalID(value)
                    setInspection(null)
                  }}
                  placeholder="无有效成员"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="authorization-inspection-permission">
                  数据权限
                </Label>
                <SimpleSelect
                  id="authorization-inspection-permission"
                  options={permissions.map((permission) => ({
                    value: permission.code,
                    label: `${permission.code} · ${permission.summary}`,
                  }))}
                  value={inspectionPermission}
                  onValueChange={(value) => {
                    setInspectionPermission(value)
                    setInspection(null)
                  }}
                  placeholder="无数据权限"
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="authorization-inspection-resource">
                  业务资源 ID
                </Label>
                <Input
                  id="authorization-inspection-resource"
                  value={inspectionResourceID}
                  onChange={(event) => {
                    setInspectionResourceID(event.target.value)
                    setInspection(null)
                  }}
                  placeholder="留空检查当前实体"
                  maxLength={255}
                />
              </div>
              <Button
                type="button"
                variant="outline"
                disabled={busy || !principalID || !inspectionPermission}
                onClick={() => void inspectAuthorization()}
              >
                <ScanSearch />
                计算授权
              </Button>
            </div>
            {inspection && (
              <div className="mt-4" data-authorization-explanation>
                <div className="mb-3 flex flex-wrap items-center gap-2 text-sm">
                  <Badge
                    variant={
                      inspection.explanation.allowed
                        ? "secondary"
                        : "destructive"
                    }
                  >
                    {inspection.explanation.allowed ? "允许" : "拒绝"}
                  </Badge>
                  <span>
                    {authorizationReason(inspection.explanation.reason)}
                  </span>
                  <span className="text-muted-foreground">
                    策略版本 {inspection.explanation.revision}
                  </span>
                  <span className="text-muted-foreground">
                    {inspection.constraint.allow_all
                      ? `全租户范围，排除 ${inspection.constraint.denied_ids.length} 个实体`
                      : `允许 ${inspection.constraint.resource_ids.length} 个实体，拒绝 ${inspection.constraint.denied_ids.length} 个实体`}
                  </span>
                </div>
                <div className="overflow-x-auto rounded-md border">
                  <Table className="min-w-[760px]">
                    <TableHeader>
                      <TableRow>
                        <TableHead>权限</TableHead>
                        <TableHead>角色</TableHead>
                        <TableHead>来源</TableHead>
                        <TableHead>作用域</TableHead>
                        <TableHead>关系路径</TableHead>
                        <TableHead>效果</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {inspection.explanation.matches.map((match, index) => (
                        <TableRow
                          key={`${match.role_id}:${match.scope_type}:${match.scope_id}:${match.effect}:${index}`}
                        >
                          <TableCell className="font-mono text-xs">
                            {match.permission_code}
                          </TableCell>
                          <TableCell>
                            {roleByID.get(match.role_id)?.name ?? match.role_id}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {match.source_type}
                          </TableCell>
                          <TableCell className="max-w-80 text-xs break-all text-muted-foreground">
                            {match.path
                              ?.map(
                                (step) =>
                                  `${step.subject_type}:${step.subject_id}${step.subject_relation ? `#${step.subject_relation}` : ""} → ${step.relation} → ${step.resource_type}:${step.resource_id}`
                              )
                              .join(" / ") ?? "-"}
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {match.scope_type}:{match.scope_id}
                            {match.inherited ? "（继承）" : ""}
                          </TableCell>
                          <TableCell>
                            <Badge
                              variant={
                                match.effect === "allow"
                                  ? "secondary"
                                  : "destructive"
                              }
                            >
                              {match.effect === "allow" ? "允许" : "拒绝"}
                            </Badge>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                  {inspection.explanation.matches.length === 0 && (
                    <div className="grid min-h-20 place-items-center text-sm text-muted-foreground">
                      没有匹配的授权来源
                    </div>
                  )}
                </div>
              </div>
            )}
          </section>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBindingEntity(null)}>
              关闭
            </Button>
            <Button
              type="submit"
              form="entity-binding-form"
              disabled={busy || !roleID || !principalID}
            >
              {busy ? "保存中" : "保存绑定"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function flattenEntities(
  entities: Entity[],
  collapsed: Set<string>
): Array<{ entity: Entity; depth: number }> {
  const children = new Map<string, Entity[]>()
  for (const entity of entities) {
    const parentID = entity.parent_id ?? ""
    const siblings = children.get(parentID) ?? []
    siblings.push(entity)
    children.set(parentID, siblings)
  }
  for (const siblings of children.values())
    siblings.sort(
      (left, right) =>
        left.type.localeCompare(right.type) ||
        left.name.localeCompare(right.name) ||
        left.id.localeCompare(right.id)
    )
  const result: Array<{ entity: Entity; depth: number }> = []
  const visit = (parentID: string, depth: number) => {
    for (const entity of children.get(parentID) ?? []) {
      result.push({ entity, depth })
      if (!collapsed.has(entity.id)) visit(entity.id, depth + 1)
    }
  }
  visit("", 0)
  return result
}

function isDescendantOf(
  candidate: Entity,
  ancestorID: string | undefined,
  entities: Map<string, Entity>
): boolean {
  if (!ancestorID) return false
  let parentID = candidate.parent_id
  while (parentID) {
    if (parentID === ancestorID) return true
    parentID = entities.get(parentID)?.parent_id
  }
  return false
}

function parseMetadata(raw: string): Record<string, unknown> {
  let value: unknown
  try {
    value = JSON.parse(raw || "{}")
  } catch {
    throw new Error("元数据必须是有效的 JSON 对象")
  }
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("元数据必须是 JSON 对象")
  return value as Record<string, unknown>
}

function metadataSummary(metadata: Record<string, unknown>): string {
  return Object.keys(metadata).length ? JSON.stringify(metadata) : "-"
}

function authorizationReason(
  reason: AuthorizationExplanation["reason"]
): string {
  return {
    inactive_membership: "租户成员未启用",
    inactive_tenant: "租户未启用",
    inactive_resource: "实体已停用或不存在",
    explicit_deny: "命中显式拒绝",
    administrator_scope_denied: "管理员作用域被拒绝",
    permission_grant: "命中权限授权",
    administrator_grant: "命中管理员兜底授权",
    relationship_grant: "命中关系授权",
    no_matching_grant: "没有匹配的授权",
  }[reason]
}
