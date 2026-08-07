import { useTranslations } from "use-intl"

import { ScrollArea } from "../scroll-area"
import { Separator } from "../separator"
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

const TAGS = Array.from({ length: 50 }, (_, i) => `Tag ${i + 1}`)

const USER_ROLES = [
  "admin",
  "editor",
  "viewer",
  "editor",
  "viewer",
  "admin",
  "viewer",
  "editor",
] as const
const USER_NAMES = [
  "Alice Chen",
  "Bob Smith",
  "Carol White",
  "David Lee",
  "Eva Brown",
  "Frank Zhang",
  "Grace Kim",
  "Henry Wang",
]
const USER_EMAILS: string[] = [
  "alice",
  "bob",
  "carol",
  "david",
  "eva",
  "frank",
  "grace",
  "henry",
].map((n) => `${n}@example.com`)

export function ScrollAreaDemo() {
  const t = useTranslations("showcase.demos.scrollArea")
  const tTable = useTranslations("showcase.demos.table")
  return (
    <div className="space-y-8">
      <DemoSection titleKey="vertical">
        <ScrollArea className="h-72 w-full max-w-xs rounded-md border">
          <div className="p-4">
            <h4 className="mb-4 text-sm leading-none font-medium">
              {t("userList")}
            </h4>
            {USER_NAMES.map((name, i) => (
              <div key={USER_EMAILS[i]}>
                <div className="py-2">
                  <p className="text-sm font-medium">{name}</p>
                  <p className="text-xs text-muted-foreground">
                    {USER_EMAILS[i]!} · {tTable(USER_ROLES[i]!)}
                  </p>
                </div>
                {i < USER_NAMES.length - 1 && <Separator />}
              </div>
            ))}
          </div>
        </ScrollArea>
      </DemoSection>

      <LocalSection title={t("horizontal")}>
        <ScrollArea
          className="w-full max-w-sm rounded-md border whitespace-nowrap"
          orientation="horizontal"
        >
          <div className="flex w-max gap-2 p-4">
            {TAGS.map((tag) => (
              <div
                key={tag}
                className="flex shrink-0 items-center justify-center rounded-md bg-muted px-3 py-1.5 text-sm font-medium"
              >
                {tag}
              </div>
            ))}
          </div>
        </ScrollArea>
      </LocalSection>

      <LocalSection title={t("both")}>
        <ScrollArea
          className="h-48 w-full max-w-sm rounded-md border"
          orientation="both"
        >
          <div className="p-4" style={{ width: "600px" }}>
            <p className="mb-2 text-sm text-muted-foreground">
              {t("bothDesc")}
            </p>
            {Array.from({ length: 12 }, (_, i) => (
              <p key={i} className="py-1 text-sm whitespace-nowrap">
                {t("row")} {i + 1}:{" "}
                {`Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor.`}
              </p>
            ))}
          </div>
        </ScrollArea>
      </LocalSection>
    </div>
  )
}
