import { useCallback, useEffect, useState } from "react"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { controlApi, type Machine } from "../../../lib/control-api"

export default function MachinesPage() {
  const [machines, setMachines] = useState<Machine[]>([])
  const [wizard, setWizard] = useState<{ token: string; machineId: string; expiresIn: number } | null>(null)
  const [countdown, setCountdown] = useState(0)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    void controlApi.machines().then(setMachines).catch(() => setMachines([]))
  }, [])

  useEffect(() => {
    load()
    const t = setInterval(load, 5000)
    return () => clearInterval(t)
  }, [load])

  useEffect(() => {
    if (!wizard) return
    setCountdown(wizard.expiresIn)
    const t = setInterval(() => setCountdown((c) => Math.max(0, c - 1)), 1000)
    return () => clearInterval(t)
  }, [wizard])

  const startWizard = async () => {
    setBusy(true)
    try {
      setWizard(await controlApi.issueToken())
    } finally {
      setBusy(false)
    }
  }

  const confirm = async () => {
    if (!wizard) return
    await controlApi.confirmMachine(wizard.machineId, wizard.token)
    setWizard(null)
    load()
  }

  const cancel = async () => {
    if (!wizard) return
    await controlApi.cancelMachine(wizard.machineId)
    setWizard(null)
  }

  const refresh = async () => {
    if (!wizard) return
    const t = await controlApi.refreshToken(wizard.machineId)
    setWizard({ ...wizard, token: t.token, expiresIn: 0 })
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">机器 Machines</h1>
        <Button onClick={startWizard} disabled={busy}>
          ＋ 添加 Machine
        </Button>
      </div>

      {wizard && (
        <Card className="border-primary/50">
          <CardHeader>
            <CardTitle className="text-sm">接入新机器(token {countdown}s 内确认;长期 token 仅手动轮换)</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">
              bun run src/serve.ts --server http://{location.host} --token {wizard.token}
            </pre>
            <div className="flex gap-2">
              <Button onClick={confirm} disabled={countdown <= 0}>
                确认接入
              </Button>
              <Button variant="outline" onClick={cancel}>
                取消
              </Button>
              <Button variant="outline" onClick={refresh}>
                刷新命令
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        {machines.map((m) => (
          <Card key={m.id}>
            <CardHeader>
              <CardTitle className="flex items-center justify-between text-base">
                <span>{m.name}</span>
                <Badge variant={m.online ? "default" : "secondary"}>{m.online ? "在线" : "离线"}</Badge>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-1 text-sm text-muted-foreground">
              <div>id: {m.id}</div>
              <div>地址: {m.address}</div>
              <div>最近心跳: {m.lastHeartbeatAt ? new Date(m.lastHeartbeatAt).toLocaleTimeString() : "—"}</div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
