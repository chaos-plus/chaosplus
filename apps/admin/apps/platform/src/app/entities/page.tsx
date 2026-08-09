import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Boxes, Plus } from "lucide-react"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"
import { getEntity, getTenant, iamApi, setEntity, setTenant, type Entity } from "../../lib/iam-api"

/** 实体(instance)创建/加入:登录后必须选定一个实例才能查看其下的资源。 */
export default function EntitiesPage() {
  const navigate = useNavigate()
  const [entities, setEntities] = useState<Entity[]>([])
  const [current, setCurrent] = useState(getEntity())
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")

  // 实体接口需要租户上下文(X-Tenant-Id)。独立路由不经 layout,先确保有租户。
  const ensureTenant = useCallback(async () => {
    if (getTenant()) return
    const mine = (await iamApi.myTenants().catch(() => [])) ?? []
    if (mine[0]) setTenant(mine[0].id)
  }, [])

  const load = useCallback(() => {
    void iamApi
      .entities()
      .then((x) => setEntities(x ?? []))
      .catch((e: unknown) => {
        setEntities([])
        toast.error(`加载实体失败:${e instanceof Error ? e.message : String(e)}`)
      })
  }, [])

  useEffect(() => {
    void ensureTenant().then(load)
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load, ensureTenant])

  const enter = (id: string) => {
    setEntity(id)
    setCurrent(id)
    navigate("/") // 进入平台,按该实体展示资源
  }

  const create = async () => {
    if (!name.trim()) return
    try {
      const en = await iamApi.createEntity({ type: "instance", name: name.trim(), status: "active", metadata: {} })
      setOpen(false)
      setName("")
      load()
      enter(en.id)
    } catch (e) {
      toast.error(`创建失败:${e instanceof Error ? e.message : String(e)}`)
    }
  }

  return (
    <div className="mx-auto flex min-h-svh max-w-2xl flex-col justify-center gap-4 px-4 py-8">
      <div className="grid justify-items-center gap-2 text-center">
        <Boxes className="size-10 text-muted-foreground" aria-hidden="true" />
        <h1 className="text-xl font-semibold">选择一个实例</h1>
        <p className="text-sm text-muted-foreground">
          仪表盘、工作区、工作流、会话区等资源都归属在实例(instance)下。请选择或创建一个实例进入。
        </p>
      </div>

      <div className="grid gap-2">
        {entities.map((en) => (
          <Card
            key={en.id}
            className={`cursor-pointer p-4 transition-colors hover:border-primary/40 hover:bg-accent/40 ${
              current === en.id ? "border-primary/60" : ""
            }`}
            onClick={() => enter(en.id)}
          >
            <div className="flex items-center gap-2">
              <Boxes className="size-5 text-muted-foreground" aria-hidden="true" />
              <span className="font-medium">{en.name}</span>
              {current === en.id && <span className="ml-auto text-xs text-primary">当前</span>}
            </div>
            <p className="mt-1 text-xs text-muted-foreground">{en.id}</p>
          </Card>
        ))}
        {entities.length === 0 && (
          <p className="rounded-xl border border-dashed p-6 text-center text-sm text-muted-foreground">
            还没有实例。创建一个后即可开始。
          </p>
        )}
      </div>

      <Button className="cursor-pointer gap-1.5" onClick={() => setOpen(true)}>
        <Plus className="size-4" />
        创建实例
      </Button>

      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-sm">
          <DialogHeader>
            <DialogTitle>创建实例</DialogTitle>
          </DialogHeader>
          <div className="grid gap-1.5 py-2">
            <label htmlFor="entity-name" className="text-sm font-medium">名称</label>
            <Input
              id="entity-name"
              autoFocus
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="如:my-app"
              onKeyDown={(e) => e.key === "Enter" && create()}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" className="cursor-pointer" onClick={() => setOpen(false)}>取消</Button>
            <Button className="cursor-pointer" onClick={create} disabled={!name.trim()}>创建并进入</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
