import { useTranslations } from "use-intl"

import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "../collapsible"
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

export function CollapsibleDemo() {
  const t = useTranslations("showcase.demos.collapsible")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="basic">
        <Collapsible className="w-full max-w-sm overflow-hidden rounded-lg border">
          <CollapsibleTrigger className="bg-muted/50 px-4 py-3">
            {t("whatIsCollapsible")}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="px-4 py-3 text-sm text-muted-foreground">
              {t("collapsibleDesc")}
            </div>
          </CollapsibleContent>
        </Collapsible>
      </DemoSection>

      <LocalSection title={t("faq")}>
        <div className="w-full max-w-sm space-y-2">
          {[
            { q: t("q1Question"), a: t("q1Answer") },
            { q: t("q2Question"), a: t("q2Answer") },
            { q: t("q3Question"), a: t("q3Answer") },
          ].map(({ q, a }) => (
            <Collapsible key={q} className="overflow-hidden rounded-lg border">
              <CollapsibleTrigger className="px-4 py-3 text-left">
                {q}
              </CollapsibleTrigger>
              <CollapsibleContent>
                <div className="border-t px-4 py-3 text-sm text-muted-foreground">
                  {a}
                </div>
              </CollapsibleContent>
            </Collapsible>
          ))}
        </div>
      </LocalSection>

      <LocalSection title={t("defaultOpen")}>
        <Collapsible
          defaultOpen
          className="w-full max-w-sm overflow-hidden rounded-lg border"
        >
          <CollapsibleTrigger className="bg-muted/50 px-4 py-3">
            {t("openByDefault")}
          </CollapsibleTrigger>
          <CollapsibleContent>
            <div className="px-4 py-3 text-sm text-muted-foreground">
              {t("openByDefaultDesc")}
            </div>
          </CollapsibleContent>
        </Collapsible>
      </LocalSection>
    </div>
  )
}
