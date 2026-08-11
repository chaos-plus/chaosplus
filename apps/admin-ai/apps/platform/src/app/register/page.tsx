import { useEffect, useRef, useState, type FormEvent } from "react"
import { ArrowLeft, Eye, EyeOff, RefreshCw, ShieldCheck } from "lucide-react"
import { Link } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { Input } from "@workspace/ui/components/input"
import { Label } from "@workspace/ui/components/label"
import { ThemeModeButton } from "../../components/theme-mode-button"
import { ApiError, iamApi } from "../../lib/iam-api"

export default function RegisterPage() {
  const formRef = useRef<HTMLFormElement>(null)
  const [busy, setBusy] = useState(false)
  const [code, setCode] = useState("")
  const [codeSent, setCodeSent] = useState(false)
  const [captchaId, setCaptchaId] = useState("")
  const [captchaImage, setCaptchaImage] = useState("")
  const [captchaCode, setCaptchaCode] = useState("")
  const [verifyMsg, setVerifyMsg] = useState("")
  const [visible, setVisible] = useState(false)
  const [error, setError] = useState("")

  const loadCaptcha = async () => {
    try {
      const c = await iamApi.captcha()
      setCaptchaId(c.captcha_id)
      setCaptchaImage(c.image_base64)
      setCaptchaCode("")
    } catch {
      setCaptchaImage("")
    }
  }

  useEffect(() => {
    void loadCaptcha()
  }, [])

  const getCode = async () => {
    const data = new FormData(formRef.current!)
    const password = String(data.get("password") ?? "")
    if (password !== String(data.get("confirm_password") ?? "")) {
      setError("两次输入的密码不一致")
      return
    }
    if (!captchaId || captchaCode.trim().length < 1) {
      setError("请先输入图形验证码")
      return
    }
    setBusy(true)
    setError("")
    try {
      const email = String(data.get("email") ?? "").trim()
      await iamApi.register({
        email,
        password,
        display_name: String(data.get("display_name") ?? "").trim(),
        captcha_id: captchaId,
        captcha_code: captchaCode.trim(),
      })
      setCodeSent(true)
      setVerifyMsg(`验证码已发送至 ${email},请查收`)
    } catch (cause) {
      setError(registrationError(cause))
    } finally {
      setBusy(false)
      void loadCaptcha() // captcha 一次性,每次尝试后刷新
    }
  }

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    if (!codeSent) {
      setError("请先获取验证码")
      return
    }
    if (code.trim().length !== 6) {
      setVerifyMsg("请输入 6 位验证码")
      return
    }
    setBusy(true)
    setVerifyMsg("")
    try {
      await iamApi.completeEmailVerification("", code.trim())
      window.location.assign("/login")
    } catch (cause) {
      setVerifyMsg(cause instanceof Error ? cause.message : "验证码错误")
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

          <form ref={formRef} className="mt-5 space-y-5" onSubmit={submit}>
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
            <div className="space-y-2">
              <Label htmlFor="register-captcha">图形验证码</Label>
              <div className="flex gap-2">
                <Input
                  id="register-captcha"
                  value={captchaCode}
                  onChange={(e) =>
                    setCaptchaCode(e.target.value.toLowerCase().replace(/[^a-z0-9-]/g, "").slice(0, 6))
                  }
                  autoComplete="off"
                  placeholder="输入验证码/结果"
                  disabled={!captchaImage}
                  className="text-center text-lg tracking-[0.2em]"
                />
                <button
                  type="button"
                  className="shrink-0 overflow-hidden rounded-md border border-border"
                  onClick={() => void loadCaptcha()}
                  title="点击刷新"
                >
                  {captchaImage ? (
                    <img src={captchaImage} alt="图形验证码" className="h-10 w-28 object-contain" />
                  ) : (
                    <RefreshCw className="m-auto size-4" />
                  )}
                </button>
              </div>
            </div>
            <div className="space-y-2">
              <Label htmlFor="verify-code">验证码</Label>
              <div className="flex gap-2">
                <Input
                  id="verify-code"
                  name="verify_code"
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 6))}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="6 位数字"
                  disabled={!codeSent}
                  className="text-center text-lg tracking-[0.4em]"
                />
                <Button
                  type="button"
                  variant="outline"
                  className="shrink-0"
                  disabled={busy}
                  onClick={getCode}
                >
                  {codeSent ? "重新获取" : "获取验证码"}
                </Button>
              </div>
              {verifyMsg && <p className="text-sm text-muted-foreground">{verifyMsg}</p>}
              {error && (
                <p className="text-sm text-destructive" role="alert">
                  {error}
                </p>
              )}
            </div>
            <Button
              type="submit"
              className="w-full"
              disabled={busy || !codeSent || code.trim().length !== 6}
            >
              {busy ? "正在验证" : "注册"}
            </Button>
          </form>

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
          minLength={8}
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
  if (cause.status === 409) return "该邮箱已被注册,请直接登录"
  if (cause.status === 422) return "邮箱、密码或图形验证码有误"
  if (cause.status === 429) return "请求过于频繁,请 60 秒后再试"
  if (cause.status === 503) return "当前未开放自助注册"
  return "身份服务暂时不可用"
}
