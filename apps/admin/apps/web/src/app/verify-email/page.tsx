import { useEffect, useRef, useState } from "react"
import {
  ArrowLeft,
  CheckCircle2,
  LoaderCircle,
  MailCheck,
  ShieldAlert,
} from "lucide-react"
import { Link } from "react-router"
import { Button } from "@workspace/ui/components/button"
import { ThemeModeButton } from "../../components/theme-mode-button"
import { ApiError, iamApi } from "../../lib/iam-api"

type VerificationState = "pending" | "success" | "invalid" | "unavailable"

export default function VerifyEmailPage() {
  const [token] = useState(
    () => new URLSearchParams(window.location.search).get("token") ?? ""
  )
  const [state, setState] = useState<VerificationState>(() =>
    token ? "pending" : "invalid"
  )
  const started = useRef(false)

  useEffect(() => {
    if (started.current) return
    started.current = true
    window.history.replaceState(null, "", "/verify-email")
    if (!token) return
    void iamApi.completeEmailVerification(token).then(
      () => setState("success"),
      (cause: unknown) =>
        setState(
          cause instanceof ApiError && cause.status === 400
            ? "invalid"
            : "unavailable"
        )
    )
  }, [token])

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
                <MailCheck className="size-4" />
              </span>
              <span className="text-sm font-medium text-muted-foreground">
                Chaosplus IAM
              </span>
            </div>
            <h1 className="text-xl font-semibold">验证邮箱</h1>
          </header>

          <VerificationResult state={state} />

          {state !== "pending" && (
            <Button className="mt-5 w-full" asChild>
              <Link to="/login">
                <ArrowLeft />
                返回登录
              </Link>
            </Button>
          )}
        </div>
        <p className="mt-6 text-center text-xs text-muted-foreground">
          Chaosplus · © 2026
        </p>
      </main>
    </div>
  )
}

function VerificationResult({ state }: { state: VerificationState }) {
  const content = {
    pending: {
      icon: <LoaderCircle className="size-5 animate-spin" />,
      title: "正在验证",
      description: "正在确认邮箱所有权。",
    },
    success: {
      icon: <CheckCircle2 className="size-5" />,
      title: "邮箱已验证",
      description: "邮箱所有权已确认，现在可以登录。",
    },
    invalid: {
      icon: <ShieldAlert className="size-5" />,
      title: "验证链接无效",
      description: "链接已过期、已使用，或邮箱已经发生变化。",
    },
    unavailable: {
      icon: <ShieldAlert className="size-5" />,
      title: "暂时无法验证",
      description: "身份服务暂时不可用，请稍后重新打开验证链接。",
    },
  }[state]

  return (
    <div
      className="mt-5 flex items-start gap-3 rounded-md border border-border bg-muted/40 px-4 py-3"
      role="status"
      aria-live="polite"
    >
      <span className="mt-0.5 text-primary">{content.icon}</span>
      <div>
        <p className="text-sm font-medium">{content.title}</p>
        <p className="mt-1 text-sm text-muted-foreground">
          {content.description}
        </p>
      </div>
    </div>
  )
}
