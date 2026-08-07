import { useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslations } from "use-intl"
import { Loader2 } from "lucide-react"

import { Button } from "../button"
import { cn } from "@workspace/ui/lib/utils"
import { DemoSection } from "./_section"

const positions = [
  "top-left",
  "top-center",
  "top-right",
  "bottom-left",
  "bottom-center",
  "bottom-right",
] as const

function LocalSection({
  title,
  children,
}: {
  title: string
  children: React.ReactNode
}) {
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">{title}</h4>
      {children}
    </div>
  )
}

function CountdownToast({
  id,
  showTime = false,
}: {
  id: string | number
  showTime?: boolean
}) {
  const baseMs = 8_000
  const snoozeMs = 5_000
  const [durationMs, setDurationMs] = useState(baseMs)
  const [runId, setRunId] = useState(0)
  // `startedAt`/`now` are seeded once via lazy initializers (Date.now runs outside render) and
  // only ever advanced from event handlers or the interval tick — never from inside an effect.
  const [startedAt, setStartedAt] = useState(() => Date.now())
  const [now, setNow] = useState(() => startedAt)

  useEffect(() => {
    const interval = showTime
      ? window.setInterval(() => setNow(Date.now()), 1000)
      : undefined
    const timeout = window.setTimeout(() => toast.dismiss(id), durationMs)
    return () => {
      if (interval) window.clearInterval(interval)
      window.clearTimeout(timeout)
    }
  }, [durationMs, id, runId, showTime])

  function handleSnooze() {
    const restartedAt = Date.now()
    const elapsedMs = restartedAt - startedAt
    const remainingMs = Math.max(0, durationMs - elapsedMs)
    setDurationMs(remainingMs + snoozeMs)
    setStartedAt(restartedAt)
    setNow(restartedAt)
    setRunId((current) => current + 1)
  }

  const elapsed = Math.min(now - startedAt, durationMs)
  const remainingSeconds = Math.ceil((durationMs - elapsed) / 1000)

  return (
    <div className="relative w-[min(22rem,calc(100vw-3rem))] overflow-hidden rounded-[var(--radius-lg)] border border-border bg-popover p-4 pb-5 text-popover-foreground shadow-lg">
      <div className="flex items-start justify-between gap-4">
        <div className="space-y-1">
          <p className="text-sm font-medium">Sync in progress</p>
          <p className="text-xs text-muted-foreground">
            Finishing background task
          </p>
        </div>
        {showTime && (
          <span className="shrink-0 rounded-md bg-muted px-2 py-1 text-xs font-medium text-muted-foreground">
            {remainingSeconds}s
          </span>
        )}
      </div>
      <div className="mt-3 flex justify-end gap-2">
        <Button size="xs" variant="ghost" onClick={() => toast.dismiss(id)}>
          Dismiss
        </Button>
        <Button size="xs" onClick={handleSnooze}>
          Snooze
        </Button>
      </div>
      <div
        key={runId}
        className="absolute bottom-0 left-0 h-1 bg-primary/60"
        style={{
          animation: `toast-progress ${durationMs}ms linear forwards`,
        }}
      />
    </div>
  )
}

export function ToastDemo() {
  const t = useTranslations("showcase.demos.toast")

  return (
    <div className="space-y-8">
      <DemoSection titleKey="status">
        <div className="flex flex-wrap gap-2">
          <Button variant="outline" onClick={() => toast.success(t("success"))}>
            {t("success")}
          </Button>
          <Button variant="outline" onClick={() => toast.error(t("error"))}>
            {t("error")}
          </Button>
          <Button variant="outline" onClick={() => toast.warning(t("warning"))}>
            {t("warning")}
          </Button>
          <Button variant="outline" onClick={() => toast.info(t("info"))}>
            {t("info")}
          </Button>
        </div>
      </DemoSection>

      <LocalSection title="Positions">
        <div className="flex flex-wrap gap-2">
          {positions.map((position) => (
            <Button
              key={position}
              variant="outline"
              onClick={() => toast.info(position, { position })}
            >
              {position}
            </Button>
          ))}
        </div>
      </LocalSection>

      <LocalSection title="Actions">
        <div className="flex flex-wrap gap-2">
          <Button
            variant="outline"
            onClick={() =>
              toast("Invite sent", {
                description: "The recipient can accept or decline from email.",
                action: {
                  label: "View",
                  onClick: () => toast.info("Opening details"),
                },
              })
            }
          >
            Action button
          </Button>
          <Button
            variant="outline"
            onClick={() =>
              toast("Record archived", {
                description:
                  "You can undo this change before leaving the page.",
                action: {
                  label: "Undo",
                  onClick: () => toast.success(t("saved")),
                },
                cancel: {
                  label: "Dismiss",
                  onClick: () => undefined,
                },
              })
            }
          >
            Action + cancel
          </Button>
        </div>
      </LocalSection>

      <LocalSection title="Countdown progress">
        <Button
          variant="outline"
          onClick={() =>
            toast.custom((id) => <CountdownToast id={id} />, {
              duration: Infinity,
              position: "bottom-right",
              className: cn("toast-custom-shell"),
            })
          }
        >
          8s countdown
        </Button>
      </LocalSection>

      <DemoSection titleKey="loading">
        <Button
          variant="outline"
          onClick={() =>
            toast.promise(new Promise((resolve) => setTimeout(resolve, 2000)), {
              loading: t("loading"),
              success: t("saved"),
              error: t("failed"),
            })
          }
        >
          <Loader2 className="mr-1.5 size-4 animate-spin" />
          {t("saveWithPromise")}
        </Button>
      </DemoSection>
    </div>
  )
}
