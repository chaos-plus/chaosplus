import { useEffect, useState } from "react"
import { useTranslations } from "use-intl"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"

import { cn } from "@workspace/ui/lib/utils"

/**
 * Shiki syntax highlighting + a copy button. Shiki is dynamically loaded on demand on the client (lazy),
 * using dual themes (github-light/github-dark) to adapt to light/dark mode.
 */
export function CodeBlock({
  code,
  lang = "tsx",
  className,
}: {
  code: string
  lang?: string
  className?: string
}) {
  const t = useTranslations("showcase")
  const [html, setHtml] = useState<string>("")
  const [copied, setCopied] = useState(false)

  useEffect(() => {
    let active = true
    void (async () => {
      try {
        const { codeToHtml } = await import("shiki")
        const out = await codeToHtml(code, {
          lang,
          themes: { light: "github-light", dark: "github-dark" },
          defaultColor: false,
        })
        if (active) setHtml(out)
      } catch {
        if (active) setHtml("")
      }
    })()
    return () => {
      active = false
    }
  }, [code, lang])

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(code)
      setCopied(true)
      toast.success(t("copied"))
      setTimeout(() => setCopied(false), 1500)
    } catch {
      toast.error(t("copyFailed"))
    }
  }

  return (
    <div className={cn("group/code relative", className)}>
      <button
        type="button"
        onClick={onCopy}
        aria-label={t("copy")}
        className="absolute top-3 right-3 z-10 inline-flex size-7 items-center justify-center rounded-md border bg-background/80 text-muted-foreground opacity-0 backdrop-blur transition-opacity group-hover/code:opacity-100 hover:text-foreground focus-visible:opacity-100"
      >
        {copied ? (
          <Check className="size-3.5" />
        ) : (
          <Copy className="size-3.5" />
        )}
      </button>
      {html ? (
        <div
          className="max-h-[480px] overflow-auto rounded-xl border [&_pre]:m-0 [&_pre]:rounded-xl [&_pre]:bg-muted/40 [&_pre]:p-4 [&_pre]:text-[0.8rem] [&_pre]:leading-relaxed"
          dangerouslySetInnerHTML={{ __html: html }}
        />
      ) : (
        <pre className="max-h-[480px] overflow-auto rounded-xl border bg-muted/40 p-4 text-[0.8rem] leading-relaxed">
          <code>{code}</code>
        </pre>
      )}
    </div>
  )
}
