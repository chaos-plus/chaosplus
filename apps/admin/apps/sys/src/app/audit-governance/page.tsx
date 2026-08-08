import { useEffect, useState, type FormEvent } from "react"
import {
  CheckCircle2,
  FileClock,
  Fingerprint,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
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
import { iamApi, type AuditAnchor, type AuditGovernance } from "../../lib/iam-api"

export default function AuditGovernancePage() {
  const [governance, setGovernance] = useState<AuditGovernance | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [dialogOpen, setDialogOpen] = useState(false)
  const [minDays, setMinDays] = useState("365")
  const [archiveDays, setArchiveDays] = useState("730")

  const refresh = async () => {
    setLoading(true)
    setError("")
    try {
      const report = await iamApi.auditGovernance()
      setGovernance(report)
      setMinDays(String(report.policy.min_days))
      setArchiveDays(String(report.policy.archive_after_days))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    let active = true
    void iamApi.auditGovernance().then(
      (report) => {
        if (!active) return
        setGovernance(report)
        setMinDays(String(report.policy.min_days))
        setArchiveDays(String(report.policy.archive_after_days))
        setError("")
        setLoading(false)
      },
      (cause: Error) => {
        if (!active) return
        setError(cause.message)
        setLoading(false)
      }
    )
    return () => {
      active = false
    }
  }, [])

  const saveRetention = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    try {
      await iamApi.setAuditRetention({
        min_days: Number(minDays),
        archive_after_days: Number(archiveDays),
      })
      setDialogOpen(false)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const signRoot = async () => {
    setBusy(true)
    setError("")
    try {
      await iamApi.auditSignRoot()
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const policy = governance?.policy
  const integrity = governance?.integrity
  const anchor = integrity?.anchor

  return (
    <>
      <PageHeader
        title="审计治理"
        description="审计事件保留策略、链完整性校验与 WORM 根锚点签名"
        action={
          <div className="flex flex-wrap items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => void refresh()}
              disabled={loading}
            >
              <RefreshCw className={loading ? "animate-spin" : ""} />
              刷新
            </Button>
            <Button type="button" size="sm" onClick={() => setDialogOpen(true)}>
              更新保留策略
            </Button>
            <Button
              type="button"
              size="sm"
              variant="secondary"
              onClick={() => void signRoot()}
              disabled={busy || integrity?.valid !== true}
            >
              {busy ? <LoaderCircle className="animate-spin" /> : <Fingerprint />}
              签名当前根
            </Button>
          </div>
        }
      />
      {error && <Alert>{error}</Alert>}
      {loading && !governance ? (
        <div className="flex items-center gap-2 text-muted-foreground">
          <LoaderCircle className="animate-spin" />
          加载中
        </div>
      ) : (
        <div className="grid gap-4">
          <section className="grid gap-4 lg:grid-cols-3">
            <PolicyCard
              minDays={policy?.min_days}
              archiveAfterDays={policy?.archive_after_days}
              updatedAt={policy?.updated_at}
            />
            <IntegrityCard
              valid={integrity?.valid}
              verified={integrity?.verified_events}
              headSequence={integrity?.head_sequence}
              anchor={anchor}
            />
            <StatsCard
              total={governance?.total_events}
              archiveReady={governance?.archive_ready_events}
              anchored={governance?.anchored_events}
            />
          </section>
          <AnchorsTable anchors={governance?.anchors ?? []} />
        </div>
      )}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>配置保留策略</DialogTitle>
            <DialogDescription>设置事件的最小保留期与归档就绪期，归档不会删除事件。</DialogDescription>
          </DialogHeader>
          <form onSubmit={(event) => void saveRetention(event)}>
            <div className="grid gap-4">
              <div className="grid gap-2">
                <Label htmlFor="min-days">最短保留天数</Label>
                <Input
                  id="min-days"
                  type="number"
                  min={1}
                  max={36500}
                  required
                  value={minDays}
                  onChange={(event) => setMinDays(event.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="archive-days">归档就绪天数</Label>
                <Input
                  id="archive-days"
                  type="number"
                  min={1}
                  max={36500}
                  required
                  value={archiveDays}
                  onChange={(event) => setArchiveDays(event.target.value)}
                />
              </div>
            </div>
            <DialogFooter className="mt-6">
              <Button
                type="button"
                variant="outline"
                onClick={() => setDialogOpen(false)}
              >
                取消
              </Button>
              <Button type="submit" disabled={busy}>
                {busy && <LoaderCircle className="animate-spin" />}
                保存
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  )
}

function PolicyCard({
  minDays,
  archiveAfterDays,
  updatedAt,
}: {
  minDays?: number
  archiveAfterDays?: number
  updatedAt?: string
}) {
  return (
    <Card icon={<ShieldCheck />} title="保留策略">
      <Stat label="最短保留天数" value={minDays ?? "-"} />
      <Stat label="归档就绪天数" value={archiveAfterDays ?? "-"} />
      <Stat
        label="更新时间"
        value={updatedAt ? new Date(updatedAt).toLocaleString() : "平台默认"}
      />
    </Card>
  )
}

function IntegrityCard({
  valid,
  verified,
  headSequence,
  anchor,
}: {
  valid?: boolean
  verified?: number
  headSequence?: number
  anchor?: AuditGovernance["integrity"]["anchor"]
}) {
  return (
    <Card icon={<CheckCircle2 />} title="链完整性">
      <Stat
        label="链完整性"
        value={
          <Badge variant={valid ? "default" : "destructive"}>
            {valid ? "有效" : "无效"}
          </Badge>
        }
      />
      <Stat label="已校验事件" value={verified ?? 0} />
      <Stat label="链头序号" value={headSequence ?? 0} />
      <Stat
        label="锚定状态"
        value={
          <Badge variant={anchor?.enabled ? "default" : "outline"}>
            {anchor?.enabled ? "已启用" : "未启用"}
          </Badge>
        }
      />
      <Stat
        label="签名状态"
        value={
          <Badge
            variant={
              anchor?.signed
                ? anchor.signature_valid
                  ? "default"
                  : "destructive"
                : "outline"
            }
          >
            {anchor?.signed
              ? anchor.signature_valid
                ? "已签名"
                : "签名无效"
              : "未签名"}
          </Badge>
        }
      />
    </Card>
  )
}

function StatsCard({
  total,
  archiveReady,
  anchored,
}: {
  total?: number
  archiveReady?: number
  anchored?: number
}) {
  return (
    <Card icon={<FileClock />} title="统计数据">
      <Stat label="事件总数" value={total ?? 0} />
      <Stat label="可归档事件" value={archiveReady ?? 0} />
      <Stat label="已锚定事件" value={anchored ?? 0} />
    </Card>
  )
}

function AnchorsTable({ anchors }: { anchors: AuditAnchor[] }) {
  return (
    <section className="rounded-md border">
      <div className="flex items-center gap-2 border-b px-4 py-3">
        <FileClock className="size-4 text-muted-foreground" />
        <h2 className="text-sm font-medium">根锚点链</h2>
      </div>
      {anchors.length === 0 ? (
        <p className="px-4 py-6 text-sm text-muted-foreground">无锚点记录</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>序号</TableHead>
              <TableHead>链头哈希</TableHead>
              <TableHead>锚定时间</TableHead>
              <TableHead>锚点哈希</TableHead>
              <TableHead>签名状态</TableHead>
              <TableHead>签名键 ID</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {anchors.map((item) => (
              <TableRow key={item.head_sequence}>
                <TableCell className="font-mono text-xs">
                  {item.head_sequence}
                </TableCell>
                <TableCell className="max-w-52 truncate font-mono text-xs">
                  {item.head_hash}
                </TableCell>
                <TableCell className="text-xs">
                  {new Date(item.anchored_at).toLocaleString()}
                </TableCell>
                <TableCell className="max-w-52 truncate font-mono text-xs">
                  {item.anchor_hash}
                </TableCell>
                <TableCell>
                  <Badge variant={item.root_signature ? "default" : "outline"}>
                    {item.root_signature ? "已签名" : "未签名"}
                  </Badge>
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {item.signing_key_id ?? "-"}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </section>
  )
}

function Card({
  icon,
  title,
  children,
}: {
  icon: React.ReactNode
  title: string
  children: React.ReactNode
}) {
  return (
    <section className="rounded-md border p-4">
      <div className="mb-3 flex items-center gap-2">
        {icon}
        <h2 className="text-sm font-medium">{title}</h2>
      </div>
      <dl className="grid gap-3">{children}</dl>
    </section>
  )
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="text-sm font-medium">{value}</dd>
    </div>
  )
}
