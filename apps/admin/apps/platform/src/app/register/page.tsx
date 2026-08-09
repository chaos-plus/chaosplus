import { useState, type FormEvent } from "react"
import { ArrowLeft, Eye, EyeOff, MailCheck, ShieldCheck } from "lucide-react"
import { Link } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { Input } from "@workspace/ui/components/input"
import { Label } from "@workspace/ui/components/label"
import { ThemeModeButton } from "../../components/theme-mode-button"
import { ApiError, iamApi } from "../../lib/iam-api"

export default function RegisterPage() {
  const [busy, setBusy] = useState(false)
  const [complete, setComplete] = useState(false)
  const [code, setCode] = useState("")
  const [verifyMsg, setVerifyMsg] = useState("")
  const [visible, setVisible] = useState(false)
  const [error, setError] = useState("")

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    setBusy(true)
    setError("")
    const data = new FormData(event.currentTarget)
    const password = String(data.get("password") ?? "")
    if (password !== String(data.get("confirm_password") ?? "")) {
      setError("两次输入的密码不一致")
      setBusy(false)
      return
    }
    try {
      await iamApi.register({
        email: String(data.get("email") ?? "").trim(),
        password,
        display_name: String(data.get("display_name") ?? "").trim(),
      })
      setComplete(true)
    } catch (cause) {
      setError(registrationError(cause))
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
          <header className="space-y-2.5">
            <div className="flex items-center gap-2">
              <span className="grid size-7 place-items-center rounded-md bg-primary text-primary-foreground">
                <ShieldCheck className="size-4" />
              </span>
              <span className="text-sm font-medium text-muted-foreground">
                Chaosplus IAM
              </span>
            </div>
            <h1 className="text-xl font-semibold tracking-tight">创建账号</h1>
            <p className="text-sm text-muted-foreground">验证邮箱后即可登录</p>
          </header>

          {complete ? (
            <div className="mt-5 space-y-4">
              <div className="flex items-start gap-3 rounded-md border border-primary/25 bg-primary/5 px-4 py-3 text-sm">
                <MailCheck className="mt-0.5 size-5 shrink-0 text-primary" />
                <div>
                  <p className="font-medium">请输入邮箱验证码</p>
                  <p className="mt-1 text-muted-foreground">
                    我们已向你的邮箱发送 6 位验证码,输入即可完成注册。
                  </p>
                </div>
              </div>
              <form
                className="space-y-4"
                onSubmit={async (e) => {
                  e.preventDefault()
                  if (code.trim().length !== 6) {
                    setVerifyMsg("请输入 6 位验证码")
                    return
                  }
                  try {
                    await iamApi.completeEmailVerification("", code.trim())
                    setVerifyMsg("")
                    window.location.assign("/login")
                  } catch (cause) {
                    setVerifyMsg(cause instanceof Error ? cause.message : "验证码错误")
                  }
                }}
              >
                <div className="space-y-2">
                  <Label htmlFor="verify-code">验证码</Label>
                  <Input
                    id="verify-code"
                    value={code}
                    onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                    inputMode="numeric"
                    autoComplete="one-time-code"
                    autoFocus
                    placeholder="6 位数字"
                    className="text-center text-2xl tracking-[0.5em]"
                  />
                  {verifyMsg && <p className="text-sm text-destructive">{verifyMsg}</p>}
                </div>
                <Button type="submit" className="w-full" disabled={code.trim().length !== 6}>
                  验证并注册
                </Button>
              </form>
            </div>
          ) : (
            <form className="mt-5 space-y-5" onSubmit={submit}>
              <div className="space-y-2">
                <Label htmlFor="registration-email">邮箱</Label>
                <Input
                  id="registration-email"
                  name="email"
                  type="email"
                  autoComplete="email"
                  autoFocus
                  required
                  maxLength={320}
                />
              </div>
              <div className="space-y-2">
                <Label htmlFor="registration-name">显示名称</Label>
                <Input
                  id="registration-name"
                  name="display_name"
                  autoComplete="name"
                  maxLength={128}
                />
              </div>
              <PasswordField
                id="registration-password"
                name="password"
                label="密码"
                visible={visible}
                onVisible={() => setVisible((value) => !value)}
              />
              <PasswordField
                id="registration-confirm"
                name="confirm_password"
                label="确认密码"
                visible={visible}
              />
              {error && (
                <p className="text-sm text-destructive" role="alert">
                  {error}
                </p>
              )}
              <Button type="submit" className="w-full" disabled={busy}>
                {busy ? "正在提交" : "创建账号"}
              </Button>
            </form>
          )}

          <Button variant="ghost" className="mt-4 w-full" asChild>
            <Link to="/login">
              <ArrowLeft />
              返回登录
            </Link>
          </Button>
        </div>
        <p className="mt-6 text-center text-xs text-muted-foreground">
          Chaosplus · © 2026
        </p>
      </main>
    </div>
  )
}

function PasswordField({
  id,
  name,
  label,
  visible,
  onVisible,
}: {
  id: string
  name: string
  label: string
  visible: boolean
  onVisible?: () => void
}) {
  return (
    <div className="space-y-2">
      <Label htmlFor={id}>{label}</Label>
      <div className="relative">
        <Input
          id={id}
          name={name}
          type={visible ? "text" : "password"}
          className={onVisible ? "pr-10" : undefined}
          autoComplete="new-password"
          required
          minLength={12}
          maxLength={1024}
        />
        {onVisible && (
          <button
            type="button"
            className="absolute top-1 right-1 grid size-8 place-items-center rounded-md text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            onClick={onVisible}
            aria-label={visible ? "隐藏密码" : "显示密码"}
          >
            {visible ? (
              <EyeOff className="size-4" />
            ) : (
              <Eye className="size-4" />
            )}
          </button>
        )}
      </div>
    </div>
  )
}

function registrationError(cause: unknown): string {
  if (!(cause instanceof ApiError)) return "身份服务暂时不可用"
  if (cause.status === 403) return "请从已配置的应用地址发起注册"
  if (cause.status === 422) return "请输入有效邮箱和至少 12 个字符的密码"
  if (cause.status === 503) return "当前未开放自助注册"
  return "身份服务暂时不可用"
}
