import { useCallback, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Copy } from "lucide-react"
import { useTranslations } from "use-intl"
import { Badge } from "@workspace/ui/components/badge"
import { Button } from "@workspace/ui/components/button"
import { Card, CardContent, CardHeader, CardTitle } from "@workspace/ui/components/card"
import { toast } from "@workspace/ui/components/sonner"
import { controlApi, machineConnectCommand, type Machine } from "../../../lib/control-api"

export default function MachinesPage() {
  const navigate = useNavigate()
  const t = useTranslations("platform")
  const [machines, setMachines] = useState<Machine[]>([])
  const [wizard, setWizard] = useState<{ token: string; machineId: string } | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(() => {
    void controlApi.machines().then(setMachines).catch(() => setMachines([]))
  }, [])

  useEffect(() => {
    load()
    const timer = setInterval(load, 5000)
    return () => clearInterval(timer)
  }, [load])

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

  const copyCommand = () => {
    if (!wizard) return
    void navigator.clipboard.writeText(machineConnectCommand(wizard.token)).then(
      () => toast.success(t("machines.copied")),
      () => toast.error(t("machines.copyFailed"))
    )
  }

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold">{t("nav.machines")}</h1>
        <Button onClick={startWizard} disabled={busy}>
          {t("machines.add")}
        </Button>
      </div>

      {wizard && (
        <Card className="border-primary/50">
          <CardHeader>
            <CardTitle className="text-sm">{t("machines.wizardTitle")}</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2">
            <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs">
              {machineConnectCommand(wizard.token)}
            </pre>
            <div className="flex flex-wrap gap-2">
              <Button variant="outline" className="cursor-pointer gap-1.5" onClick={copyCommand}>
                <Copy className="size-4" />
                {t("machines.copyCommand")}
              </Button>
              <Button onClick={confirm}>
                {t("machines.confirm")}
              </Button>
              <Button variant="outline" onClick={cancel}>
                {t("common.cancel")}
              </Button>
            </div>
          </CardContent>
        </Card>
      )}

      <div className="grid gap-3 sm:grid-cols-2">
        {machines.map((m) => (
          <Card
            key={m.id}
            className="cursor-pointer transition-colors hover:border-primary/40 hover:bg-accent/30"
            onClick={() => navigate(`/team/machines/${m.id}`)}
          >
            <CardHeader>
              <CardTitle className="flex items-center justify-between text-base">
                <span>{m.name}</span>
                <Badge variant={m.online ? "default" : "secondary"}>
                  {m.online ? t("dashboard.online") : t("machines.offline")}
                </Badge>
              </CardTitle>
            </CardHeader>
            <CardContent className="space-y-1 text-sm text-muted-foreground">
              <div>id: {m.id}</div>
              <div>{t("machines.address")}: {m.address}</div>
              <div>{t("machines.agentCount")}: {m.agentCount}</div>
              <div>{t("dashboard.lastHeartbeat")}: {m.lastHeartbeatAt ? new Date(m.lastHeartbeatAt).toLocaleTimeString() : "—"}</div>
              <div className="pt-1 text-xs text-primary">{t("machines.viewDetail")} →</div>
            </CardContent>
          </Card>
        ))}
      </div>
    </div>
  )
}
