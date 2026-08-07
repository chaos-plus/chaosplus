import { useEffect, useState, type FormEvent } from "react"
import {
  CheckCircle2,
  Copy,
  Download,
  Fingerprint,
  KeyRound,
  Laptop,
  LogOut,
  Mail,
  Pencil,
  Plus,
  RefreshCw,
  ShieldCheck,
  ShieldOff,
  Smartphone,
  Trash2,
} from "lucide-react"
import { QRCodeSVG } from "qrcode.react"
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
import { Alert } from "../../components/alert"
import { useAuth } from "../../components/auth"
import { PageHeader } from "../../components/page-header"
import {
  iamApi,
  type BrowserSession,
  type MfaEnrollment,
  type MfaStatus,
  type Passkey,
  type Session,
} from "../../lib/iam-api"
import { createPasskeyCredential } from "../../lib/webauthn"

type MfaDialog =
  | "enroll-password"
  | "enroll-confirm"
  | "recovery"
  | "regenerate"
  | "disable"
  | null

type PasskeyDialog =
  | { type: "register" }
  | { type: "rename"; passkey: Passkey }
  | { type: "delete"; passkey: Passkey }
  | null

export default function SecurityPage() {
  const { session } = useAuth()
  const [sessions, setSessions] = useState<BrowserSession[]>([])
  const [mfa, setMfa] = useState<MfaStatus | null>(null)
  const [passkeys, setPasskeys] = useState<Passkey[]>([])
  const [passwordOpen, setPasswordOpen] = useState(false)
  const [mfaDialog, setMfaDialog] = useState<MfaDialog>(null)
  const [passkeyDialog, setPasskeyDialog] = useState<PasskeyDialog>(null)
  const [enrollment, setEnrollment] = useState<MfaEnrollment | null>(null)
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [notice, setNotice] = useState("")

  const refresh = async () => {
    const [nextSessions, nextMfa, nextPasskeys] = await Promise.all([
      iamApi.sessions(),
      iamApi.mfaStatus(),
      iamApi.passkeys(),
    ])
    setSessions(nextSessions)
    setMfa(nextMfa)
    setPasskeys(nextPasskeys)
  }

  useEffect(() => {
    let active = true
    void Promise.all([
      iamApi.sessions(),
      iamApi.mfaStatus(),
      iamApi.passkeys(),
    ]).then(
      ([nextSessions, nextMfa, nextPasskeys]) => {
        if (!active) return
        setSessions(nextSessions)
        setMfa(nextMfa)
        setPasskeys(nextPasskeys)
      },
      (cause: Error) => {
        if (active) setError(cause.message)
      }
    )
    return () => {
      active = false
    }
  }, [])

  const run = async (operation: () => Promise<void>) => {
    setBusy(true)
    setError("")
    setNotice("")
    try {
      await operation()
    } catch (cause) {
      setError((cause as Error).message)
    } finally {
      setBusy(false)
    }
  }

  const revoke = (session: BrowserSession) =>
    run(async () => {
      await iamApi.revokeSession(session.id)
      if (session.current) window.location.assign("/login")
      else {
        await refresh()
        setNotice("会话已撤销")
      }
    })

  const logoutAll = () =>
    run(async () => {
      await iamApi.logoutAll()
      window.location.assign("/login")
    })

  const sendEmailVerification = () =>
    run(async () => {
      await iamApi.startEmailVerification()
      setNotice("验证邮件已进入发送队列")
    })

  const changePassword = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    void run(async () => {
      await iamApi.changePassword({
        current_password: String(data.get("current_password") ?? ""),
        new_password: String(data.get("new_password") ?? ""),
      })
      form.reset()
      setPasswordOpen(false)
      setNotice("密码已更新，其他设备会话已撤销")
      await refresh()
    })
  }

  const beginEnrollment = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    void run(async () => {
      const next = await iamApi.beginTotpEnrollment(
        String(data.get("current_password") ?? "")
      )
      form.reset()
      setEnrollment(next)
      setMfaDialog("enroll-confirm")
    })
  }

  const confirmEnrollment = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    void run(async () => {
      const result = await iamApi.confirmTotpEnrollment(
        String(data.get("code") ?? "").trim()
      )
      form.reset()
      setEnrollment(null)
      setRecoveryCodes(result.recovery_codes)
      setMfaDialog("recovery")
      await refresh()
    })
  }

  const verifyMfaChange = (
    event: FormEvent<HTMLFormElement>,
    action: "regenerate" | "disable"
  ) => {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    const input = {
      current_password: String(data.get("current_password") ?? ""),
      code: String(data.get("code") ?? "").trim(),
    }
    void run(async () => {
      if (action === "regenerate") {
        const result = await iamApi.regenerateRecoveryCodes(input)
        setRecoveryCodes(result.recovery_codes)
        setMfaDialog("recovery")
      } else {
        await iamApi.disableTotp(input)
        setMfaDialog(null)
        setNotice("多因素认证已停用，其他设备会话已撤销")
      }
      form.reset()
      await refresh()
    })
  }

  const closeMfaDialog = (open: boolean) => {
    if (open) return
    if (mfaDialog === "recovery") setRecoveryCodes([])
    if (mfaDialog === "enroll-confirm") setEnrollment(null)
    setMfaDialog(null)
  }

  const registerPasskey = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    const form = event.currentTarget
    const data = new FormData(form)
    void run(async () => {
      const options = await iamApi.beginPasskeyRegistration(
        String(data.get("current_password") ?? "")
      )
      const credential = await createPasskeyCredential(options.options)
      await iamApi.finishPasskeyRegistration({
        challenge_id: options.challenge_id,
        name: String(data.get("name") ?? "").trim(),
        credential,
      })
      form.reset()
      setPasskeyDialog(null)
      setNotice("通行密钥已添加")
      await refresh()
    })
  }

  const renamePasskey = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (passkeyDialog?.type !== "rename") return
    const form = event.currentTarget
    const data = new FormData(form)
    const id = passkeyDialog.passkey.id
    void run(async () => {
      await iamApi.renamePasskey(id, String(data.get("name") ?? "").trim())
      setPasskeyDialog(null)
      setNotice("通行密钥名称已更新")
      await refresh()
    })
  }

  const deletePasskey = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (passkeyDialog?.type !== "delete") return
    const form = event.currentTarget
    const data = new FormData(form)
    const id = passkeyDialog.passkey.id
    void run(async () => {
      await iamApi.deletePasskey(id, String(data.get("current_password") ?? ""))
      form.reset()
      setPasskeyDialog(null)
      setNotice("通行密钥已删除，其他设备会话已撤销")
      await refresh()
    })
  }

  return (
    <>
      <PageHeader
        title="安全中心"
        description="管理密码、通行密钥、多因素认证和已登录设备"
        action={
          <Button onClick={() => setPasswordOpen(true)}>
            <KeyRound />
            修改密码
          </Button>
        }
      />
      {error && <Alert>{error}</Alert>}
      {notice && <SuccessNotice>{notice}</SuccessNotice>}

      <EmailSection
        session={session}
        busy={busy}
        onSend={sendEmailVerification}
      />

      <MfaSection
        status={mfa}
        busy={busy}
        onEnable={() => setMfaDialog("enroll-password")}
        onRegenerate={() => setMfaDialog("regenerate")}
        onDisable={() => setMfaDialog("disable")}
      />

      <PasskeySection
        passkeys={passkeys}
        busy={busy}
        onRegister={() => setPasskeyDialog({ type: "register" })}
        onRename={(passkey) => setPasskeyDialog({ type: "rename", passkey })}
        onDelete={(passkey) => setPasskeyDialog({ type: "delete", passkey })}
      />

      <SessionSection
        sessions={sessions}
        busy={busy}
        onLogoutAll={logoutAll}
        onRevoke={revoke}
      />

      <PasswordDialog
        open={passwordOpen}
        busy={busy}
        onOpenChange={setPasswordOpen}
        onSubmit={changePassword}
      />
      <MfaFlowDialog
        state={mfaDialog}
        enrollment={enrollment}
        recoveryCodes={recoveryCodes}
        busy={busy}
        onOpenChange={closeMfaDialog}
        onBegin={beginEnrollment}
        onConfirm={confirmEnrollment}
        onRegenerate={(event) => verifyMfaChange(event, "regenerate")}
        onDisable={(event) => verifyMfaChange(event, "disable")}
      />
      <PasskeyFlowDialog
        state={passkeyDialog}
        busy={busy}
        onOpenChange={(open) => !open && setPasskeyDialog(null)}
        onRegister={registerPasskey}
        onRename={renamePasskey}
        onDelete={deletePasskey}
      />
    </>
  )
}

