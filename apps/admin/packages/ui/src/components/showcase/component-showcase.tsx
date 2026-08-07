import {
  Suspense,
  lazy,
  useMemo,
  useState,
  useTransition,
  useDeferredValue,
} from "react"
import { useTranslations } from "use-intl"
import { cn } from "@workspace/ui/lib/utils"
import {
  demoConfig,
  defaultDemoId,
  type DemoCategory,
} from "@workspace/ui/components/showcase/component-showcase-config"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@workspace/ui/components/index"
import { Skeleton } from "@workspace/ui/components/skeleton"
import { CodeBlock } from "@workspace/ui/components/demos/_code-block"
import { demoSources } from "@workspace/ui/components/demos/sources.generated"
import { getPropsMeta } from "@workspace/ui/components/demos/_props-meta"

const categories: DemoCategory[] = [
  "forms",
  "data",
  "layout",
  "navigation",
  "feedback",
  "overlay",
  "media",
  "automation",
]

type ViewTab = "preview" | "code" | "api"
const viewTabs: ViewTab[] = ["preview", "code", "api"]

// Loaded in a separate JS chunk — only fetched when the page is idle
const LazyDemoRenderer = lazy(() =>
  import("@workspace/ui/components/showcase/component-demo-renderer").then(
    (m) => ({
      default: m.ComponentDemoRenderer,
    })
  )
)

function DemoSkeleton() {
  return (
    <div className="space-y-6">
      <div className="rounded-xl border bg-card p-6">
        <div className="space-y-3">
          <div className="flex flex-wrap gap-3">
            <Skeleton className="h-8 w-20" />
            <Skeleton className="h-8 w-24" />
            <Skeleton className="h-8 w-20" />
            <Skeleton className="h-8 w-16" />
          </div>
        </div>
      </div>
    </div>
  )
}

export interface ComponentShowcaseProps {
  className?: string
}

