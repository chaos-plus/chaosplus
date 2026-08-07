import { useTranslations } from "use-intl"

/**
 * Used inside each demo to group multiple examples (variants/sizes/states…).
 * titleKey defaults to the `showcase.demos.common` namespace.
 */
export function DemoSection({
  titleKey,
  children,
}: {
  titleKey: string
  children: React.ReactNode
}) {
  const t = useTranslations("showcase.demos.common")
  return (
    <div className="space-y-3">
      <h4 className="text-sm font-semibold text-muted-foreground">
        {t(titleKey)}
      </h4>
      {children}
    </div>
  )
}