function EmailSection({
  session,
  busy,
  onSend,
}: {
  session: Session | null
  busy: boolean
  onSend: () => void
}) {
  const email = session?.email ?? ""
  const verified = Boolean(session?.email_verified)
  return (
    <section className="mb-5 overflow-hidden rounded-md border bg-card">
      <header className="flex min-h-20 flex-wrap items-center justify-between gap-4 border-b px-5 py-4">
        <div className="flex min-w-0 items-start gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <Mail size={20} />
          </span>
          <div className="min-w-0">
            <h2 className="font-semibold">主邮箱</h2>
            <p className="mt-1 truncate text-sm text-muted-foreground">
              {email || "尚未设置邮箱"}
            </p>
          </div>
        </div>
        {verified ? (
          <span className="inline-flex min-h-9 items-center gap-2 text-sm font-medium text-emerald-600 dark:text-emerald-400">
            <CheckCircle2 size={17} />
            已验证
          </span>
        ) : email ? (
          <Button disabled={busy} onClick={onSend}>
            <Mail />
            发送验证邮件
          </Button>
        ) : (
          <span className="text-sm text-muted-foreground">未配置</span>
        )}
      </header>
    </section>
  )
}

function PasskeySection({
  passkeys,
  busy,
  onRegister,
  onRename,
  onDelete,
}: {
  passkeys: Passkey[]
  busy: boolean
  onRegister: () => void
  onRename: (passkey: Passkey) => void
  onDelete: (passkey: Passkey) => void
}) {
  return (
    <section className="mb-5 overflow-hidden rounded-md border bg-card">
      <header className="flex min-h-20 flex-wrap items-center justify-between gap-4 border-b px-5 py-4">
        <div className="flex min-w-0 items-start gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <Fingerprint size={20} />
          </span>
          <div>
            <h2 className="font-semibold">通行密钥</h2>
            <p className="mt-1 text-xs text-muted-foreground">
              使用设备解锁或安全密钥进行无密码登录
            </p>
          </div>
        </div>
        <Button disabled={busy} onClick={onRegister}>
          <Plus />
          添加通行密钥
        </Button>
      </header>
      {passkeys.map((passkey) => (
        <article
          key={passkey.id}
          className="grid min-h-20 grid-cols-[40px_minmax(0,1fr)_36px_36px] items-center gap-3 border-b px-5 py-3 last:border-0"
        >
          <span className="grid size-10 place-items-center rounded-md bg-primary/10 text-primary">
            <KeyRound size={20} />
          </span>
          <div className="min-w-0">
            <strong className="block truncate text-sm">{passkey.name}</strong>
            <small className="mt-1 block text-xs text-muted-foreground">
              {passkey.last_used_at
                ? `最近使用 ${new Date(passkey.last_used_at).toLocaleString()}`
                : `添加于 ${new Date(passkey.created_at).toLocaleString()}`}
            </small>
          </div>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            disabled={busy}
            onClick={() => onRename(passkey)}
            aria-label={`重命名通行密钥 ${passkey.name}`}
            title="重命名"
          >
            <Pencil />
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="icon"
            disabled={busy}
            onClick={() => onDelete(passkey)}
            aria-label={`删除通行密钥 ${passkey.name}`}
            title="删除"
          >
            <Trash2 className="text-destructive" />
          </Button>
        </article>
      ))}
      {passkeys.length === 0 && (
        <div className="grid min-h-32 place-items-center px-5 text-sm text-muted-foreground">
          尚未添加通行密钥
        </div>
      )}
    </section>
  )
}

