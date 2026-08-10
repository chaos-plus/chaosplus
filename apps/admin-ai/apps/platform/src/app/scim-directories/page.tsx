import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  CheckCircle2,
  Copy,
  FolderSync,
  KeyRound,
  Pencil,
  Plus,
  Trash2,
} from "lucide-react"
import { Badge } from "@workspace/ui/components/badge"
import { Button, buttonVariants } from "@workspace/ui/components/button"
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
import { Switch } from "@workspace/ui/components/switch"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@workspace/ui/components/table"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@workspace/ui/components/tooltip"
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  clientErrorMessage,
  iamApi,
  type SCIMCredential,
  type SCIMCredentialSecret,
  type SCIMDirectory,
} from "../../lib/iam-api"

interface DirectoryDraft {
  name: string
  enabled: boolean
}

interface CredentialDraft {
  name: string
  expiresAt: string
}

const emptyDirectory: DirectoryDraft = { name: "", enabled: true }
const emptyCredential: CredentialDraft = { name: "", expiresAt: "" }

export default function SCIMDirectoriesPage() {
  const [directories, setDirectories] = useState<SCIMDirectory[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [editing, setEditing] = useState<SCIMDirectory | null | undefined>()
  const [directoryDraft, setDirectoryDraft] =
    useState<DirectoryDraft>(emptyDirectory)
  const [credentialDirectory, setCredentialDirectory] =
    useState<SCIMDirectory | null>(null)
  const [credentials, setCredentials] = useState<SCIMCredential[]>([])
  const [credentialsLoading, setCredentialsLoading] = useState(false)
  const [credentialDraft, setCredentialDraft] =
    useState<CredentialDraft>(emptyCredential)
  const [revoking, setRevoking] = useState<SCIMCredential | null>(null)
  const [secret, setSecret] = useState<SCIMCredentialSecret | null>(null)
  const [endpoint] = useState(() => scimEndpoint())

  const refresh = useCallback(async () => {
    setDirectories(await iamApi.scimDirectories())
  }, [])

  useEffect(() => {
    let active = true
    void iamApi
      .scimDirectories()
      .then(
        (items) => active && setDirectories(items),
        (cause: Error) => active && setError(cause.message)
      )
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [])

  const openCreate = () => {
    setError("")
    setDirectoryDraft({ ...emptyDirectory })
    setEditing(null)
  }

  const openEdit = (directory: SCIMDirectory) => {
    setError("")
    setDirectoryDraft({
      name: directory.name,
      enabled: directory.status === "active",
    })
    setEditing(directory)
  }

  const saveDirectory = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    setNotice("")
    try {
      if (editing) {
        await iamApi.updateSCIMDirectory(editing.id, {
          name: directoryDraft.name.trim(),
          status: directoryDraft.enabled ? "active" : "disabled",
          version: editing.version,
        })
        setNotice("SCIM 目录已更新")
      } else {
        await iamApi.createSCIMDirectory({ name: directoryDraft.name.trim() })
        setNotice("SCIM 目录已创建")
      }
      setEditing(undefined)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openCredentials = async (directory: SCIMDirectory) => {
    setCredentialDirectory(directory)
    setCredentialDraft({ ...emptyCredential })
    setCredentials([])
    setCredentialsLoading(true)
    setError("")
    try {
      setCredentials(await iamApi.scimCredentials(directory.id))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setCredentialsLoading(false)
    }
  }

  const createCredential = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!credentialDirectory) return
    setBusy(true)
    setError("")
    try {
      const result = await iamApi.createSCIMCredential(credentialDirectory.id, {
        name: credentialDraft.name.trim(),
        expires_at: isoDateTime(credentialDraft.expiresAt),
      })
      setCredentialDirectory(null)
      setSecret(result)
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const revokeCredential = async () => {
    if (!credentialDirectory || !revoking) return
    setBusy(true)
    setError("")
    try {
      await iamApi.revokeSCIMCredential(credentialDirectory.id, revoking.id)
      setCredentials(await iamApi.scimCredentials(credentialDirectory.id))
      setRevoking(null)
      setNotice("SCIM 凭据已撤销")
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const copy = async (value: string, message: string) => {
    try {
      await navigator.clipboard.writeText(value)
      setNotice(message)
    } catch {
      setError(clientErrorMessage("clipboard_access_denied"))
    }
  }

  return (
    <>
      <PageHeader
        title="SCIM 目录"
        description="管理租户的 SCIM 2.0 入站预配目录和 Bearer 凭据"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建目录
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      {notice && <SuccessNotice>{notice}</SuccessNotice>}

      <section className="mb-4 flex min-h-14 flex-wrap items-center justify-between gap-3 border-y bg-muted/20 px-4 py-3">
        <div className="min-w-0">
          <span className="block text-xs font-medium text-muted-foreground">
            SCIM Base URL
          </span>
          <code className="mt-1 block truncate font-mono text-xs">
            {endpoint}
          </code>
        </div>
        <Button
          type="button"
          size="icon"
          variant="outline"
          aria-label="复制 SCIM Base URL"
          onClick={() => void copy(endpoint, "SCIM Base URL 已复制")}
        >
          <Copy />
        </Button>
      </section>

      <section className="overflow-hidden rounded-md border bg-card">
        <header className="flex min-h-14 items-center justify-between gap-4 border-b bg-muted/20 px-4">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <FolderSync className="size-4 text-muted-foreground" />
            预配目录
          </div>
          <span className="text-xs text-muted-foreground">
            {loading ? "正在加载" : `${directories.length} 个目录`}
          </span>
        </header>
        <div className="hidden md:block">
          <Table containerClassName="min-h-56">
            <TableHeader>
              <TableRow>
                <TableHead className="min-w-64">目录</TableHead>
                <TableHead className="min-w-44">更新时间</TableHead>
                <TableHead className="w-24">版本</TableHead>
                <TableHead className="w-24">状态</TableHead>
                <TableHead className="w-32 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {directories.map((directory) => (
                <TableRow key={directory.id}>
                  <TableCell>
                    <strong className="block text-sm">{directory.name}</strong>
                    <code className="mt-1 block max-w-72 truncate font-mono text-xs text-muted-foreground">
                      {directory.id}
                    </code>
                  </TableCell>
                  <TableCell>{formatDate(directory.updated_at)}</TableCell>
                  <TableCell>v{directory.version}</TableCell>
                  <TableCell>
                    <DirectoryStatus status={directory.status} />
                  </TableCell>
                  <TableCell className="text-right">
                    <DirectoryActions
                      directory={directory}
                      busy={busy}
                      onCredentials={() => void openCredentials(directory)}
                      onEdit={() => openEdit(directory)}
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!loading && directories.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="h-52 text-center">
                    <EmptyDirectories />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
        <div className="md:hidden">
          {directories.map((directory) => (
            <article
              key={directory.id}
              className="space-y-3 border-b p-4 last:border-0"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <strong className="block text-sm break-words">
                    {directory.name}
                  </strong>
                  <code className="mt-1 block truncate font-mono text-xs text-muted-foreground">
                    {directory.id}
                  </code>
                </div>
                <DirectoryStatus status={directory.status} />
              </div>
              <p className="text-xs text-muted-foreground">
                v{directory.version} · {formatDate(directory.updated_at)}
              </p>
              <DirectoryActions
                directory={directory}
                busy={busy}
                onCredentials={() => void openCredentials(directory)}
                onEdit={() => openEdit(directory)}
              />
            </article>
          ))}
          {!loading && directories.length === 0 && (
            <div className="grid h-52 place-items-center text-center">
              <EmptyDirectories />
            </div>
          )}
        </div>
      </section>

      <DirectoryEditor
        open={editing !== undefined}
        directory={editing ?? null}
        draft={directoryDraft}
        busy={busy}
        onDraft={setDirectoryDraft}
        onClose={() => !busy && setEditing(undefined)}
        onSubmit={saveDirectory}
      />
      <CredentialsDialog
        directory={credentialDirectory}
        credentials={credentials}
        loading={credentialsLoading}
        busy={busy}
        draft={credentialDraft}
        onDraft={setCredentialDraft}
        onClose={() => !busy && setCredentialDirectory(null)}
        onSubmit={createCredential}
        onRevoke={setRevoking}
      />
      <ConfirmDialog
        open={revoking !== null}
        busy={busy}
        title="撤销 SCIM 凭据"
        description={`撤销 ${revoking?.name ?? "该凭据"} 后，使用它的预配请求将立即被拒绝。`}
        onClose={() => !busy && setRevoking(null)}
        onConfirm={() => void revokeCredential()}
      />
      <SecretDialog
        value={secret}
        endpoint={endpoint}
        onCopy={copy}
        onClose={() => setSecret(null)}
      />
    </>
  )
}

function DirectoryActions({
  directory,
  busy,
  onCredentials,
  onEdit,
}: {
  directory: SCIMDirectory
  busy: boolean
  onCredentials: () => void
  onEdit: () => void
}) {
  return (
    <TooltipProvider>
      <div className="flex justify-end gap-1">
        <ActionButton
          label={`管理 ${directory.name} 的凭据`}
          onClick={onCredentials}
          disabled={busy}
        >
          <KeyRound />
        </ActionButton>
        <ActionButton
          label={`编辑 ${directory.name}`}
          onClick={onEdit}
          disabled={busy}
        >
          <Pencil />
        </ActionButton>
      </div>
    </TooltipProvider>
  )
}

function DirectoryEditor({
  open,
  directory,
  draft,
  busy,
  onDraft,
  onClose,
  onSubmit,
}: {
  open: boolean
  directory: SCIMDirectory | null
  draft: DirectoryDraft
  busy: boolean
  onDraft: (value: DirectoryDraft) => void
  onClose: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <Dialog open={open} onOpenChange={(value) => !value && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {directory ? "编辑 SCIM 目录" : "创建 SCIM 目录"}
          </DialogTitle>
          <DialogDescription>
            每个目录独立管理外部身份源、资源映射和访问凭据。
          </DialogDescription>
        </DialogHeader>
        <form
          id="scim-directory-form"
          className="grid gap-5"
          onSubmit={onSubmit}
        >
          <div className="grid gap-2">
            <Label htmlFor="scim-directory-name">目录名称</Label>
            <Input
              id="scim-directory-name"
              value={draft.name}
              onChange={(event) =>
                onDraft({ ...draft, name: event.target.value })
              }
              required
              minLength={1}
              maxLength={128}
              autoFocus
            />
          </div>
          {directory && (
            <div className="flex min-h-16 items-center justify-between gap-3 rounded-md border px-3">
              <div>
                <Label htmlFor="scim-directory-enabled">启用目录</Label>
                <p className="mt-1 text-xs text-muted-foreground">
                  停用后拒绝该目录全部 Bearer 凭据
                </p>
              </div>
              <Switch
                id="scim-directory-enabled"
                checked={draft.enabled}
                onCheckedChange={(enabled) => onDraft({ ...draft, enabled })}
              />
            </div>
          )}
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button type="submit" form="scim-directory-form" disabled={busy}>
            {busy ? "正在保存" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CredentialsDialog({
  directory,
  credentials,
  loading,
  busy,
  draft,
  onDraft,
  onClose,
  onSubmit,
  onRevoke,
}: {
  directory: SCIMDirectory | null
  credentials: SCIMCredential[]
  loading: boolean
  busy: boolean
  draft: CredentialDraft
  onDraft: (value: CredentialDraft) => void
  onClose: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onRevoke: (credential: SCIMCredential) => void
}) {
  const disabled = directory?.status !== "active"
  return (
    <Dialog
      open={directory !== null}
      onOpenChange={(value) => !value && onClose()}
    >
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle>{directory?.name ?? "SCIM"} · 凭据</DialogTitle>
          <DialogDescription>
            新凭据的 Bearer token 仅在创建成功后显示一次。
          </DialogDescription>
        </DialogHeader>
        <form
          id="scim-credential-form"
          className="grid gap-4 border-y py-4 sm:grid-cols-2"
          onSubmit={onSubmit}
        >
          <div className="grid gap-2">
            <Label htmlFor="scim-credential-name">凭据名称</Label>
            <Input
              id="scim-credential-name"
              value={draft.name}
              onChange={(event) =>
                onDraft({ ...draft, name: event.target.value })
              }
              required
              minLength={1}
              maxLength={128}
              disabled={disabled}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="scim-credential-expiry">过期时间</Label>
            <Input
              id="scim-credential-expiry"
              type="datetime-local"
              value={draft.expiresAt}
              onChange={(event) =>
                onDraft({ ...draft, expiresAt: event.target.value })
              }
              min={localDateTime(new Date().toISOString())}
              disabled={disabled}
            />
          </div>
        </form>
        <div className="hidden sm:block">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>凭据</TableHead>
                <TableHead>过期时间</TableHead>
                <TableHead>最后使用</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-16 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {credentials.map((credential) => (
                <TableRow key={credential.id}>
                  <TableCell>
                    <strong className="block text-sm">{credential.name}</strong>
                    <code className="block max-w-48 truncate font-mono text-xs text-muted-foreground">
                      {credential.id}
                    </code>
                  </TableCell>
                  <TableCell>{formatDate(credential.expires_at)}</TableCell>
                  <TableCell>{formatDate(credential.last_used_at)}</TableCell>
                  <TableCell>
                    <CredentialStatus credential={credential} />
                  </TableCell>
                  <TableCell className="text-right">
                    {!credential.revoked_at && (
                      <ActionButton
                        label={`撤销 ${credential.name}`}
                        destructive
                        disabled={busy}
                        onClick={() => onRevoke(credential)}
                      >
                        <Trash2 />
                      </ActionButton>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
        <div className="sm:hidden">
          {credentials.map((credential) => (
            <article key={credential.id} className="space-y-2 border-b py-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <strong className="block text-sm">{credential.name}</strong>
                  <code className="block truncate font-mono text-xs text-muted-foreground">
                    {credential.id}
                  </code>
                </div>
                <CredentialStatus credential={credential} />
              </div>
              <p className="text-xs text-muted-foreground">
                过期 {formatDate(credential.expires_at)} · 最后使用{" "}
                {formatDate(credential.last_used_at)}
              </p>
              {!credential.revoked_at && (
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={busy}
                  onClick={() => onRevoke(credential)}
                >
                  <Trash2 />
                  撤销
                </Button>
              )}
            </article>
          ))}
        </div>
        {(loading || credentials.length === 0) && (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {loading ? "正在加载凭据" : "暂无凭据"}
          </p>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            关闭
          </Button>
          <Button
            type="submit"
            form="scim-credential-form"
            disabled={busy || disabled}
          >
            <Plus />
            {busy ? "正在创建" : "创建凭据"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function ConfirmDialog({
  open,
  busy,
  title,
  description,
  onClose,
  onConfirm,
}: {
  open: boolean
  busy: boolean
  title: string
  description: string
  onClose: () => void
  onConfirm: () => void
}) {
  return (
    <Dialog open={open} onOpenChange={(value) => !value && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{description}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            取消
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={busy}>
            {busy ? "正在处理" : "确认撤销"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SecretDialog({
  value,
  endpoint,
  onCopy,
  onClose,
}: {
  value: SCIMCredentialSecret | null
  endpoint: string
  onCopy: (value: string, message: string) => Promise<void>
  onClose: () => void
}) {
  return (
    <Dialog open={value !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>保存 SCIM Bearer token</DialogTitle>
          <DialogDescription>
            token 仅显示一次，关闭后无法再次查看。
          </DialogDescription>
        </DialogHeader>
        <SecretField
          label="SCIM Base URL"
          value={endpoint}
          onCopy={() => onCopy(endpoint, "SCIM Base URL 已复制")}
        />
        <SecretField
          label="Bearer token"
          value={value?.bearer_token ?? ""}
          onCopy={() =>
            onCopy(value?.bearer_token ?? "", "SCIM Bearer token 已复制")
          }
        />
        <DialogFooter>
          <Button onClick={onClose}>
            <CheckCircle2 />
            已安全保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SecretField({
  label,
  value,
  onCopy,
}: {
  label: string
  value: string
  onCopy: () => Promise<void>
}) {
  return (
    <div className="grid gap-2">
      <Label>{label}</Label>
      <div className="flex gap-2">
        <Input value={value} readOnly className="font-mono text-xs" />
        <Button
          type="button"
          size="icon"
          variant="outline"
          aria-label={`复制${label}`}
          onClick={() => void onCopy()}
        >
          <Copy />
        </Button>
      </div>
    </div>
  )
}

function ActionButton({
  label,
  destructive = false,
  children,
  ...props
}: React.ButtonHTMLAttributes<HTMLButtonElement> & {
  label: string
  destructive?: boolean
}) {
  return (
    <Tooltip>
      <TooltipTrigger
        {...props}
        aria-label={label}
        className={buttonVariants({
          size: "icon",
          variant: "ghost",
          className: destructive ? "size-10 text-destructive" : "size-10",
        })}
      >
        {children}
      </TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  )
}

function DirectoryStatus({ status }: { status: SCIMDirectory["status"] }) {
  return (
    <Badge variant={status === "active" ? "default" : "outline"}>
      {status === "active" ? "启用" : "停用"}
    </Badge>
  )
}

function CredentialStatus({ credential }: { credential: SCIMCredential }) {
  const label = credential.revoked_at ? "已撤销" : "未撤销"
  return (
    <Badge variant={label === "未撤销" ? "default" : "outline"}>{label}</Badge>
  )
}

function EmptyDirectories() {
  return (
    <div>
      <FolderSync className="mx-auto mb-2 size-7 text-muted-foreground" />
      <p className="text-sm font-medium">尚未创建 SCIM 目录</p>
    </div>
  )
}

function SuccessNotice({ children }: { children: React.ReactNode }) {
  return (
    <div
      className="mb-4 flex items-start gap-2 rounded-md border border-emerald-600/30 bg-emerald-600/10 px-4 py-3 text-sm text-emerald-800 dark:text-emerald-300"
      role="status"
    >
      <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
      {children}
    </div>
  )
}

function scimEndpoint(): string {
  const api = (import.meta.env.VITE_API_URL ?? "/api").replace(/\/$/, "")
  if (typeof window === "undefined") return `${api}/scim/v2`
  return new URL(`${api}/scim/v2`, window.location.origin)
    .toString()
    .replace(/\/$/, "")
}

function localDateTime(value?: string): string {
  if (!value) return ""
  const date = new Date(value)
  const offset = date.getTimezoneOffset() * 60_000
  return new Date(date.getTime() - offset).toISOString().slice(0, 16)
}

function isoDateTime(value: string): string | undefined {
  return value ? new Date(value).toISOString() : undefined
}

function formatDate(value?: string): string {
  return value
    ? new Intl.DateTimeFormat("zh-CN", {
        dateStyle: "medium",
        timeStyle: "short",
      }).format(new Date(value))
    : "永不过期"
}
