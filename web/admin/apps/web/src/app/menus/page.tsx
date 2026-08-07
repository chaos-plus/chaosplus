import { useCallback, useEffect, useState, type FormEvent } from "react"
import { ArrowUpDown, Boxes, Pencil, Plus, Trash2 } from "lucide-react"
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
import { iamApi, type Menu, type MenuInput } from "../../lib/iam-api"

const rootMenu = "__root__"

export default function MenusPage() {
  const [menus, setMenus] = useState<Menu[]>([])
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Menu | null>(null)
  const [parentID, setParentID] = useState(rootMenu)
  const [status, setStatus] = useState<MenuInput["status"]>("active")
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const refresh = useCallback(() => iamApi.menus().then(setMenus), [])

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const openCreate = () => {
    setEditing(null)
    setParentID(rootMenu)
    setStatus("active")
    setOpen(true)
  }

  const openEdit = (menu: Menu) => {
    setEditing(menu)
    setParentID(menu.parent_id || rootMenu)
    setStatus(menu.status)
    setOpen(true)
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const body: MenuInput = {
      parent_id: parentID === rootMenu ? "" : parentID,
      label: String(data.get("label")),
      route: String(data.get("route")),
      icon: String(data.get("icon")),
      permission_code: String(data.get("permission_code")),
      sort_order: Number(data.get("sort_order") || 0),
      status,
    }
    try {
      if (editing) await iamApi.updateMenu(editing.id, body)
      else await iamApi.createMenu(body)
      setOpen(false)
      setEditing(null)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async (id: string) => {
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteMenu(id)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const parentOptions = [
    { value: rootMenu, label: "顶级菜单" },
    ...menus
      .filter((menu) => menu.id !== editing?.id)
      .map((menu) => ({ value: menu.id, label: menu.label })),
  ]

  return (
    <>
      <PageHeader
        title="菜单管理"
        description="菜单只控制导航可见性，后端权限仍由路由 Guard 强制执行"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建菜单
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="overflow-hidden rounded-md border bg-card">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>菜单</TableHead>
              <TableHead>路由</TableHead>
              <TableHead>权限码</TableHead>
              <TableHead>排序</TableHead>
              <TableHead>状态</TableHead>
              <TableHead className="w-24 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {menus.map((menu) => (
              <TableRow key={menu.id}>
                <TableCell>
                  <span className="flex items-center gap-2 font-medium">
                    <Boxes className="size-4 text-primary" />
                    {menu.label}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {menu.route || "-"}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {menu.permission_code || "-"}
                </TableCell>
                <TableCell>
                  <span className="flex items-center gap-1 text-xs">
                    <ArrowUpDown size={13} />
                    {menu.sort_order}
                  </span>
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      menu.status === "active" ? "secondary" : "destructive"
                    }
                  >
                    {menu.status === "active" ? "启用" : "停用"}
                  </Badge>
                </TableCell>
                <TableCell className="text-right">
                  <div className="flex justify-end gap-1">
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => openEdit(menu)}
                      aria-label={`编辑${menu.label}`}
                      title="编辑菜单"
                    >
                      <Pencil />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      disabled={busy}
                      onClick={() => void remove(menu.id)}
                      aria-label={`删除${menu.label}`}
                      title="删除菜单"
                    >
                      <Trash2 className="text-destructive" />
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {menus.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <Boxes className="mb-2 size-7" />
            暂无菜单节点
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
            <DialogTitle>{editing ? "编辑菜单" : "创建菜单"}</DialogTitle>
            <DialogDescription>
              路由和权限码必须与已声明的前后端能力一致。
            </DialogDescription>
          </DialogHeader>
          <form
            key={editing?.id ?? "create"}
            id="menu-form"
            className="grid gap-4 sm:grid-cols-2"
            onSubmit={submit}
          >
            <MenuField
              label="显示名称"
              name="label"
              defaultValue={editing?.label}
              maxLength={128}
              required
            />
            <MenuField
              label="前端路由"
              name="route"
              defaultValue={editing?.route}
              maxLength={512}
            />
            <MenuField
              label="图标标识"
              name="icon"
              defaultValue={editing?.icon}
              maxLength={64}
            />
            <MenuField
              label="权限码"
              name="permission_code"
              defaultValue={editing?.permission_code}
              maxLength={128}
            />
            <MenuField
              label="排序"
              name="sort_order"
              type="number"
              defaultValue={String(editing?.sort_order ?? 0)}
              min={-100000}
              max={100000}
            />
            <div className="grid gap-2">
              <Label htmlFor="menu-parent">父级菜单</Label>
              <SimpleSelect
                id="menu-parent"
                options={parentOptions}
                value={parentID}
                onValueChange={setParentID}
              />
            </div>
            <div className="grid gap-2 sm:col-span-2">
              <Label htmlFor="menu-status">状态</Label>
              <SimpleSelect
                id="menu-status"
                options={[
                  { value: "active", label: "启用" },
                  { value: "disabled", label: "停用" },
                ]}
                value={status}
                onValueChange={(value) =>
                  setStatus(value as MenuInput["status"])
                }
              />
            </div>
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="menu-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

function MenuField({
  label,
  name,
  type = "text",
  required = false,
  defaultValue,
  maxLength,
  min,
  max,
}: {
  label: string
  name: string
  type?: string
  required?: boolean
  defaultValue?: string
  maxLength?: number
  min?: number
  max?: number
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={`menu-${name}`}>{label}</Label>
      <Input
        id={`menu-${name}`}
        name={name}
        type={type}
        required={required}
        defaultValue={defaultValue}
        maxLength={maxLength}
        min={min}
        max={max}
      />
    </div>
  )
}