function PasskeyFlowDialog({
  state,
  busy,
  onOpenChange,
  onRegister,
  onRename,
  onDelete,
}: {
  state: PasskeyDialog
  busy: boolean
  onOpenChange: (open: boolean) => void
  onRegister: (event: FormEvent<HTMLFormElement>) => void
  onRename: (event: FormEvent<HTMLFormElement>) => void
  onDelete: (event: FormEvent<HTMLFormElement>) => void
}) {
  const formID = state ? `passkey-${state.type}-form` : "passkey-form"
  return (
    <Dialog open={state !== null} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {state?.type === "register"
              ? "添加通行密钥"
              : state?.type === "rename"
                ? "重命名通行密钥"
                : "删除通行密钥"}
          </DialogTitle>
          <DialogDescription>
            {state?.type === "register"
              ? "验证当前密码后，按浏览器提示解锁设备或使用安全密钥。"
              : state?.type === "rename"
                ? "使用易识别的名称区分设备和安全密钥。"
                : "删除后该凭据立即失效，并撤销其他设备会话。"}
          </DialogDescription>
        </DialogHeader>
        <form
          id={formID}
          className="grid gap-4"
          onSubmit={
            state?.type === "register"
              ? onRegister
              : state?.type === "rename"
                ? onRename
                : onDelete
          }
        >
          {state?.type !== "delete" && (
            <div className="grid gap-2">
              <Label htmlFor={`${formID}-name`}>名称</Label>
              <Input
                id={`${formID}-name`}
                name="name"
                defaultValue={
                  state?.type === "rename" ? state.passkey.name : ""
                }
                autoComplete="off"
                required
                maxLength={200}
              />
            </div>
          )}
          {state?.type !== "rename" && (
            <PasswordField id={`${formID}-password`} label="当前密码" />
          )}
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button
            type="submit"
            form={formID}
            variant={state?.type === "delete" ? "destructive" : "default"}
            disabled={busy}
          >
            {busy ? "处理中" : state?.type === "delete" ? "确认删除" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MfaSection({
  status,
  busy,
  onEnable,
  onRegenerate,
  onDisable,
}: {
  status: MfaStatus | null
  busy: boolean
  onEnable: () => void
  onRegenerate: () => void
  onDisable: () => void
}) {
  return (
    <section className="mb-5 overflow-hidden rounded-md border bg-card">
      <header className="flex min-h-20 flex-wrap items-center justify-between gap-4 px-5 py-4">
        <div className="flex min-w-0 items-start gap-3">
          <span className="grid size-10 shrink-0 place-items-center rounded-md bg-primary/10 text-primary">
            <ShieldCheck size={20} />
          </span>
          <div>
            <div className="flex flex-wrap items-center gap-2">
              <h2 className="font-semibold">多因素认证</h2>
              {status?.totp_enabled && (
                <span className="rounded bg-success/10 px-2 py-0.5 text-xs font-semibold text-success">
                  已启用
                </span>
              )}
            </div>
            <p className="mt-1 text-xs text-muted-foreground">
              {status?.totp_enabled
                ? `验证器已绑定，剩余 ${status.recovery_codes_remaining} 枚恢复码`
                : "使用验证器动态码保护账号登录"}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap gap-2">
          {status?.totp_enabled ? (
            <>
              <Button variant="outline" disabled={busy} onClick={onRegenerate}>
                <RefreshCw />
                更新恢复码
              </Button>
              <Button variant="destructive" disabled={busy} onClick={onDisable}>
                <ShieldOff />
                停用
              </Button>
            </>
          ) : (
            <Button disabled={busy || status === null} onClick={onEnable}>
              <Smartphone />
              启用验证器
            </Button>
          )}
        </div>
      </header>
    </section>
  )
}

function SessionSection({
  sessions,
  busy,
  onLogoutAll,
  onRevoke,
}: {
  sessions: BrowserSession[]
  busy: boolean
  onLogoutAll: () => void
  onRevoke: (session: BrowserSession) => void
}) {
  return (
    <section className="overflow-hidden rounded-md border bg-card">
      <header className="flex min-h-20 flex-wrap items-center justify-between gap-4 border-b px-5 py-4">
        <div>
          <h2 className="font-semibold">登录设备</h2>
          <p className="mt-1 text-xs text-muted-foreground">
            发现不认识的设备时立即撤销该会话
          </p>
        </div>
        <Button
          variant="destructive"
          disabled={busy || sessions.length === 0}
          onClick={onLogoutAll}
        >
          <LogOut />
          退出全部设备
        </Button>
      </header>
      <div>
        {sessions.map((session) => (
          <SessionRow
            key={session.id}
            session={session}
            busy={busy}
            onRevoke={() => onRevoke(session)}
          />
        ))}
        {sessions.length === 0 && (
          <div className="grid min-h-40 place-items-center text-sm text-muted-foreground">
            <div className="text-center">
              <ShieldCheck className="mx-auto mb-2 size-7" />
              暂无活动会话
            </div>
          </div>
        )}
      </div>
    </section>
  )
}

function PasswordDialog({
  open,
  busy,
  onOpenChange,
  onSubmit,
}: {
  open: boolean
  busy: boolean
  onOpenChange: (open: boolean) => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>修改密码</DialogTitle>
          <DialogDescription>
            更新后保留当前设备，撤销其他会话和全部刷新令牌
          </DialogDescription>
        </DialogHeader>
        <form id="password-form" className="grid gap-4" onSubmit={onSubmit}>
          <PasswordField id="current-password" label="当前密码" />
          <div className="grid gap-2">
            <Label htmlFor="new-password">新密码</Label>
            <Input
              id="new-password"
              name="new_password"
              type="password"
              autoComplete="new-password"
              required
              minLength={12}
              maxLength={1024}
            />
          </div>
        </form>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            取消
          </Button>
          <Button type="submit" form="password-form" disabled={busy}>
            {busy ? "保存中" : "保存"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function MfaFlowDialog({
  state,
  enrollment,
  recoveryCodes,
  busy,
  onOpenChange,
  onBegin,
  onConfirm,
  onRegenerate,
  onDisable,
}: {
  state: MfaDialog
  enrollment: MfaEnrollment | null
  recoveryCodes: string[]
  busy: boolean
  onOpenChange: (open: boolean) => void
  onBegin: (event: FormEvent<HTMLFormElement>) => void
  onConfirm: (event: FormEvent<HTMLFormElement>) => void
  onRegenerate: (event: FormEvent<HTMLFormElement>) => void
  onDisable: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <Dialog open={state !== null} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-xl">
        {state === "enroll-password" && (
          <VerificationForm
            id="mfa-enroll-password-form"
            title="启用验证器"
            description="先验证当前密码，然后绑定 TOTP 验证器"
            busy={busy}
            submitLabel="继续"
            passwordOnly
            onSubmit={onBegin}
          />
        )}
        {state === "enroll-confirm" && enrollment && (
          <EnrollmentForm
            enrollment={enrollment}
            busy={busy}
            onSubmit={onConfirm}
          />
        )}
        {state === "recovery" && (
          <RecoveryCodes
            codes={recoveryCodes}
            onDone={() => onOpenChange(false)}
          />
        )}
        {state === "regenerate" && (
          <VerificationForm
            id="mfa-regenerate-form"
            title="更新恢复码"
            description="输入当前密码和动态码或现有恢复码。旧恢复码将立即失效。"
            busy={busy}
            submitLabel="生成新恢复码"
            onSubmit={onRegenerate}
          />
        )}
        {state === "disable" && (
          <VerificationForm
            id="mfa-disable-form"
            title="停用多因素认证"
            description="输入当前密码和动态码或恢复码。停用后账号仅使用密码登录。"
            busy={busy}
            submitLabel="确认停用"
            destructive
            onSubmit={onDisable}
          />
        )}
      </DialogContent>
    </Dialog>
  )
}

function VerificationForm({
  id,
  title,
  description,
  busy,
  submitLabel,
  passwordOnly = false,
  destructive = false,
  onSubmit,
}: {
  id: string
  title: string
  description: string
  busy: boolean
  submitLabel: string
  passwordOnly?: boolean
  destructive?: boolean
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <>
      <DialogHeader>
        <DialogTitle>{title}</DialogTitle>
        <DialogDescription>{description}</DialogDescription>
      </DialogHeader>
      <form id={id} className="grid gap-4" onSubmit={onSubmit}>
        <PasswordField id={`${id}-password`} label="当前密码" />
        {!passwordOnly && <CodeField id={`${id}-code`} />}
      </form>
      <DialogFooter>
        <Button
          type="submit"
          form={id}
          variant={destructive ? "destructive" : "default"}
          disabled={busy}
        >
          {busy ? "处理中" : submitLabel}
        </Button>
      </DialogFooter>
    </>
  )
}

function EnrollmentForm({
  enrollment,
  busy,
  onSubmit,
}: {
  enrollment: MfaEnrollment
  busy: boolean
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
}) {
  return (
    <>
      <DialogHeader>
        <DialogTitle>绑定验证器</DialogTitle>
        <DialogDescription>
          使用验证器扫描二维码，再输入生成的 6 位动态码
        </DialogDescription>
      </DialogHeader>
      <div className="grid gap-4 sm:grid-cols-[176px_minmax(0,1fr)] sm:items-center">
        <div className="mx-auto grid size-44 place-items-center rounded-md border bg-white p-3">
          <QRCodeSVG
            value={enrollment.provisioning_uri}
            title="Chaosplus TOTP 配置二维码"
            size={152}
            level="M"
          />
        </div>
        <div className="min-w-0 space-y-2 text-sm">
          <p className="text-muted-foreground">无法扫码时手动输入密钥：</p>
          <code className="block rounded-md border bg-muted px-3 py-2 font-mono text-xs break-all">
            {enrollment.secret}
          </code>
          <p className="text-xs text-muted-foreground">
            绑定请求在 {new Date(enrollment.expires_at).toLocaleTimeString()}{" "}
            前有效
          </p>
        </div>
      </div>
      <form id="mfa-confirm-form" onSubmit={onSubmit}>
        <CodeField id="mfa-confirm-code" totpOnly />
      </form>
      <DialogFooter>
        <Button type="submit" form="mfa-confirm-form" disabled={busy}>
          {busy ? "正在确认" : "确认并启用"}
        </Button>
      </DialogFooter>
    </>
  )
}

function RecoveryCodes({
  codes,
  onDone,
}: {
  codes: string[]
  onDone: () => void
}) {
  const content = `${codes.join("\n")}\n`
  const copy = () => void navigator.clipboard.writeText(content)
  const download = () => {
    const url = URL.createObjectURL(
      new Blob([content], { type: "text/plain;charset=utf-8" })
    )
    const anchor = document.createElement("a")
    anchor.href = url
    anchor.download = "chaosplus-recovery-codes.txt"
    anchor.click()
    URL.revokeObjectURL(url)
  }
  return (
    <>
      <DialogHeader>
        <DialogTitle>保存恢复码</DialogTitle>
        <DialogDescription>
          每枚恢复码只能使用一次，关闭后将无法再次查看
        </DialogDescription>
      </DialogHeader>
      <div className="grid grid-cols-1 gap-2 rounded-md border bg-muted/40 p-3 font-mono text-sm sm:grid-cols-2">
        {codes.map((code) => (
          <code
            key={code}
            className="rounded bg-background px-2 py-1.5 text-center text-xs break-all"
          >
            {code}
          </code>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button type="button" variant="outline" onClick={copy}>
          <Copy />
          复制
        </Button>
        <Button type="button" variant="outline" onClick={download}>
          <Download />
          下载
        </Button>
      </div>
      <DialogFooter>
        <Button type="button" onClick={onDone}>
          <CheckCircle2 />
          已安全保存
        </Button>
      </DialogFooter>
    </>
  )
}

function PasswordField({ id, label }: { id: string; label: string }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{label}</Label>
      <Input
        id={id}
        name="current_password"
        type="password"
        autoComplete="current-password"
        required
        maxLength={1024}
      />
    </div>
  )
}

function CodeField({
  id,
  totpOnly = false,
}: {
  id: string
  totpOnly?: boolean
}) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>{totpOnly ? "6 位动态码" : "动态码或恢复码"}</Label>
      <Input
        id={id}
        name="code"
        inputMode={totpOnly ? "numeric" : "text"}
        autoComplete="one-time-code"
        autoCapitalize="characters"
        required
        minLength={6}
        maxLength={64}
        placeholder={totpOnly ? "000000" : "000000 或恢复码"}
      />
    </div>
  )
}

function SuccessNotice({ children }: { children: string }) {
  return (
    <div
      className="mb-4 rounded-md border border-success/30 bg-success/5 px-3 py-2 text-sm text-success"
      role="status"
    >
      {children}
    </div>
  )
}

function SessionRow({
  session,
  busy,
  onRevoke,
}: {
  session: BrowserSession
  busy: boolean
  onRevoke: () => void
}) {
  const mobile = /android|iphone|ipad|mobile/i.test(session.user_agent ?? "")
  return (
    <article className="grid min-h-20 grid-cols-[40px_minmax(0,1fr)_auto_36px] items-center gap-3 border-b px-5 py-3 last:border-0 max-sm:grid-cols-[40px_minmax(0,1fr)_36px]">
      <span className="grid size-10 place-items-center rounded-md bg-primary/10 text-primary">
        {mobile ? <Smartphone size={20} /> : <Laptop size={20} />}
      </span>
      <div className="min-w-0">
        <strong className="block truncate text-sm">
          {session.user_agent || "未知浏览器"}
        </strong>
        <small className="mt-1 block text-xs text-muted-foreground">
          {session.ip_address || "IP 未记录"} · 最近活动{" "}
          {new Date(session.last_seen_at).toLocaleString()}
        </small>
      </div>
      {session.current && (
        <span className="rounded bg-success/10 px-2 py-1 text-xs font-semibold text-success max-sm:col-start-2">
          当前设备
        </span>
      )}
      <Button
        type="button"
        variant="ghost"
        size="icon"
        disabled={busy}
        onClick={onRevoke}
        aria-label={`撤销${session.current ? "当前" : ""}会话`}
      >
        <Trash2 className="text-destructive" />
      </Button>
    </article>
  )
}
