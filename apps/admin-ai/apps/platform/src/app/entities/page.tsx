/* eslint-disable react-hooks/set-state-in-effect */
import { useCallback, useEffect, useState } from "react"
import { Boxes, Plus } from "lucide-react"
import { Button } from "@workspace/ui/components/button"
import { Card } from "@workspace/ui/components/card"
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@workspace/ui/components/dialog"
import { Input } from "@workspace/ui/components/input"
import { toast } from "@workspace/ui/components/sonner"
import { getEntity, getTenant, iamApi, resolveCurrentTenant, setEntity, type Entity } from "../../lib/iam-api"

/** 实体(instance)创建/加入:登录后必须选定一个实例才能查看其下的资源。 */
export default function EntitiesPage() {
  const [entities, setEntities] = useState<Entity[]>([])
  const [current, setCurrent] = useState(getEntity())
  const [tenant, setTenantId] = useState(getTenant())
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  // tab:默认创建;URL 带 ?invite= 时切到加入并预填邀请码。
  const inviteCode = new URLSearchParams(window.location.search).get("invite") ?? ""
  const [tab, setTab] = useState<"create" | "join">(inviteCode ? "join" : "create")
  const [invite, setInvite] = useState(inviteCode)
  const [inviteInfo, setInviteInfo] = useState<Entity | null>(null)
  const [inviteErr, setInviteErr] = useState("")
  const [joining, setJoining] = useState(false)

  // 实体接口需要租户上下文(X-Tenant-Id)。独立路由不经 layout,先确保有租户,
  // 并显式传给每次请求(不依赖 getTenant 兜底)。
  const ensureTenant = useCallback(async () => {
    const id = await resolveCurrentTenant()
    if (id) setTenantId(id)
    return id
  }, [])

  const load = useCallback((tenantId: string) => {
    void iamApi
      .entities(tenantId)
      .then((x) => setEntities(x ?? []))
      .catch((e: unknown) => {
        setEntities([])
        toast.error(`加载实体失败:${e instanceof Error ? e.message : String(e)}`)
      })
  }, [])

  useEffect(() => {
    // 轮询必须用解析后的租户:直接闭包捕获会拿到解析前的空值,
    // 第二次 tick 就用空租户把列表清掉了。
    let tenantId = tenant
    const tick = () => {
      if (tenantId) load(tenantId)
    }
    void ensureTenant().then((id) => {
      tenantId = id || tenantId
      tick()
    })
    const t = setInterval(tick, 5000)
    return () => clearInterval(t)
  }, [load, ensureTenant, tenant])

  const enter = (id: string) => {
    setCurrent(id)
    setEntity(id, "/") // 整页跳转进平台,按该实体重新拉数据
  }

  const create = async () => {
    if (!name.trim()) return
    try {
      const tenantId = await ensureTenant()
      const en = await iamApi.createEntity({ type: "instance", name: name.trim(), status: "active", metadata: {} }, tenantId)
      setOpen(false)
      setName("")
      load(tenantId)
      enter(en.id)
    } catch (e) {
      toast.error(`创建失败:${e instanceof Error ? e.message : String(e)}`)
    }
  }

  const lookupInvite = async () => {
    if (!invite.trim()) return
    setInviteErr("")
    setInviteInfo(null)
    try {
      const en = await iamApi.lookupEntityInvite(invite.trim())
      setInviteInfo(en)
    } catch (e) {
      setInviteErr(e instanceof Error ? e.message : "邀请码无效")
    }
  }

  useEffect(() => {
    if (inviteCode) void lookupInvite()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const join = async () => {
    if (!inviteInfo || joining) return
    setJoining(true)
    try {
      await iamApi.acceptEntityInvite(invite.trim())
      enter(inviteInfo.id)
    } catch (e) {
      setInviteErr(e instanceof Error ? e.message : "加入失败")
    } finally {
      setJoining(false)
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

      {/* 创建 / 加入 */}
      <div className="flex gap-1 rounded-lg border bg-muted/40 p-1">
        {(["create", "join"] as const).map((k) => (
          <button
            key={k}
            onClick={() => setTab(k)}
            className={`flex-1 cursor-pointer rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
              tab === k ? "bg-background text-foreground shadow-sm" : "text-muted-foreground hover:text-foreground"
            }`}
          >
            {k === "create" ? "创建" : "加入"}
          </button>
        ))}
      </div>

      {tab === "join" && (
        <div className="grid gap-2 rounded-xl border p-4">
          <label htmlFor="invite-code" className="text-sm font-medium">邀请码</label>
          <div className="flex gap-2">
            <Input
              id="invite-code"
              value={invite}
              onChange={(e) => {
                setInvite(e.target.value)
                setInviteInfo(null)
                setInviteErr("")
              }}
              placeholder="粘贴邀请链接或邀请码"
            />
            <Button variant="outline" className="cursor-pointer" onClick={lookupInvite} disabled={!invite.trim()}>
              查询实例
            </Button>
          </div>
          {inviteErr && <p className="text-sm text-destructive">{inviteErr}</p>}
          {inviteInfo && (
            <div className="rounded-lg border bg-muted/40 p-3">
              <div className="flex items-center gap-2">
                <Boxes className="size-4 text-muted-foreground" aria-hidden="true" />
                <span className="font-medium">{inviteInfo.name}</span>
              </div>
              <p className="mt-1 text-xs text-muted-foreground">
                实例 {inviteInfo.id} · 将加入该实例,可以看到它名下的资源。
              </p>
              <Button className="mt-2 cursor-pointer gap-1.5" onClick={join} disabled={joining}>
                <Plus className="size-4" />
                {joining ? "加入中…" : "确认加入"}
              </Button>
            </div>
          )}
        </div>
      )}

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
