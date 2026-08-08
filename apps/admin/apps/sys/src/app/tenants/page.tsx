import { useCallback, useEffect, useState, type FormEvent } from "react"
import { ArrowRight, Landmark, Pencil, Plus, Trash2 } from "lucide-react"
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
import { useNavigate } from "react-router"
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  setTenant,
  type Tenant,
  type TenantUpdate,
} from "../../lib/iam-api"

const statusLabels: Record<Tenant["status"], string> = {
  active: "启用",
  suspended: "停用",
  deleted: "已删除",
}

export default function TenantsPage() {
  const [tenants, setTenants] = useState<Tenant[]>([])
  const [includeDeleted, setIncludeDeleted] = useState(false)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Tenant | null>(null)
  const [deleting, setDeleting] = useState<Tenant | null>(null)
  const [status, setStatus] = useState<TenantUpdate["status"]>("active")
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const navigate = useNavigate()

  const refresh = useCallback(
    () =>
      iamApi
        .tenants(includeDeleted)
        .then(setTenants)
        .finally(() => setLoading(false)),
    [includeDeleted]
  )

  useEffect(() => {
    void refresh().catch((cause: Error) => setError(cause.message))
  }, [refresh])

  const openCreate = () => {
    setEditing(null)
    setStatus("active")
    setOpen(true)
  }

  const openEdit = (tenant: Tenant) => {
    setEditing(tenant)
    setStatus(tenant.status === "suspended" ? "suspended" : "active")
    setOpen(true)
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    try {
      if (editing) {
        await iamApi.updateTenant(editing.id, {
          name: String(data.get("name")),
          status,
          version: editing.version,
        })
      } else {
        await iamApi.createTenant({
          slug: String(data.get("slug")),
          name: String(data.get("name")),
        })
      }
      setOpen(false)
      setEditing(null)
      await refresh()
      window.dispatchEvent(new Event("tenant-catalog-change"))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const remove = async () => {
    if (!deleting) return
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteTenant(deleting.id, deleting.version)
      setDeleting(null)
      await refresh()
      window.dispatchEvent(new Event("tenant-catalog-change"))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const enterTenant = (tenant: Tenant) => {
    setTenant(tenant.id)
    navigate("/")
  }

  return (
    <>
      <PageHeader
        title="租户管理"
        description="平台租户生命周期"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建租户
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      <div className="mb-3 flex min-h-9 items-center justify-end">
        <label
          htmlFor="include-deleted-tenants"
          className="flex min-h-9 cursor-pointer items-center gap-2 text-sm"
        >
          <Checkbox
            id="include-deleted-tenants"
            checked={includeDeleted}
            onCheckedChange={(checked) => {
              setError("")
              setLoading(true)
              setIncludeDeleted(checked === true)
            }}
          />
          显示已删除租户
        </label>
      </div>
      <div className="overflow-x-auto rounded-md border bg-card">
        <Table className="min-w-[900px]">
          <TableHeader>
            <TableRow>
              <TableHead>租户</TableHead>
              <TableHead>标识</TableHead>
              <TableHead>ID</TableHead>
              <TableHead>状态</TableHead>
              <TableHead>版本</TableHead>
              <TableHead>更新时间</TableHead>
              <TableHead className="w-36 text-right">操作</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tenants.map((tenant) => (
              <TableRow key={tenant.id}>
                <TableCell>
                  <div className="flex min-w-0 items-center gap-2">
                    <Landmark className="size-4 shrink-0 text-primary" />
                    <strong className="truncate text-sm">{tenant.name}</strong>
                  </div>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {tenant.slug}
                </TableCell>
                <TableCell className="max-w-64 truncate font-mono text-xs text-muted-foreground">
                  {tenant.id}
                </TableCell>
                <TableCell>
                  <Badge
                    variant={
                      tenant.status === "active"
                        ? "secondary"
                        : tenant.status === "suspended"
                          ? "destructive"
                          : "outline"
                    }
                  >
                    {statusLabels[tenant.status]}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  v{tenant.version}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {new Date(tenant.updated_at).toLocaleString("zh-CN", {
                    hour12: false,
                  })}
                </TableCell>
                <TableCell className="text-right">
                  {tenant.status !== "deleted" && (
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy || tenant.status !== "active"}
                        onClick={() => enterTenant(tenant)}
                        aria-label={`进入${tenant.name}`}
                        title="进入租户"
                      >
                        <ArrowRight />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        disabled={busy}
                        onClick={() => openEdit(tenant)}
                        aria-label={`编辑${tenant.name}`}
                        title="编辑租户"
                      >
                        <Pencil />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="text-destructive"
                        disabled={busy}
                        onClick={() => setDeleting(tenant)}
                        aria-label={`删除${tenant.name}`}
                        title="删除租户"
                      >
                        <Trash2 />
                      </Button>
                    </div>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
        {!loading && tenants.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <span className="text-center">
              <Landmark className="mx-auto mb-2 size-7" />
              暂无租户
            </span>
          </div>
        )}
        {loading && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            正在加载
          </div>
        )}
      </div>

      <Dialog
        open={open}
        onOpenChange={(next) => {
          setOpen(next)
          if (!next) setEditing(null)
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "编辑租户" : "创建租户"}</DialogTitle>
            <DialogDescription>
              {editing
                ? "租户标识创建后不可修改。"
                : "租户标识用于平台内唯一识别。"}
            </DialogDescription>
          </DialogHeader>
          <form
            key={editing?.id ?? "create"}
            id="tenant-form"
            className="grid gap-4"
            onSubmit={submit}
          >
            <div className="grid gap-2">
              <Label htmlFor="tenant-name">名称</Label>
              <Input
                id="tenant-name"
                name="name"
                defaultValue={editing?.name}
                maxLength={128}
                required
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="tenant-slug">标识</Label>
              <Input
                id="tenant-slug"
                name="slug"
                defaultValue={editing?.slug}
                minLength={3}
                maxLength={63}
                pattern="[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?"
                disabled={Boolean(editing)}
                required={!editing}
              />
            </div>
            {editing && (
              <div className="grid gap-2">
                <Label htmlFor="tenant-status">状态</Label>
                <SimpleSelect
                  id="tenant-status"
                  options={{ active: "启用", suspended: "停用" }}
                  value={status}
                  onValueChange={(value) =>
                    setStatus(value as TenantUpdate["status"])
                  }
                />
              </div>
            )}
          </form>
          <DialogFooter>
            <Button variant="outline" onClick={() => setOpen(false)}>
              取消
            </Button>
            <Button type="submit" form="tenant-form" disabled={busy}>
              {busy ? "保存中" : editing ? "保存" : "创建"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog
        open={Boolean(deleting)}
        onOpenChange={(next) => !next && setDeleting(null)}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>删除租户</DialogTitle>
            <DialogDescription>
              确认删除“{deleting?.name}”？删除后该租户将立即停止授权。
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => void remove()}
            >
              {busy ? "删除中" : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
