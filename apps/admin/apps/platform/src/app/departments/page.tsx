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
  Pencil,
  Plus,
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
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type Department,
  type DepartmentInput,
} from "../../lib/iam-api"

const rootDepartment = "__root__"

export default function DepartmentsPage() {
  const [departments, setDepartments] = useState<Department[]>([])
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set())
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Department | null>(null)
  const [parentID, setParentID] = useState(rootDepartment)
  const [status, setStatus] = useState<DepartmentInput["status"]>("active")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")

  const refresh = useCallback(
    () => iamApi.departments().then(setDepartments),
    []
  )

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const childrenByParent = useMemo(() => {
    const result = new Map<string, number>()
    for (const department of departments) {
      const parent = department.parent_id ?? ""
      result.set(parent, (result.get(parent) ?? 0) + 1)
    }
    return result
  }, [departments])

  const departmentByID = useMemo(
    () => new Map(departments.map((department) => [department.id, department])),
    [departments]
  )

  const visibleDepartments = useMemo(
    () => filterCollapsedDepartments(departments, collapsed),
    [collapsed, departments]
  )

  const parentOptions = useMemo(
    () => [
      { value: rootDepartment, label: "顶级部门" },
      ...departments
        .filter(
          (candidate) =>
            candidate.id !== editing?.id &&
            !isDescendantOf(candidate, editing?.id, departmentByID)
        )
        .map((department) => ({
          value: department.id,
          label: `${"　".repeat(department.depth)}${department.name}`,
        })),
    ],
    [departmentByID, departments, editing?.id]
  )

  const openCreate = () => {
    setEditing(null)
    setParentID(rootDepartment)
    setStatus("active")
    setOpen(true)
  }

  const openEdit = (department: Department) => {
    setEditing(department)
    setParentID(department.parent_id || rootDepartment)
    setStatus(department.status)
    setOpen(true)
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const input: DepartmentInput = {
      parent_id: parentID === rootDepartment ? "" : parentID,
      name: String(data.get("name")),
      status,
      sort_order: Number(data.get("sort_order") || 0),
    }
    try {
      if (editing)
        await iamApi.updateDepartment(editing.id, {
          ...input,
          version: editing.version,
        })
      else await iamApi.createDepartment(input)
      setOpen(false)
      setEditing(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (department: Department) => {
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteDepartment(department.id, department.version)
      setCollapsed((current) => {
        const next = new Set(current)
        next.delete(department.id)
        return next
      })
      await refresh()
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
        title="部门管理"
        description="维护当前租户的行政组织树，部门变更会立即推进授权策略版本"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建部门
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[720px]">
          <TableHeader>
            <TableRow>
              <TableHead>部门</TableHead>
              <TableHead>上级部门</TableHead>
              <TableHead>排序</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>版本</TableHead>
              <TableHead className="w-24 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {visibleDepartments.map((department) => {
              const hasChildren = (childrenByParent.get(department.id) ?? 0) > 0
              const parent = department.parent_id
                ? departmentByID.get(department.parent_id)
                : undefined
              return (
                <TableRow key={department.id}>
                  <TableCell>
                    <div
                      className="flex min-w-0 items-center gap-1"
                      style={{
                        paddingInlineStart: `${department.depth * 24}px`,
                      }}
                    >
                      {hasChildren ? (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="size-8 shrink-0"
                          onClick={() => toggleCollapsed(department.id)}
                          aria-label={`${collapsed.has(department.id) ? "展开" : "收起"}${department.name}`}
                          title={
                            collapsed.has(department.id)
                              ? "展开子部门"
                              : "收起子部门"
                          }
                        >
                          {collapsed.has(department.id) ? (
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
                        {department.name}
                      </strong>
                    </div>
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {parent?.name ?? "-"}
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    {department.sort_order}
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        department.status === "active"
                          ? "secondary"
                          : "destructive"
                      }
                    >
                      {department.status === "active" ? "启用" : "停用"}
                    </Badge>
                  </TableCell>
                  <TableCell className="font-mono text-xs">
                    v{department.version}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => openEdit(department)}
                        aria-label={`编辑${department.name}`}
                        title="编辑部门"
                      >
                        <Pencil />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => void remove(department)}
                        aria-label={`删除${department.name}`}
                        title="删除空部门"
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
        {departments.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <span className="text-center">
              <Building2 className="mx-auto mb-2 size-7" />
              当前租户暂无部门
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
            <DialogTitle>{editing ? "编辑部门" : "创建部门"}</DialogTitle>
            <DialogDescription>
              移动部门时会同步重建整棵子树的层级关系；冲突版本不会覆盖其他管理员的修改。
            </DialogDescription>
          </DialogHeader>
          <form
            key={editing?.id ?? "create"}
            id="department-form"
            className="grid gap-4"
            onSubmit={submit}
          >
            <div className="grid gap-2">
              <Label htmlFor="department-name">部门名称</Label>
              <Input
                id="department-name"
                name="name"
                defaultValue={editing?.name}
                required
                maxLength={128}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="department-parent">上级部门</Label>
              <SimpleSelect
                id="department-parent"
                options={parentOptions}
                value={parentID}
                onValueChange={setParentID}
              />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="grid gap-2">
                <Label htmlFor="department-sort-order">排序</Label>
                <Input
                  id="department-sort-order"
                  name="sort_order"
                  type="number"
                  min={0}
                  max={1000000}
                  defaultValue={editing?.sort_order ?? 0}
                  required
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="department-status">状态</Label>
                <SimpleSelect
                  id="department-status"
                  options={{ active: "启用", disabled: "停用" }}
                  value={status}
                  onValueChange={(value) =>
                    setStatus(value as DepartmentInput["status"])
                  }
                />
              </div>
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="department-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function filterCollapsedDepartments(
  departments: Department[],
  collapsed: Set<string>
): Department[] {
  const byID = new Map(
    departments.map((department) => [department.id, department])
  )
  return departments.filter((department) => {
    let parentID = department.parent_id
    while (parentID) {
      if (collapsed.has(parentID)) return false
      parentID = byID.get(parentID)?.parent_id
    }
    return true
  })
}

function isDescendantOf(
  candidate: Department,
  ancestorID: string | undefined,
  departments: Map<string, Department>
): boolean {
  if (!ancestorID) return false
  let parentID = candidate.parent_id
  while (parentID) {
    if (parentID === ancestorID) return true
    parentID = departments.get(parentID)?.parent_id
  }
  return false
}
