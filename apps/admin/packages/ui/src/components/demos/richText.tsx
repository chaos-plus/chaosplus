import { useRef, useState } from "react"
import { useTranslations } from "use-intl"

import { Button } from "../button"
import { Label } from "../label"
import { DemoSection } from "./_section"

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

export function RichTextDemo() {
  const t = useTranslations("showcase.demos.richText")
  const [html, setHtml] = useState(t("initialText"))
  const editorRef = useRef<HTMLDivElement>(null)

  const exec = (command: string, value: string | undefined = undefined) => {
    document.execCommand(command, false, value)
    if (editorRef.current) {
      setHtml(editorRef.current.textContent ?? "")
    }
  }

  const toolbarItems = [
    { label: "B", command: "bold" },
    { label: "I", command: "italic" },
    { label: "U", command: "underline" },
    { label: "S", command: "strikeThrough" },
    { label: "H1", command: "formatBlock", value: "H1" },
    { label: "H2", command: "formatBlock", value: "H2" },
    { label: "P", command: "formatBlock", value: "P" },
    { label: "UL", command: "insertUnorderedList" },
    { label: "OL", command: "insertOrderedList" },
    { label: t("clear"), command: "removeFormat" },
  ]

  return (
    <div className="space-y-8">
      <DemoSection titleKey="contentEditable">
        <div className="grid w-full max-w-lg gap-1.5">
          <Label>{t("editor")}</Label>
          <div className="rounded-lg border bg-background">
            <div className="flex flex-wrap items-center gap-1 border-b bg-muted/50 p-2">
              {toolbarItems.map((item) => (
                <button
                  key={item.label}
                  type="button"
                  onClick={() => exec(item.command, item.value)}
                  className="rounded bg-muted px-2 py-1 text-xs font-medium text-muted-foreground transition-colors hover:bg-muted/80 hover:text-foreground"
                >
                  {item.label}
                </button>
              ))}
            </div>
            <div
              ref={editorRef}
              contentEditable
              suppressContentEditableWarning
              className="min-h-[120px] px-3 py-2 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
              onInput={(e) => setHtml(e.currentTarget.textContent ?? "")}
            >
              {html}
            </div>
          </div>
          <p className="text-xs text-muted-foreground">
            {t("plainText")} {html}
          </p>
        </div>
      </DemoSection>

      <LocalSection title={t("readonlyView")}>
        <div className="w-full max-w-lg rounded-lg border bg-muted/30 px-5 py-4">
          <div className="prose-like space-y-3 text-sm">
            <h2 className="text-base font-semibold text-foreground">
              {t("sampleContent")}
            </h2>
            <p className="leading-relaxed text-muted-foreground">
              {t("readonlyParagraph")}
            </p>
            <ul className="list-disc space-y-1 pl-5 text-muted-foreground">
              <li>{t("listItemOne")}</li>
              <li>{t("listItemTwo")}</li>
              <li>{t("listItemThree")}</li>
            </ul>
            <p className="leading-relaxed text-muted-foreground">
              {t("readonlyUsage")}
            </p>
          </div>
        </div>
      </LocalSection>

      <LocalSection title={t("toolbar")}>
        <div className="w-full max-w-lg">
          <p className="mb-2 text-xs text-muted-foreground">
            {t("toolbarTitle")}
          </p>
          <div className="flex flex-wrap items-center gap-1 rounded-lg border bg-muted/50 p-2">
            {[
              { label: t("bold"), shortLabel: "B" },
              { label: t("italic"), shortLabel: "I" },
              { label: t("underline"), shortLabel: "U" },
              { label: t("strike"), shortLabel: "S" },
            ].map((item) => (
              <Button
                key={item.label}
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs font-semibold"
                aria-label={item.label}
              >
                {item.shortLabel}
              </Button>
            ))}
            <div className="mx-1 h-5 w-px bg-border" />
            {["H1", "H2", "P"].map((label) => (
              <Button
                key={label}
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs font-medium"
                aria-label={label}
              >
                {label}
              </Button>
            ))}
            <div className="mx-1 h-5 w-px bg-border" />
            {["UL", "OL"].map((label) => (
              <Button
                key={label}
                variant="ghost"
                size="sm"
                className="h-7 px-2 text-xs font-medium"
                aria-label={label}
              >
                {label}
              </Button>
            ))}
          </div>
        </div>
      </LocalSection>
    </div>
  )
}