export function ComponentShowcase({ className }: ComponentShowcaseProps) {
  const t = useTranslations("showcase")
  // isPending = true immediately on click → causes instant visual update → closes INP
  const [isPending, startTransition] = useTransition()
  const [activeId, setActiveId] = useState(defaultDemoId)
  const [view, setView] = useState<ViewTab>("preview")
  // deferredId lags behind activeId — React renders it as low-priority work
  const deferredId = useDeferredValue(activeId)

  const grouped = useMemo(() => {
    return categories.map((cat) => ({
      id: cat,
      label: t(`categories.${cat}`),
      items: demoConfig.filter((d) => d.category === cat),
    }))
  }, [t])
  const demoSelectItems = useMemo(
    () =>
      demoConfig.map((demo) => ({
        value: demo.id,
        label: t(`components.${demo.id}`),
      })),
    [t]
  )

  const handleItemClick = (id: string) => {
    startTransition(() => setActiveId(id))
  }

  const handleSelectChange = (value: string | null) => {
    if (value) startTransition(() => setActiveId(value))
  }

  const propsMeta = getPropsMeta(activeId)

  return (
    <div
      className={cn("flex min-h-0 flex-1 flex-col overflow-hidden", className)}
    >
      <div className="flex flex-1 overflow-hidden">
        <aside className="hidden w-60 shrink-0 overflow-y-auto border-r bg-sidebar p-4 md:block">
          <nav className="space-y-6">
            {grouped.map((group) => (
              <div key={group.id}>
                <h3 className="mb-2 px-2 text-xs font-semibold tracking-wider text-sidebar-foreground/60 uppercase">
                  {group.label}
                </h3>
                <ul className="space-y-0.5">
                  {group.items.map((item) => (
                    <li key={item.id}>
                      <button
                        type="button"
                        onClick={() => handleItemClick(item.id)}
                        className={cn(
                          "w-full cursor-pointer rounded-md px-2 py-1.5 text-left text-sm transition-colors",
                          activeId === item.id
                            ? "sidebar-active-item bg-primary font-medium text-primary-foreground"
                            : "text-sidebar-foreground hover:bg-sidebar-accent hover:text-sidebar-accent-foreground"
                        )}
                      >
                        {t(`components.${item.id}`)}
                      </button>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
          </nav>
        </aside>

        <main className="flex-1 overflow-y-auto p-6">
          <div className="mx-auto max-w-3xl space-y-6">
            <div className="md:hidden">
              <Select
                items={demoSelectItems}
                value={activeId}
                onValueChange={handleSelectChange}
              >
                <SelectTrigger className="w-full">
                  <SelectValue placeholder={t("components.button")} />
                </SelectTrigger>
                <SelectContent>
                  {demoSelectItems.map((demo) => (
                    <SelectItem key={demo.value} value={demo.value}>
                      {demo.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Title updates immediately (via activeId), not deferred */}
            <div>
              <h2 className="text-2xl font-bold">
                {t(`components.${activeId}`)}
              </h2>
              <p className="text-muted-foreground">{t("usage")}</p>
            </div>

            {/* Preview / Code / API three-way switcher */}
            <div className="flex gap-1 border-b">
              {viewTabs.map((tab) => (
                <button
                  key={tab}
                  type="button"
                  onClick={() => setView(tab)}
                  className={cn(
                    "relative cursor-pointer px-3 py-2 text-sm font-medium transition-colors",
                    view === tab
                      ? "text-foreground"
                      : "text-muted-foreground hover:text-foreground"
                  )}
                >
                  {t(`tabs.${tab}`)}
                  {view === tab && (
                    <span className="absolute inset-x-0 -bottom-px h-0.5 bg-primary" />
                  )}
                </button>
              ))}
            </div>

            {view === "preview" && (
              <div
                className={cn(
                  "transition-opacity duration-150",
                  isPending && "pointer-events-none opacity-40"
                )}
              >
                <Suspense fallback={<DemoSkeleton />}>
                  <div className="rounded-xl border bg-card p-6 text-card-foreground">
                    {/* deferredId: low-priority — browser stays responsive */}
                    <LazyDemoRenderer id={deferredId} />
                  </div>
                </Suspense>
              </div>
            )}

            {view === "code" && (
              <CodeBlock code={demoSources[activeId] ?? ""} lang="tsx" />
            )}

            {view === "api" && (
              <div className="rounded-xl border">
                {propsMeta.length === 0 ? (
                  <p className="p-6 text-sm text-muted-foreground">
                    {t("noApi")}
                  </p>
                ) : (
                  <div className="overflow-x-auto">
                    <table className="w-full min-w-[720px] text-sm">
                      <thead>
                        <tr className="border-b text-left text-muted-foreground">
                          <th className="p-3 font-medium">
                            {t("api.colName")}
                          </th>
                          <th className="p-3 font-medium">
                            {t("api.colType")}
                          </th>
                          <th className="p-3 font-medium">
                            {t("api.colDefault")}
                          </th>
                          <th className="p-3 font-medium">
                            {t("api.colDesc")}
                          </th>
                        </tr>
                      </thead>
                      <tbody>
                        {propsMeta.map((p) => {
                          const descKey = `api.${activeId}.${p.descKey}`
                          return (
                            <tr
                              key={p.name}
                              className="border-b align-top last:border-0"
                            >
                              <td className="p-3 font-mono text-xs">
                                {p.name}
                              </td>
                              <td className="p-3 font-mono text-xs text-muted-foreground">
                                {p.type}
                              </td>
                              <td className="p-3 font-mono text-xs text-muted-foreground">
                                {p.default ?? "—"}
                              </td>
                              <td className="p-3 text-muted-foreground">
                                {t.has(descKey) ? t(descKey) : p.descKey}
                              </td>
                            </tr>
                          )
                        })}
                      </tbody>
                    </table>
                  </div>
                )}
              </div>
            )}
          </div>
        </main>
      </div>
    </div>
  )
}
