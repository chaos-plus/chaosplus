import { useMemo, useState, type FormEvent } from "react"
import { Pencil, Trash2 } from "lucide-react"
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
import type { Member, MembershipWindowInput } from "../lib/iam-api"
import {
  membershipState,
  membershipWindow,
  toDateTimeLocal,
  toISOString,
  type MembershipDates,
} from "../lib/membership-presentation"

export interface DirectoryMember extends MembershipDates {
  principal_id: string
  display_name: string
  email?: string
}

interface DirectoryMembersDialogProps {
  open: boolean
  itemName: string
  directoryLabel: string
  scheduleLabel: string
  formID: string
  inputPrefix: string
  members: DirectoryMember[]
  tenantMembers: Member[]
  busy: boolean
  computed?: boolean
  onClose: () => void
  onError: (message: string) => void
  onPut: (principalID: string, input: MembershipWindowInput) => Promise<boolean>
  onDelete: (member: DirectoryMember) => Promise<boolean>
}

export function DirectoryMembersDialog({
  open,
  itemName,
  directoryLabel,
  scheduleLabel,
  formID,
  inputPrefix,
  members,
  tenantMembers,
  busy,
  computed = false,
  onClose,
  onError,
  onPut,
  onDelete,
}: DirectoryMembersDialogProps) {
  const firstAvailable = () =>
    tenantMembers.find(
      (member) =>
        member.status === "active" &&
        !members.some((item) => item.principal_id === member.subject)
    )?.subject ?? ""
  const [editing, setEditing] = useState<DirectoryMember | null>(null)
  const [selectedPrincipal, setSelectedPrincipal] = useState(firstAvailable)
  const availableMembers = useMemo(() => {
    const assigned = new Set(members.map((member) => member.principal_id))
    return tenantMembers.filter(
      (member) =>
        member.status === "active" &&
        (!assigned.has(member.subject) ||
          member.subject === editing?.principal_id)
    )
  }, [editing?.principal_id, members, tenantMembers])

  const editMember = (member: DirectoryMember) => {
    setEditing(member)
    setSelectedPrincipal(member.principal_id)
  }

  const cancelEdit = () => {
    setEditing(null)
    setSelectedPrincipal(firstAvailable())
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!selectedPrincipal) return
    const data = new FormData(event.currentTarget)
    const startsAt = toISOString(String(data.get("starts_at")))
    const endsAt = toISOString(String(data.get("ends_at")))
    if (startsAt && endsAt && Date.parse(endsAt) <= Date.parse(startsAt)) {
      onError("结束时间必须晚于开始时间")
      return
    }
    const saved = await onPut(selectedPrincipal, {
      ...(startsAt ? { starts_at: startsAt } : {}),
      ...(endsAt ? { ends_at: endsAt } : {}),
    })
    if (saved) cancelEdit()
  }

  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="max-h-[calc(100dvh-2rem)] max-w-3xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle className="pr-8">
            {itemName} · {directoryLabel}成员
          </DialogTitle>
          <DialogDescription>
            {computed
              ? `成员由${directoryLabel}规则和当前成员属性实时计算。`
              : `只有当前租户的启用成员可以分配到${directoryLabel}。`}
          </DialogDescription>
        </DialogHeader>
        <div className="max-h-64 overflow-auto rounded-md border">
          <Table className={computed ? "w-full" : "min-w-[640px]"}>
            <TableHeader>
              <TableRow>
                <TableHead>成员</TableHead>
                {!computed && <TableHead>{scheduleLabel}</TableHead>}
                <TableHead>状态</TableHead>
                {!computed && (
                  <TableHead className="w-20 text-right">操作</TableHead>
                )}
              </TableRow>
            </TableHeader>
            <TableBody>
              {members.map((member) => (
                <TableRow key={member.principal_id}>
                  <TableCell>
                    <div className="font-medium">{member.display_name}</div>
                    <div className="font-mono text-xs text-muted-foreground">
                      {member.principal_id}
                    </div>
                  </TableCell>
                  {!computed && (
                    <TableCell className="text-xs text-muted-foreground">
                      {membershipWindow(member)}
                    </TableCell>
                  )}
                  <TableCell>
                    {computed ? (
                      <Badge variant="secondary">实时</Badge>
                    ) : (
                      <Badge
                        variant={
                          membershipState(member) === "有效"
                            ? "secondary"
                            : "outline"
                        }
                      >
                        {membershipState(member)}
                      </Badge>
                    )}
                  </TableCell>
                  {!computed && (
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1">
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled={busy}
                          onClick={() => editMember(member)}
                          aria-label={`编辑${member.display_name}${scheduleLabel}`}
                          title={`编辑${scheduleLabel}`}
                        >
                          <Pencil />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon"
                          disabled={busy}
                          onClick={() => void onDelete(member)}
                          aria-label={`移除${member.display_name}`}
                          title="移除成员"
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </TableCell>
                  )}
                </TableRow>
              ))}
            </TableBody>
          </Table>
          {members.length === 0 && (
            <div className="grid min-h-24 place-items-center text-sm text-muted-foreground">
              暂无{directoryLabel}成员
            </div>
          )}
        </div>
        {!computed && (
          <form
            key={editing?.principal_id ?? "assign"}
            id={formID}
            className="grid gap-4"
            onSubmit={submit}
          >
            <div className="grid gap-2">
              <Label htmlFor={inputPrefix}>租户成员</Label>
              <SimpleSelect
                id={inputPrefix}
                options={availableMembers.map((member) => ({
                  value: member.subject,
                  label: member.display_name || member.subject,
                }))}
                value={selectedPrincipal}
                onValueChange={setSelectedPrincipal}
                disabled={Boolean(editing)}
              />
            </div>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div className="grid gap-2">
                <Label htmlFor={`${inputPrefix}-start`}>开始时间</Label>
                <Input
                  id={`${inputPrefix}-start`}
                  name="starts_at"
                  type="datetime-local"
                  defaultValue={toDateTimeLocal(editing?.starts_at)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor={`${inputPrefix}-end`}>结束时间</Label>
                <Input
                  id={`${inputPrefix}-end`}
                  name="ends_at"
                  type="datetime-local"
                  defaultValue={toDateTimeLocal(editing?.ends_at)}
                />
              </div>
            </div>
          </form>
        )}
        {!computed && (
          <DialogFooter>
            {editing && (
              <Button variant="outline" disabled={busy} onClick={cancelEdit}>
                取消编辑
              </Button>
            )}
            <Button
              type="submit"
              form={formID}
              disabled={busy || !selectedPrincipal}
            >
              {busy ? "保存中" : editing ? "更新时间" : "分配成员"}
            </Button>
          </DialogFooter>
        )}
      </DialogContent>
    </Dialog>
  )
}
