import { useEffect, useState, type FormEvent } from "react"
import { ArrowLeft, Eye, EyeOff, Fingerprint, ShieldCheck } from "lucide-react"
import { Link, Navigate, useLocation } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { Input } from "@workspace/ui/components/input"
import { Label } from "@workspace/ui/components/label"
import {
  ApiError,
  iamApi,
  type AuthnCapabilities,
  type LoginResult,
} from "../../lib/iam-api"
import { useAuth } from "../../components/auth"
import { ThemeModeButton } from "../../components/theme-mode-button"

export default function LoginPage() {
  const { status, login, verifyLoginMfa, loginWithPasskey } = useAuth()
  const location = useLocation()
  const [visible, setVisible] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState("")
  const [challenge, setChallenge] = useState<LoginResult | null>(null)
  const [capabilities, setCapabilities] = useState<AuthnCapabilities | null>(
    null
  )
  useEffect(() => {
    void iamApi
      .capabilities()
      .then(setCapabilities, () => setCapabilities(null))
  }, [])
  if (status === "authenticated") return <Navigate to="/" replace />

  const submitPassword = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const requested = (location.state as { from?: string } | null)?.from ?? "/"
    try {
      const result = await login(
        String(data.get("login_name") ?? ""),
        String(data.get("password") ?? ""),
        `${window.location.origin}${requested}`
      )
      if (result.status === "mfa_required") setChallenge(result)
    } catch (cause) {
      setError(loginError(cause))
    } finally {
      setBusy(false)
    }
  }

  const submitMfa = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!challenge?.challenge_id) return
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    try {
      await verifyLoginMfa(
        challenge.challenge_id,
        String(data.get("code") ?? "").trim()
      )
    } catch (cause) {
      setError(
        cause instanceof ApiError && cause.status === 401
          ? "验证码、恢复码或登录挑战无效"
          : "身份服务暂时不可用"
      )
    } finally {
      setBusy(false)
    }
  }

  const submitPasskey = async () => {
    setBusy(true)
    setError("")
    const requested = (location.state as { from?: string } | null)?.from ?? "/"
    try {
      await loginWithPasskey(`${window.location.origin}${requested}`)
    } catch (cause) {
      setError(
        cause instanceof DOMException && cause.name === "NotAllowedError"
          ? "未完成通行密钥验证"
          : cause instanceof ApiError && cause.status === 401
            ? "通行密钥无效或登录挑战已过期"
            : (cause as Error).message || "身份服务暂时不可用"
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex min-h-svh flex-col items-center justify-center bg-muted/30 px-4 py-10">
      <div className="absolute top-4 right-4">
        <ThemeModeButton />
      </div>
      <main className="w-full max-w-[400px]">
        <div className="rounded-xl border border-border/70 bg-card p-8 shadow-lg">
          {challenge ? (
            <MfaForm
              challenge={challenge}
              busy={busy}
              error={error}
              onSubmit={submitMfa}
              onBack={() => {
                setChallenge(null)
                setError("")
              }}
            />
          ) : (
            <PasswordForm
              busy={busy}
              error={error}
              visible={visible}
              onVisibleChange={() => setVisible((value) => !value)}
              onSubmit={submitPassword}
              onPasskey={() => void submitPasskey()}
              capabilities={capabilities}
            />
          )}
        </div>
        <p className="mt-6 text-center text-xs text-muted-foreground">
          Chaosplus · © 2026
        </p>
      </main>
    </div>
  )
}

function BrandHeader({ mfa = false }: { mfa?: boolean }) {
  return (
    <div className="space-y-2.5">
      <div className="flex items-center gap-2">
        <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
          <ShieldCheck className="size-4" />
        </span>
        <span className="text-sm font-medium text-muted-foreground">
          Chaosplus IAM
        </span>
      </div>
      <h1 id="login-title" className="text-xl font-semibold tracking-tight">
        {mfa ? "完成安全验证" : "欢迎回来"}
      </h1>
      <p className="text-sm text-muted-foreground">
        {mfa ? "输入验证器动态码或一枚恢复码" : "登录身份与访问管理控制台"}
      </p>
    </div>
  )
}

function PasswordForm({
  busy,
  error,
  visible,
  onVisibleChange,
  onSubmit,
  onPasskey,
  capabilities,
}: {
  busy: boolean
  error: string
  visible: boolean
  onVisibleChange: () => void
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onPasskey: () => void
  capabilities: AuthnCapabilities | null
}) {
  return (
    <form
      className="space-y-5"
      onSubmit={onSubmit}
      aria-labelledby="login-title"
    >
      <BrandHeader />
      <div className="space-y-2">
        <Label htmlFor="login-name">账号</Label>
        <Input
          id="login-name"
          name="login_name"
          autoComplete="username"
          autoFocus
          required
          maxLength={200}
        />
      </div>
      <div className="space-y-2">
        <Label htmlFor="password">密码</Label>
        <div className="relative">
          <Input
            id="password"
            name="password"
            type={visible ? "text" : "password"}
            className="pr-10"
            autoComplete="current-password"
            required
            maxLength={1024}
          />
          <button
            type="button"
            className="absolute top-1 right-1 grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            onClick={onVisibleChange}
            aria-label={visible ? "隐藏密码" : "显示密码"}
          >
            {visible ? (
              <EyeOff className="size-4" />
            ) : (
              <Eye className="size-4" />
            )}
          </button>
        </div>
        {capabilities?.password_recovery && (
          <div className="flex justify-end">
            <Link
              to="/recover"
              className="text-sm font-medium text-primary hover:underline focus-visible:rounded-sm focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
            >
              忘记密码
            </Link>
          </div>
        )}
      </div>
      <FormError error={error} />
      <Button type="submit" className="w-full" disabled={busy}>
        {busy ? "正在验证" : "登录"}
      </Button>
      {capabilities?.passkey && (
        <>
          <div className="relative text-center text-xs text-muted-foreground before:absolute before:top-1/2 before:left-0 before:h-px before:w-full before:bg-border">
            <span className="relative bg-card px-2">或</span>
          </div>
          <Button
            type="button"
            variant="outline"
            className="w-full"
            disabled={busy}
            onClick={onPasskey}
          >
            <Fingerprint />
            使用通行密钥
          </Button>
        </>
      )}
      {capabilities?.registration && (
        <p className="text-center text-sm text-muted-foreground">
          还没有账号？{" "}
          <Link
            className="font-medium text-primary hover:underline"
            to="/register"
          >
            创建账号
          </Link>
        </p>
      )}
    </form>
  )
}

function MfaForm({
  challenge,
  busy,
  error,
  onSubmit,
  onBack,
}: {
  challenge: LoginResult
  busy: boolean
  error: string
  onSubmit: (event: FormEvent<HTMLFormElement>) => void
  onBack: () => void
}) {
  return (
    <form
      className="space-y-5"
      onSubmit={onSubmit}
      aria-labelledby="login-title"
    >
      <BrandHeader mfa />
      <div className="space-y-2">
        <Label htmlFor="mfa-code">验证码或恢复码</Label>
        <Input
          id="mfa-code"
          name="code"
          autoComplete="one-time-code"
          autoCapitalize="characters"
          autoFocus
          required
          minLength={6}
          maxLength={64}
          placeholder="000000"
        />
        {challenge.expires_at && (
          <p className="text-xs text-muted-foreground">
            本次验证在 {new Date(challenge.expires_at).toLocaleTimeString()}{" "}
            前有效
          </p>
        )}
      </div>
      <FormError error={error} />
      <div className="grid grid-cols-[auto_1fr] gap-2">
        <Button
          type="button"
          variant="outline"
          onClick={onBack}
          disabled={busy}
        >
          <ArrowLeft />
          返回
        </Button>
        <Button type="submit" disabled={busy}>
          {busy ? "正在验证" : "继续"}
        </Button>
      </div>
    </form>
  )
}

function FormError({ error }: { error: string }) {
  return error ? (
    <p className="text-sm text-destructive" role="alert">
      {error}
    </p>
  ) : null
}

function loginError(cause: unknown): string {
  return cause instanceof ApiError && cause.status === 401
    ? "账号或密码不正确"
    : "身份服务暂时不可用"
}
