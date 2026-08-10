import { useCallback, useEffect, useState, type FormEvent } from "react"
import {
  Bot,
  CheckCircle2,
  Copy,
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
import { Textarea } from "@workspace/ui/components/textarea"
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@workspace/ui/components/tooltip"
import { Alert } from "../../components/alert"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type ServiceAccount,
  type ServiceAccountCredential,
  type ServiceAccountInput,
  type ServiceAccountUpdate,
} from "../../lib/iam-api"

interface AccountDraft {
  loginName: string
  displayName: string
  description: string
  expiresAt: string
  enabled: boolean
}

interface CredentialDraft {
  name: string
  scopes: string
  expiresAt: string
}

interface SecretResult {
  clientID: string
  clientSecret: string
}

const emptyAccount: AccountDraft = {
  loginName: "",
  displayName: "",
  description: "",
  expiresAt: "",
  enabled: true,
}

const emptyCredential: CredentialDraft = {
  name: "",
  scopes: "chaosplus-api",
  expiresAt: "",
}

function accountDraft(account: ServiceAccount): AccountDraft {
  return {
    loginName: account.login_name,
    displayName: account.display_name,
    description: account.description ?? "",
    expiresAt: localDateTime(account.expires_at),
    enabled: account.status === "active",
  }
}

export default function ServiceAccountsPage() {
  const [accounts, setAccounts] = useState<ServiceAccount[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")
  const [editing, setEditing] = useState<ServiceAccount | null | undefined>()
  const [draft, setDraft] = useState<AccountDraft>(emptyAccount)
  const [deleting, setDeleting] = useState<ServiceAccount | null>(null)
  const [credentialAccount, setCredentialAccount] =
    useState<ServiceAccount | null>(null)
  const [credentials, setCredentials] = useState<ServiceAccountCredential[]>([])
  const [credentialsLoading, setCredentialsLoading] = useState(false)
  const [credentialDraft, setCredentialDraft] =
    useState<CredentialDraft>(emptyCredential)
  const [revokeCredential, setRevokeCredential] =
    useState<ServiceAccountCredential | null>(null)
  const [secret, setSecret] = useState<SecretResult | null>(null)

  const refresh = useCallback(async () => {
    setAccounts((await iamApi.serviceAccounts()).items)
  }, [])

  useEffect(() => {
    let active = true
    void iamApi
      .serviceAccounts()
      .then(
        (result) => active && setAccounts(result.items),
        (cause: Error) => active && setError(cause.message)
      )
      .finally(() => active && setLoading(false))
    return () => {
      active = false
    }
  }, [])

  const openCreate = () => {
    setError("")
    setDraft({ ...emptyAccount })
    setEditing(null)
  }

  const openEdit = (account: ServiceAccount) => {
    setError("")
    setDraft(accountDraft(account))
    setEditing(account)
  }

  const saveAccount = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    setNotice("")
    try {
      const expiresAt = isoDateTime(draft.expiresAt)
      if (editing) {
        const body: ServiceAccountUpdate = {
          display_name: draft.displayName.trim(),
          description: draft.description.trim() || undefined,
          status: draft.enabled ? "active" : "disabled",
          expires_at: expiresAt,
          version: editing.version,
        }
        await iamApi.updateServiceAccount(editing.id, body)
        setNotice("服务账号已更新，状态与有效期变更会立即撤销旧令牌")
      } else {
        const body: ServiceAccountInput = {
          login_name: draft.loginName.trim(),
          display_name: draft.displayName.trim() || undefined,
          description: draft.description.trim() || undefined,
          expires_at: expiresAt,
        }
        await iamApi.createServiceAccount(body)
        setNotice("服务账号已创建")
      }
      setEditing(undefined)
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const removeAccount = async () => {
    if (!deleting) return
    setBusy(true)
    setError("")
    try {
      await iamApi.deleteServiceAccount(deleting.id, deleting.version)
      setDeleting(null)
      setNotice("服务账号已删除，成员资格、凭据和旧令牌均已失效")
      await refresh()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const openCredentials = async (account: ServiceAccount) => {
    setCredentialAccount(account)
    setCredentialDraft({ ...emptyCredential })
    setCredentials([])
    setCredentialsLoading(true)
    setBusy(true)
    setError("")
    try {
      setCredentials(await iamApi.serviceAccountCredentials(account.id))
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setCredentialsLoading(false)
      setBusy(false)
    }
  }

  const createCredential = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!credentialAccount) return
    const scopes = uniqueWords(credentialDraft.scopes)
    if (scopes.length === 0) {
      setError("至少配置一个凭据 scope")
      return
    }
    setBusy(true)
    setError("")
    try {
      const result = await iamApi.createServiceAccountCredential(
        credentialAccount.id,
        {
          name: credentialDraft.name.trim(),
          scopes,
          expires_at: isoDateTime(credentialDraft.expiresAt),
        }
      )
      setCredentialAccount(null)
      setSecret({
        clientID: result.credential.id,
        clientSecret: result.client_secret,
      })
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const revoke = async () => {
    if (!credentialAccount || !revokeCredential) return
    setBusy(true)
    setError("")
    try {
      await iamApi.revokeServiceAccountCredential(
        credentialAccount.id,
        revokeCredential.id
      )
      setCredentials(
        await iamApi.serviceAccountCredentials(credentialAccount.id)
      )
      setRevokeCredential(null)
      setNotice("凭据及其签发的旧令牌已撤销")
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  return (
    <>
      <PageHeader
        title="服务账号"
        description="管理租户内的非交互式机器身份、有效期和 OAuth 客户端凭据"
        action={
          <Button onClick={openCreate}>
            <Plus />
            创建服务账号
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      {notice && <SuccessNotice>{notice}</SuccessNotice>}

      <section className="overflow-hidden rounded-md border bg-card">
        <header className="flex min-h-14 items-center justify-between gap-4 border-b bg-muted/20 px-4">
          <div className="flex items-center gap-2 text-sm font-semibold">
            <Bot className="size-4 text-muted-foreground" />
            机器身份
          </div>
          <span className="text-xs text-muted-foreground">
            {loading ? "正在加载" : `${accounts.length} 个账号`}
          </span>
        </header>
        <div className="hidden md:block">
          <Table containerClassName="min-h-56">
            <TableHeader>
              <TableRow>
                <TableHead className="min-w-64">账号</TableHead>
                <TableHead className="min-w-56">说明</TableHead>
                <TableHead className="min-w-44">有效期</TableHead>
                <TableHead className="w-24">状态</TableHead>
                <TableHead className="w-40 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {accounts.map((account) => (
                <TableRow key={account.id}>
                  <TableCell>
                    <strong className="block text-sm">
                      {account.display_name}
                    </strong>
                    <span className="block text-xs text-muted-foreground">
                      {account.login_name}
                    </span>
                    <code className="mt-1 block max-w-72 truncate font-mono text-xs text-muted-foreground">
                      {account.id}
                    </code>
                  </TableCell>
                  <TableCell className="max-w-72 truncate text-sm text-muted-foreground">
                    {account.description || "-"}
                  </TableCell>
                  <TableCell>{formatDate(account.expires_at)}</TableCell>
                  <TableCell>
                    <StatusBadge status={account.status} />
                  </TableCell>
                  <TableCell className="text-right">
                    <AccountActions
                      account={account}
                      busy={busy}
                      onCredentials={() => void openCredentials(account)}
                      onEdit={() => openEdit(account)}
                      onDelete={() => setDeleting(account)}
                    />
                  </TableCell>
                </TableRow>
              ))}
              {!loading && accounts.length === 0 && (
                <TableRow>
                  <TableCell colSpan={5} className="h-52 text-center">
                    <EmptyAccounts />
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
        <div className="md:hidden">
          {accounts.map((account) => (
            <article
              key={account.id}
              className="space-y-3 border-b p-4 last:border-0"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <strong className="block text-sm break-words">
                    {account.display_name}
                  </strong>
                  <span className="text-xs text-muted-foreground">
                    {account.login_name}
                  </span>
                </div>
                <StatusBadge status={account.status} />
              </div>
              <p className="text-sm break-words text-muted-foreground">
                {account.description || "无说明"}
              </p>
              <div className="text-xs text-muted-foreground">
                有效期：{formatDate(account.expires_at)}
              </div>
              <AccountActions
                account={account}
                busy={busy}
                onCredentials={() => void openCredentials(account)}
                onEdit={() => openEdit(account)}
                onDelete={() => setDeleting(account)}
              />
            </article>
          ))}
          {!loading && accounts.length === 0 && (
            <div className="grid h-52 place-items-center text-center">
              <EmptyAccounts />
            </div>
          )}
        </div>
      </section>

      <AccountEditor
        open={editing !== undefined}
        account={editing ?? null}
        draft={draft}
        busy={busy}
        onDraft={setDraft}
        onOpenChange={(open) => !open && !busy && setEditing(undefined)}
        onSubmit={saveAccount}
      />
      <CredentialsDialog
        account={credentialAccount}
        credentials={credentials}
        loading={credentialsLoading}
        draft={credentialDraft}
        busy={busy}
        onDraft={setCredentialDraft}
        onClose={() => !busy && setCredentialAccount(null)}
        onSubmit={createCredential}
        onRevoke={setRevokeCredential}
      />
      <ConfirmDialog
        open={deleting !== null}
        title="删除服务账号"
        description={`将删除 ${deleting?.display_name ?? "该服务账号"}，并立即撤销其全部凭据与令牌。`}
        busy={busy}
        onClose={() => !busy && setDeleting(null)}
        onConfirm={() => void removeAccount()}
      />
      <ConfirmDialog
        open={revokeCredential !== null}
        title="撤销凭据"
        description={`将撤销 ${revokeCredential?.name ?? "该凭据"}，由此凭据签发的令牌会立即失效。`}
        busy={busy}
        onClose={() => !busy && setRevokeCredential(null)}
        onConfirm={() => void revoke()}
      />
      <SecretDialog value={secret} onClose={() => setSecret(null)} />
    </>
  )
}

function AccountActions({
  account,
  busy,
  onCredentials,
  onEdit,
  onDelete,
}: {
  account: ServiceAccount
  busy: boolean
  onCredentials: () => void
  onEdit: () => void
  onDelete: () => void
}) {
  return (
    <TooltipProvider>
      <div className="flex justify-end gap-1">
        <ActionButton
          label={`管理 ${account.display_name} 的凭据`}
          onClick={onCredentials}
          disabled={busy}
        >
          <KeyRound />
        </ActionButton>
        <ActionButton
          label={`编辑 ${account.display_name}`}
          onClick={onEdit}
          disabled={busy}
        >
          <Pencil />
        </ActionButton>
        <ActionButton
          label={`删除 ${account.display_name}`}
          onClick={onDelete}
          disabled={busy}
          destructive
        >
          <Trash2 />
        </ActionButton>
      </div>
    </TooltipProvider>
  )
}

function AccountEditor({
  open,
  account,
  draft,
  busy,
  onDraft,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  account: ServiceAccount | null
  draft: AccountDraft
  busy: boolean
  onDraft: (value: AccountDraft) => void
  onOpenChange: (open: boolean) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{account ? "编辑服务账号" : "创建服务账号"}</DialogTitle>
          <DialogDescription>
            {account ? account.login_name : "服务账号不能使用密码或浏览器登录"}
          </DialogDescription>
        </DialogHeader>
        <form
          id="service-account-form"
          className="space-y-4"
          onSubmit={onSubmit}
        >
          <Field label="登录名" htmlFor="service-account-login">
            <Input
              id="service-account-login"
              value={draft.loginName}
              disabled={Boolean(account) || busy}
              required
              maxLength={200}
              onChange={(event) =>
                onDraft({ ...draft, loginName: event.target.value })
              }
            />
          </Field>
          <Field label="显示名称" htmlFor="service-account-display">
            <Input
              id="service-account-display"
              value={draft.displayName}
              disabled={busy}
              required={Boolean(account)}
              maxLength={128}
              onChange={(event) =>
                onDraft({ ...draft, displayName: event.target.value })
              }
            />
          </Field>
          <Field label="说明" htmlFor="service-account-description">
            <Textarea
              id="service-account-description"
              value={draft.description}
              disabled={busy}
              maxLength={1000}
              rows={3}
              onChange={(event) =>
                onDraft({ ...draft, description: event.target.value })
              }
            />
          </Field>
          <Field label="有效期" htmlFor="service-account-expiry">
            <Input
              id="service-account-expiry"
              type="datetime-local"
              value={draft.expiresAt}
              disabled={busy}
              onChange={(event) =>
                onDraft({ ...draft, expiresAt: event.target.value })
              }
            />
          </Field>
          {account && (
            <div className="flex min-h-11 items-center justify-between gap-4 rounded-md border px-3">
              <Label htmlFor="service-account-enabled">启用账号</Label>
              <Switch
                id="service-account-enabled"
                checked={draft.enabled}
                disabled={busy}
                onCheckedChange={(enabled) => onDraft({ ...draft, enabled })}
              />
            </div>
          )}
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
            disabled={busy}
          >
            取消
          </Button>
          <Button type="submit" form="service-account-form" disabled={busy}>
            {busy ? "正在保存" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function CredentialsDialog({
  account,
  credentials,
  loading,
  draft,
  busy,
  onDraft,
  onClose,
  onSubmit,
  onRevoke,
}: {
  account: ServiceAccount | null
  credentials: ServiceAccountCredential[]
  loading: boolean
  draft: CredentialDraft
  busy: boolean
  onDraft: (value: CredentialDraft) => void
  onClose: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onRevoke: (credential: ServiceAccountCredential) => void
}) {
  return (
    <Dialog open={account !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-h-[calc(100vh-2rem)] overflow-y-auto sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>服务账号凭据</DialogTitle>
          <DialogDescription>{account?.display_name}</DialogDescription>
        </DialogHeader>
        <form
          id="service-credential-form"
          className="grid gap-3 md:grid-cols-3"
          onSubmit={onSubmit}
        >
          <Field label="凭据名称" htmlFor="service-credential-name">
            <Input
              id="service-credential-name"
              value={draft.name}
              disabled={busy}
              required
              maxLength={128}
              onChange={(event) =>
                onDraft({ ...draft, name: event.target.value })
              }
            />
          </Field>
          <Field label="Scopes" htmlFor="service-credential-scopes">
            <Input
              id="service-credential-scopes"
              value={draft.scopes}
              disabled={busy}
              required
              onChange={(event) =>
                onDraft({ ...draft, scopes: event.target.value })
              }
            />
          </Field>
          <Field label="有效期" htmlFor="service-credential-expiry">
            <Input
              id="service-credential-expiry"
              type="datetime-local"
              value={draft.expiresAt}
              disabled={busy}
              onChange={(event) =>
                onDraft({ ...draft, expiresAt: event.target.value })
              }
            />
          </Field>
        </form>
        <div className="overflow-x-auto rounded-md border">
          <Table containerClassName="min-w-[680px]">
            <TableHeader>
              <TableRow>
                <TableHead>名称</TableHead>
                <TableHead>客户端 ID</TableHead>
                <TableHead>Scopes</TableHead>
                <TableHead>有效期</TableHead>
                <TableHead>最近使用</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="w-20 text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {credentials.map((credential) => (
                <TableRow key={credential.id}>
                  <TableCell>{credential.name}</TableCell>
                  <TableCell>
                    <code className="font-mono text-xs">{credential.id}</code>
                  </TableCell>
                  <TableCell className="max-w-48 truncate">
                    {credential.scopes.join(" ")}
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
                        onClick={() => onRevoke(credential)}
                        disabled={busy}
                        destructive
                      >
                        <Trash2 />
                      </ActionButton>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {(loading || credentials.length === 0) && (
                <TableRow>
                  <TableCell
                    colSpan={7}
                    className="h-24 text-center text-muted-foreground"
                  >
                    {loading ? "正在加载凭据" : "暂无凭据"}
                  </TableCell>
                </TableRow>
              )}
            </TableBody>
          </Table>
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={onClose}
            disabled={busy}
          >
            关闭
          </Button>
          <Button type="submit" form="service-credential-form" disabled={busy}>
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
  title,
  description,
  busy,
  onClose,
  onConfirm,
}: {
  open: boolean
  title: string
  description: string
  busy: boolean
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
            {busy ? "正在处理" : "确认"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function SecretDialog({
  value,
  onClose,
}: {
  value: SecretResult | null
  onClose: () => void
}) {
  const copy = async (text: string) => navigator.clipboard.writeText(text)
  return (
    <Dialog open={value !== null} onOpenChange={(open) => !open && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>保存客户端凭据</DialogTitle>
          <DialogDescription>客户端密钥仅显示一次</DialogDescription>
        </DialogHeader>
        <SecretField
          label="客户端 ID"
          value={value?.clientID ?? ""}
          onCopy={copy}
        />
        <SecretField
          label="客户端密钥"
          value={value?.clientSecret ?? ""}
          onCopy={copy}
        />
        <DialogFooter>
          <Button onClick={onClose}>完成</Button>
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
  onCopy: (value: string) => Promise<void>
}) {
  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div className="flex gap-2">
        <Input value={value} readOnly className="font-mono" />
        <Button
          type="button"
          size="icon"
          variant="outline"
          aria-label={`复制${label}`}
          onClick={() => void onCopy(value)}
        >
          <Copy />
        </Button>
      </div>
    </div>
  )
}

function Field({
  label,
  htmlFor,
  children,
}: {
  label: string
  htmlFor: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={htmlFor}>{label}</Label>
      {children}
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

function StatusBadge({ status }: { status: ServiceAccount["status"] }) {
  return (
    <Badge
      variant={status === "active" ? "default" : "outline"}
      className="shrink-0 whitespace-nowrap"
    >
      {status === "active" ? "启用" : "停用"}
    </Badge>
  )
}

function CredentialStatus({
  credential,
}: {
  credential: ServiceAccountCredential
}) {
  const label = credential.revoked_at ? "已撤销" : "未撤销"
  return (
    <Badge variant={label === "未撤销" ? "default" : "outline"}>{label}</Badge>
  )
}

function EmptyAccounts() {
  return (
    <div>
      <Bot className="mx-auto mb-2 size-7 text-muted-foreground" />
      <p className="text-sm font-medium">尚未创建服务账号</p>
    </div>
  )
}

function SuccessNotice({ children }: { children: React.ReactNode }) {
  return (
    <div className="mb-4 flex items-start gap-2 rounded-md border border-emerald-600/30 bg-emerald-600/10 px-4 py-3 text-sm text-emerald-800 dark:text-emerald-300">
      <CheckCircle2 className="mt-0.5 size-4 shrink-0" />
      {children}
    </div>
  )
}

function uniqueWords(value: string): string[] {
  return [
    ...new Set(
      value
        .split(/\s+/)
        .map((item) => item.trim())
        .filter(Boolean)
    ),
  ]
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
