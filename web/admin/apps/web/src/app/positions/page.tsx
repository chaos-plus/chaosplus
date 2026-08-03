import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  BriefcaseBusiness,
  Pencil,
  Plus,
  Trash2,
  UserRoundCog,
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
import { Alert } from "../../components/alert"
import {
  DirectoryMembersDialog,
  type DirectoryMember,
} from "../../components/directory-members-dialog"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Member,
  type Position,
  type PositionInput,
  type PositionMember,
  type PositionMemberInput,
} from "../../lib/iam-api"

export default function PositionsPage() {
  const [positions, setPositions] = useState<Position[]>([])
  const [positionOpen, setPositionOpen] = useState(false)
  const [editingPosition, setEditingPosition] = useState<Position | null>(null)
  const [status, setStatus] = useState<PositionInput["status"]>("active")
  const [memberPosition, setMemberPosition] = useState<Position | null>(null)
  const [positionMembers, setPositionMembers] = useState<PositionMember[]>([])
  const [tenantMembers, setTenantMembers] = useState<Member[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(() => iamApi.positions().then(setPositions), [])

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const openCreate = () => {
    setEditingPosition(null)
    setStatus("active")
    setPositionOpen(true)
  }

  const openEdit = (position: Position) => {
    setEditingPosition(position)
    setStatus(position.status)
    setPositionOpen(true)
  }

  const submitPosition = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const input: PositionInput = {
      code: String(data.get("code")),
      name: String(data.get("name")),
      status,
      sort_order: Number(data.get("sort_order") || 0),
    }
    try {
      if (editingPosition)
        await iamApi.updatePosition(editingPosition.id, {
          ...input,
          version: editingPosition.version,
        })
      else await iamApi.createPosition(input)
      setPositionOpen(false)
      setEditingPosition(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removePosition = async (position: Position) => {
    setBusy(true)
    setError("")
    try {
      await iamApi.deletePosition(position.id, position.version)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openMembers = async (position: Position) => {
    setBusy(true)
    setError("")
    try {
      const [assigned, members] = await Promise.all([
        iamApi.positionMembers(position.id),
        iamApi.members(),
      ])
      setMemberPosition(position)
      setPositionMembers(assigned)
      setTenantMembers(members)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const putMember = async (
    principalID: string,
    input: PositionMemberInput
  ): Promise<boolean> => {
    if (!memberPosition) return false
    setBusy(true)
    setError("")
    try {
      await iamApi.putPositionMember(memberPosition.id, principalID, input)
      setPositionMembers(await iamApi.positionMembers(memberPosition.id))
      return true
    } catch (cause) {
      setError((cause as Error).message)
      return false
    } finally {
      setBusy(false)
    }
  }

  const removeMember = async (member: DirectoryMember): Promise<boolean> => {
    if (!memberPosition) return false
    setBusy(true)
    setError("")
    try {
      await iamApi.deletePositionMember(memberPosition.id, member.principal_id)
      setPositionMembers(await iamApi.positionMembers(memberPosition.id))
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
        title="岗位管理"
        description="维护当前租户的岗位目录和成员任职时间"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建岗位
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[760px]">
          <TableHeader>
            <TableRow>
              <TableHead>岗位</TableHead>
              <TableHead>编码</TableHead>
              <TableHead>排序</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>版本</TableHead>
              <TableHead className="w-32 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {positions.map((position) => (
              <TableRow key={position.id}>
                <TableCell>
                  <div className="flex min-w-0 items-center gap-2">
                    <BriefcaseBusiness className="size-4 shrink-0 text-primary" />
                    <strong className="truncate text-sm">
                      {position.name}
                    </strong>
                  </div>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {position.code}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {position.sort_order}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      position.status === "active" ? "secondary" : "destructive"
                    }
                  >
                    {position.status === "active" ? "启用" : "停用"}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  v{position.version}
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => void openMembers(position)}
                      aria-label={`管理${position.name}成员`}
                      title="管理成员"
                    >
                      <UserRoundCog />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => openEdit(position)}
                      aria-label={`编辑${position.name}`}
                      title="编辑岗位"
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => void removePosition(position)}
                      aria-label={`删除${position.name}`}
                      title="删除空岗位"
                    >
                      <Trash2 />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {positions.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <span className="text-center">
              <BriefcaseBusiness className="mx-auto mb-2 size-7" />
              当前租户暂无岗位
            </span>
          </div>
        )}
      </div>

      <Dialog
        open={positionOpen}
        onOpenChange={(open) => {
          setPositionOpen(open)
          if (!open) setEditingPosition(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {editingPosition ? "编辑岗位" : "创建岗位"}
            </DialogTitle>
            <DialogDescription>
              岗位编码在租户内唯一，保存时统一规范化为小写。
            </DialogDescription>
          </DialogHeader>
          <form
            key={editingPosition?.id ?? "create"}
            id="position-form"
            className="grid gap-4"
            onSubmit={submitPosition}
          >
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor="position-code">岗位编码</Label>
                <Input
                  id="position-code"
                  name="code"
                  defaultValue={editingPosition?.code}
                  pattern="[A-Za-z][A-Za-z0-9._-]*"
                  maxLength={64}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="position-name">岗位名称</Label>
                <Input
                  id="position-name"
                  name="name"
                  defaultValue={editingPosition?.name}
                  maxLength={128}
                  required
                />
              </div>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="position-sort-order">排序</Label>
                <Input
                  id="position-sort-order"
                  name="sort_order"
                  type="number"
                  min={0}
                  max={1000000}
                  defaultValue={editingPosition?.sort_order ?? 0}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="position-status">状态</Label>
                <SimpleSelect
                  id="position-status"
                  options={{ active: "启用", disabled: "停用" }}
                  value={status}
                  onValueChange={(value) =>
                    setStatus(value as PositionInput["status"])
                  }
                />
              </div>
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setPositionOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="position-form" disabled={busy}>
              {busy ? "保存中" : editingPosition ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {memberPosition && (
        <DirectoryMembersDialog
          key={`${memberPosition.id}:${positionMembers.map((member) => `${member.principal_id}:${member.starts_at}:${member.ends_at}`).join("|")}`}
          open
          itemName={memberPosition.name}
          directoryLabel="岗位"
          scheduleLabel="任职时间"
          formID="position-member-form"
          inputPrefix="position-member"
          members={positionMembers}
          tenantMembers={tenantMembers}
          busy={busy}
          onClose={() => setMemberPosition(null)}
          onError={setError}
          onPut={putMember}
          onDelete={removeMember}
        />
      )}
    </>
  )
}
