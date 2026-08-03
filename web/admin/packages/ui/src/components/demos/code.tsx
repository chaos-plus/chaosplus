import { useTranslations } from "use-intl"

import { DemoSection } from "./_section"

export function CodeDemo() {
  const t = useTranslations("showcase.demos.code")
  const code = `function greet(name: string) {
  return \`Hello, \${name}!\`
}

console.log(greet("Chaosplus"))`

  return (
    <div className="space-y-8">
      <DemoSection titleKey="block">
        <pre className="max-w-lg overflow-x-auto rounded-lg border bg-muted p-4 text-sm">
          <code>{code}</code>
        </pre>
      </DemoSection>

      <DemoSection titleKey="inline">
        <p className="text-sm">
          {t("use")}{" "}
          <code className="rounded bg-muted px-1 py-0.5 text-sm">useTheme</code>{" "}
          {t("hook")}
        </p>
      </DemoSection>
    </div>
  )
}
